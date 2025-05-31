// GuiKeyStandaloneGo/generator/templates/server_template/log_store_sqlite.go
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time" // For PruneOldLogs

	"guikeystandalonego/pkg/types" // Shared LogEvent type
	// Pure Go SQLite driver will be added by `go mod tidy`
	// For event IDs if needed for querying specific events
)

// Driver name for modernc.org/sqlite
const serverDBDriverName = "sqlite"

type ServerLogStore struct {
	db     *sql.DB
	logger *log.Logger
	dbPath string
}

func NewServerLogStore(dbFilePath string, logger *log.Logger) (*ServerLogStore, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "SERVER_LOGSTORE_FALLBACK: ", log.LstdFlags|log.Lshortfile)
	}

	dbDir := filepath.Dir(dbFilePath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("server_logstore: failed to create database directory %s: %w", dbDir, err)
	}

	// DSN for modernc.org/sqlite, includes PRAGMAs
	dsn := fmt.Sprintf("file:%s?cache=shared&mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", dbFilePath)

	// *** THIS IS THE LINE TO FIX ***
	// db, err := sql.Open("sqlite3", dsn) // OLD, WRONG DRIVER
	db, err := sql.Open(serverDBDriverName, dsn) // CORRECTED: Use the constant for modernc.org/sqlite
	if err != nil {
		return nil, fmt.Errorf("server_logstore: failed to open SQLite database at %s: %w", dbFilePath, err)
	}

	// Pragmas are now part of the DSN, no need for separate Exec calls for them.

	store := &ServerLogStore{
		db:     db,
		logger: logger,
		dbPath: dbFilePath,
	}

	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("server_logstore: failed to initialize schema: %w", err)
	}

	logger.Printf("ServerLogStore: SQLite initialized. DB: %s", dbFilePath)
	return store, nil
}

func (s *ServerLogStore) initSchema() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS received_logs (
		event_id TEXT PRIMARY KEY,        
		client_id TEXT NOT NULL,          
		received_at INTEGER NOT NULL,     
		event_timestamp INTEGER NOT NULL, 
		application_name TEXT,
		initial_window_title TEXT,
		schema_version INTEGER,      
		full_log_event_json TEXT NOT NULL 
	);
	CREATE INDEX IF NOT EXISTS idx_received_logs_client_id ON received_logs (client_id);
	CREATE INDEX IF NOT EXISTS idx_received_logs_event_timestamp ON received_logs (event_timestamp);
	CREATE INDEX IF NOT EXISTS idx_received_logs_received_at ON received_logs (received_at);
	CREATE INDEX IF NOT EXISTS idx_received_logs_app_name ON received_logs (application_name);
	`
	_, err := s.db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("server_logstore: failed to create received_logs table: %w", err)
	}
	s.logger.Println("ServerLogStore: SQLite schema initialized/verified.")
	return nil
}

func (s *ServerLogStore) AddReceivedLogEvents(events []types.LogEvent, receivedTime time.Time) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("server_logstore: failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO received_logs (
			event_id, client_id, received_at, event_timestamp, 
			application_name, initial_window_title, schema_version, full_log_event_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(event_id) DO NOTHING`)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("server_logstore: failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	insertedCount := 0
	for _, event := range events {
		jsonData, jsonErr := json.Marshal(event)
		if jsonErr != nil {
			s.logger.Printf("ServerLogStore: Error marshaling event ID %s to JSON, skipping: %v", event.ID, jsonErr)
			continue
		}

		res, execErr := stmt.Exec(
			event.ID.String(),
			event.ClientID.String(),
			receivedTime.Unix(),
			event.Timestamp.Unix(),
			event.ApplicationName,
			event.InitialWindowTitle,
			event.SchemaVersion,
			string(jsonData),
		)
		if execErr != nil {
			s.logger.Printf("ServerLogStore: Error inserting event ID %s: %v", event.ID, execErr)
			tx.Rollback()
			return insertedCount, fmt.Errorf("server_logstore: error inserting event ID %s (rolled back): %w", event.ID, execErr)
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			insertedCount++
		}
	}

	if err = tx.Commit(); err != nil {
		return insertedCount, fmt.Errorf("server_logstore: failed to commit transaction: %w", err)
	}

	s.logger.Printf("ServerLogStore: Added %d new log events to DB (out of %d received).", insertedCount, len(events))
	return insertedCount, nil
}

func (s *ServerLogStore) GetLogEventsPaginated(page uint, pageSize uint) ([]types.LogEvent, int64, error) {
	offset := (page - 1) * pageSize

	var totalCount int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM received_logs").Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("server_logstore: failed to count total logs: %w", err)
	}

	if totalCount == 0 {
		return []types.LogEvent{}, 0, nil
	}

	rows, err := s.db.Query(
		"SELECT full_log_event_json FROM received_logs ORDER BY event_timestamp DESC LIMIT ? OFFSET ?",
		pageSize,
		offset,
	)
	if err != nil {
		return nil, totalCount, fmt.Errorf("server_logstore: failed to query paginated logs: %w", err)
	}
	defer rows.Close()

	var events []types.LogEvent
	for rows.Next() {
		var jsonData string
		if err := rows.Scan(&jsonData); err != nil {
			s.logger.Printf("ServerLogStore: Error scanning log for UI: %v", err)
			continue
		}
		var event types.LogEvent
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			s.logger.Printf("ServerLogStore: Error unmarshaling log for UI (Data: %.50s...): %v", jsonData, err)
			continue
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, totalCount, fmt.Errorf("server_logstore: error iterating paginated log rows: %w", err)
	}
	return events, totalCount, nil
}

func (s *ServerLogStore) PruneOldLogs(retentionDays uint32) (int64, error) {
	if retentionDays == 0 {
		s.logger.Println("ServerLogStore: Log retention is indefinite (0 days), skipping pruning.")
		return 0, nil
	}

	cutoffTime := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	cutoffTimestampUnix := cutoffTime.Unix()

	s.logger.Printf("ServerLogStore: Pruning logs older than %d days (event_timestamp before %s / %d).",
		retentionDays, cutoffTime.Format(time.RFC3339), cutoffTimestampUnix)

	result, err := s.db.Exec("DELETE FROM received_logs WHERE event_timestamp < ?", cutoffTimestampUnix)
	if err != nil {
		return 0, fmt.Errorf("server_logstore: failed to execute prune old logs query: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		s.logger.Printf("ServerLogStore: Pruned %d old log entries.", rowsAffected)
	} else {
		s.logger.Println("ServerLogStore: No old log entries found to prune based on event_timestamp.")
	}
	return rowsAffected, nil
}

func (s *ServerLogStore) Close() error {
	if s.db != nil {
		s.logger.Println("ServerLogStore: Closing database connection.")
		return s.db.Close()
	}
	return nil
}

func runServerLogStoreManager(
	logStore *ServerLogStore,
	internalQuit <-chan struct{},
	wg *sync.WaitGroup,
	retentionDays uint32,
	pruneInterval time.Duration,
) {
	defer wg.Done()
	logStore.logger.Println("Server LogStore Manager goroutine started.")

	if retentionDays == 0 || pruneInterval == 0 {
		logStore.logger.Println("Server LogStore Manager: Pruning disabled (retention or interval is 0).")
		<-internalQuit
		logStore.logger.Println("Server LogStore Manager goroutine stopping (pruning was disabled).")
		return
	}

	pruneTicker := time.NewTicker(pruneInterval)
	defer pruneTicker.Stop()

	for {
		select {
		case <-pruneTicker.C:
			logStore.logger.Println("Server LogStore Manager: Running periodic log pruning...")
			_, err := logStore.PruneOldLogs(retentionDays)
			if err != nil {
				logStore.logger.Printf("Server LogStore Manager: Error during periodic log pruning: %v", err)
			}
		case <-internalQuit:
			logStore.logger.Println("Server LogStore Manager goroutine stopping.")
			return
		}
	}
}
