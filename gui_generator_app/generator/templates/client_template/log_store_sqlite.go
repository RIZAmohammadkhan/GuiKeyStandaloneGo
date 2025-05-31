// GuiKeyStandaloneGo/generator/templates/client_template/log_store_sqlite.go
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/types"

	"github.com/google/uuid"
	// Pure Go SQLite driver will be added by `go mod tidy`
)

const (
	dbDriverName             = "sqlite" // For modernc.org/sqlite
	aggressivePruneBatchSize = 100      // How many oldest events to delete at a time during aggressive prune
	dbSizeReductionTarget    = 0.8      // Target to reduce DB size to (e.g., 80% of max) during aggressive prune
)

type LogStore struct {
	db     *sql.DB
	logger *log.Logger
	dbPath string
}

func NewLogStore(dbFilePath string, logger *log.Logger) (*LogStore, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "LOGSTORE_FALLBACK: ", log.LstdFlags|log.Lshortfile)
		logger.Println("Warning: LogStore initialized with fallback logger.")
	}
	dbDir := filepath.Dir(dbFilePath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("logstore: failed to create database directory %s: %w", dbDir, err)
	}
	// DSN for modernc.org/sqlite, includes PRAGMAs
	dsn := fmt.Sprintf("file:%s?cache=shared&mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", dbFilePath)
	db, err := sql.Open(dbDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("logstore: failed to open SQLite database at %s: %w", dbFilePath, err)
	}
	// Pragmas are now part of the DSN, no need for separate Exec calls for them.

	store := &LogStore{db: db, logger: logger, dbPath: dbFilePath}
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("logstore: failed to initialize schema: %w", err)
	}
	logger.Printf("LogStore: SQLite initialized. DB: %s", dbFilePath)
	return store, nil
}

func (s *LogStore) initSchema() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS log_events (
		id TEXT PRIMARY KEY, client_id TEXT NOT NULL, event_timestamp INTEGER NOT NULL, 
		application_name TEXT, schema_version INTEGER, event_data_json TEXT NOT NULL 
	);
	CREATE INDEX IF NOT EXISTS idx_log_events_event_timestamp ON log_events (event_timestamp);`
	_, err := s.db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("logstore: failed to create log_events table: %w", err)
	}
	s.logger.Println("LogStore: SQLite schema initialized/verified.")
	return nil
}

func (s *LogStore) AddLogEvent(event types.LogEvent) error {
	jsonData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("logstore: failed to marshal LogEvent (ID: %s): %w", event.ID, err)
	}
	_, err = s.db.Exec(
		"INSERT INTO log_events (id, client_id, event_timestamp, application_name, schema_version, event_data_json) VALUES (?, ?, ?, ?, ?, ?)",
		event.ID.String(), event.ClientID.String(), event.Timestamp.Unix(),
		event.ApplicationName, event.SchemaVersion, string(jsonData),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			s.logger.Printf("LogStore: Warning - LogEvent with ID %s already exists. Skipping insert.", event.ID)
			return nil
		}
		return fmt.Errorf("logstore: failed to insert LogEvent (ID: %s): %w", event.ID, err)
	}
	return nil
}

func (s *LogStore) GetBatchForSync(limit int) ([]types.LogEvent, error) {
	if limit <= 0 {
		return []types.LogEvent{}, nil
	}
	rows, err := s.db.Query("SELECT event_data_json FROM log_events ORDER BY event_timestamp ASC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("logstore: failed to query batch for sync: %w", err)
	}
	defer rows.Close()
	var events []types.LogEvent
	for rows.Next() {
		var jsonData string
		if errR := rows.Scan(&jsonData); errR != nil {
			s.logger.Printf("LogStore: Error scanning row for sync batch: %v. Skipping.", errR)
			continue
		}
		var event types.LogEvent
		if errU := json.Unmarshal([]byte(jsonData), &event); errU != nil {
			s.logger.Printf("LogStore: Error unmarshaling LogEvent JSON from DB for sync (Data: %.50s...): %v. Skipping.", jsonData, errU)
			continue
		}
		events = append(events, event)
	}
	if errR := rows.Err(); errR != nil {
		return nil, fmt.Errorf("logstore: error iterating sync batch rows: %w", errR)
	}
	return events, nil
}

func (s *LogStore) ConfirmEventsSynced(eventIDs []uuid.UUID) error {
	if len(eventIDs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("logstore: failed to begin tx for confirm sync: %w", err)
	}
	stmt, err := tx.Prepare("DELETE FROM log_events WHERE id = ?")
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("logstore: failed to prepare delete for confirm sync: %w", err)
	}
	defer stmt.Close()
	deletedCount, failedDeletes := 0, 0
	for _, id := range eventIDs {
		res, execErr := stmt.Exec(id.String())
		if execErr != nil {
			s.logger.Printf("LogStore: Error deleting event ID %s during sync: %v", id, execErr)
			failedDeletes++
			break
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			deletedCount++
		}
	}
	if failedDeletes > 0 {
		tx.Rollback()
		return fmt.Errorf("logstore: %d delete operations failed in confirm sync; transaction rolled back", failedDeletes)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("logstore: failed to commit confirm sync (deleted %d): %w", deletedCount, err)
	}
	s.logger.Printf("LogStore: Confirmed %d events synced (deleted). Batch size: %d", deletedCount, len(eventIDs))
	return nil
}

func (s *LogStore) PruneOldLogs(retentionDays uint32) (int64, error) {
	if retentionDays == 0 {
		s.logger.Println("LogStore: Regular log pruning disabled (retention_days = 0).")
		return 0, nil
	}
	cutoffTime := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	cutoffTimestampUnix := cutoffTime.Unix()
	s.logger.Printf("LogStore: Regular pruning logs older than %d days (event_timestamp before %s / %d).",
		retentionDays, cutoffTime.Format(time.RFC3339), cutoffTimestampUnix)
	result, err := s.db.Exec("DELETE FROM log_events WHERE event_timestamp < ?", cutoffTimestampUnix)
	if err != nil {
		return 0, fmt.Errorf("logstore: failed to execute regular prune: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		s.logger.Printf("LogStore: Regular pruning removed %d old entries.", rowsAffected)
	}
	return rowsAffected, nil
}

func (s *LogStore) PruneOldestEventsAggressively(count int) (int64, error) {
	if count <= 0 {
		return 0, nil
	}
	s.logger.Printf("LogStore: AGGRESSIVE PRUNING - Attempting to delete %d oldest events...", count)

	result, err := s.db.Exec("DELETE FROM log_events WHERE id IN (SELECT id FROM log_events ORDER BY event_timestamp ASC LIMIT ?)", count)
	if err != nil {
		return 0, fmt.Errorf("logstore: aggressive prune failed: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		s.logger.Printf("LogStore: AGGRESSIVE PRUNING - Successfully deleted %d oldest events.", rowsAffected)
	} else {
		s.logger.Println("LogStore: AGGRESSIVE PRUNING - No events found to delete, or query failed to match any.")
	}
	return rowsAffected, nil
}

func (s *LogStore) CheckAndEnforceDBSize(maxAllowedMB *uint64) {
	if maxAllowedMB == nil || *maxAllowedMB == 0 {
		return
	}
	limitBytes := *maxAllowedMB * 1024 * 1024

	fi, err := os.Stat(s.dbPath)
	if err != nil {
		s.logger.Printf("LogStore: Error stating DB file for size check ('%s'): %v", s.dbPath, err)
		return
	}
	currentSizeBytes := fi.Size()

	if uint64(currentSizeBytes) >= limitBytes {
		s.logger.Printf("LogStore: WARNING - DB size (%d Bytes) meets or exceeds limit (%d Bytes). Initiating aggressive pruning.", currentSizeBytes, limitBytes)

		targetSizeBytes := uint64(float64(limitBytes) * dbSizeReductionTarget)
		bytesToReduce := currentSizeBytes - int64(targetSizeBytes)

		if bytesToReduce <= 0 {
			s.logger.Println("LogStore: DB size already near target after limit hit, no aggressive prune needed now.")
			return
		}

		var totalPruned int64
		for uint64(fi.Size()) > targetSizeBytes {
			prunedThisBatch, errPrune := s.PruneOldestEventsAggressively(aggressivePruneBatchSize)
			if errPrune != nil {
				s.logger.Printf("LogStore: Error during aggressive pruning batch: %v. Stopping.", errPrune)
				break
			}
			if prunedThisBatch == 0 {
				s.logger.Println("LogStore: Aggressive pruning - no more events to delete, but size still over target. DB might contain free pages.")
				break
			}
			totalPruned += prunedThisBatch

			fi, err = os.Stat(s.dbPath)
			if err != nil {
				s.logger.Printf("LogStore: Error re-stating DB file after aggressive prune batch: %v. Stopping.", err)
				break
			}
		}
		if totalPruned > 0 {
			s.logger.Printf("LogStore: Aggressive pruning completed. Total events deleted: %d. Current DB size: %d Bytes.", totalPruned, fi.Size())
		}
	}
}

func (s *LogStore) Close() error {
	if s.db != nil {
		s.logger.Println("LogStore: Closing database connection.")
		return s.db.Close()
	}
	return nil
}

func runSQLiteCacheManager(
	logStore *LogStore,
	logEventsIn <-chan types.LogEvent,
	internalQuit <-chan struct{},
	wg *sync.WaitGroup,
	retentionDays uint32,
	maxLogFileSizeMB *uint64,
) {
	defer wg.Done()
	logStore.logger.Println("SQLite Cache Manager goroutine started.")

	cleanupInterval := 6 * time.Hour
	if retentionDays == 0 {
		cleanupInterval = 24 * 365 * time.Hour
		logStore.logger.Println("SQLite Cache Manager: Regular pruning disabled (retention is 0 days).")
	}
	cleanupTicker := time.NewTicker(cleanupInterval)
	defer cleanupTicker.Stop()

	dbSizeCheckInterval := 15 * time.Minute
	if maxLogFileSizeMB == nil || *maxLogFileSizeMB == 0 {
		dbSizeCheckInterval = 24 * 365 * time.Hour
		logStore.logger.Println("SQLite Cache Manager: DB size limit check disabled (no limit set).")
	}
	dbSizeCheckTicker := time.NewTicker(dbSizeCheckInterval)
	defer dbSizeCheckTicker.Stop()

	for {
		select {
		case event, ok := <-logEventsIn:
			if !ok {
				logStore.logger.Println("CACHE_MGR: Log event input channel closed.")
				return
			}
			if err := logStore.AddLogEvent(event); err != nil {
				logStore.logger.Printf("CACHE_MGR: Error adding LogEvent (ID: %s) to SQLite: %v", event.ID, err)
			}

		case <-cleanupTicker.C:
			if retentionDays > 0 {
				_, err := logStore.PruneOldLogs(retentionDays)
				if err != nil {
					logStore.logger.Printf("CACHE_MGR: Error during regular log pruning: %v", err)
				}
			}

		case <-dbSizeCheckTicker.C:
			logStore.CheckAndEnforceDBSize(maxLogFileSizeMB)

		case <-internalQuit:
			logStore.logger.Println("SQLite Cache Manager goroutine stopping.")
			return
		}
	}
}
