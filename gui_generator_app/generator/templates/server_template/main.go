// GuiKeyStandaloneGo/generator/templates/server_template/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

var serverGlobalLogger *log.Logger

func setupServerLogger() {
	logFileName := CfgInternalLogFileName // This comes from config_generated.go
	logDir := CfgInternalLogFileDir       // This comes from config_generated.go

	absLogDir := logDir
	if !filepath.IsAbs(logDir) {
		exePath, err := os.Executable()
		if err != nil {
			// Fallback if getting executable path fails, though unlikely
			fmt.Fprintf(os.Stderr, "SERVER CRITICAL: Cannot get executable path for logger: %v. Using current directory for logs.\n", err)
			absLogDir = "."
		} else {
			absLogDir = filepath.Join(filepath.Dir(exePath), logDir)
		}
	}

	if err := os.MkdirAll(absLogDir, 0755); err != nil {
		// Fallback logging if directory creation fails
		fallbackFile, ferr := os.OpenFile("server_critical_setup.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if ferr == nil {
			log.SetOutput(fallbackFile) // Temporarily set output
			defer fallbackFile.Close()
		}
		log.Fatalf("SERVER CRITICAL: Failed to create server log directory %s: %v", absLogDir, err)
	}

	logPath := filepath.Join(absLogDir, logFileName)

	// Configure lumberjack for diagnostic log rotation
	ljLogger := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    int(CfgDiagnosticLogMaxSizeMB), // From generated config
		MaxBackups: CfgDiagnosticLogMaxBackups,     // From generated config
		MaxAge:     CfgDiagnosticLogMaxAgeDays,     // From generated config
		Compress:   false,                          // Optional: compress rotated files
		LocalTime:  true,                           // Use local time for timestamps in log files
	}
	serverGlobalLogger = log.New(ljLogger, "SERVER: ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile) // Added Lmicroseconds
	serverGlobalLogger.Printf(
		"Server diagnostic logger initialized (Level: %s). Log file: %s, MaxSizeMB: %d, MaxBackups: %d, MaxAgeDays: %d",
		CfgInternalLogLevel, logPath, CfgDiagnosticLogMaxSizeMB, CfgDiagnosticLogMaxBackups, CfgDiagnosticLogMaxAgeDays,
	)
}

func main() {
	setupServerLogger()

	serverGlobalLogger.Printf("Local Log Server starting. Version: DEV. (Embedded Config Active)")
	// REMOVED: serverGlobalLogger.Printf("P2P Listen Address: %s", CfgP2PListenAddress)
	// The actual P2P listen addresses are now determined and logged by the P2PManager.
	serverGlobalLogger.Printf("P2P Listen Strategy: Dynamic TCP and QUIC ports for IPv4/IPv6.")
	serverGlobalLogger.Printf("Web UI Listen Address: %s", CfgWebUIListenAddress) // This is fine, it's for the web server
	serverGlobalLogger.Printf("Database Path: %s", CfgDatabasePath)
	serverGlobalLogger.Printf("Log Retention Days (for DB): %d", CfgLogRetentionDays)
	serverGlobalLogger.Printf("Server Bootstrap Addresses: %v", CfgBootstrapAddresses) // This is fine

	dbPathServer := CfgDatabasePath
	if !filepath.IsAbs(dbPathServer) {
		exePath, err := os.Executable()
		if err != nil {
			serverGlobalLogger.Fatalf("SERVER CRITICAL: Failed to get executable path for DB: %v", err)
		}
		dbPathServer = filepath.Join(filepath.Dir(exePath), CfgDatabasePath)
	}
	serverLogStore, err := NewServerLogStore(dbPathServer, serverGlobalLogger)
	if err != nil {
		serverGlobalLogger.Fatalf("SERVER CRITICAL: Failed to initialize ServerLogStore: %v", err)
	}
	defer func() {
		if errClose := serverLogStore.Close(); errClose != nil {
			serverGlobalLogger.Printf("Error closing ServerLogStore: %v", errClose)
		}
	}()

	serverP2PMan, err := NewServerP2PManager(
		serverGlobalLogger,
		"", // Pass empty string for old listenAddrStr, as it's now ignored
		CfgServerIdentityKeySeedHex,
		CfgBootstrapAddresses,
		serverLogStore,
		CfgEncryptionKeyHex,
	)
	if err != nil {
		serverGlobalLogger.Fatalf("SERVER CRITICAL: Failed to initialize ServerP2PManager: %v", err)
	}
	// Defer P2PManager close AFTER other components that might depend on it (though usually it's the other way)
	// For safety, P2P manager should be closed after HTTP server to allow graceful connection handling.
	// The actual close order is managed by WaitGroup and internalQuitChan signal.

	exePath, _ := os.Executable() // Get exePath again if needed for templates
	templateDir := filepath.Join(filepath.Dir(exePath), "web_templates")
	if err := LoadTemplates(templateDir, serverGlobalLogger); err != nil {
		serverGlobalLogger.Fatalf("SERVER CRITICAL: Failed to load HTML templates from %s: %v", templateDir, err)
	}

	osQuitChan := make(chan os.Signal, 1)
	signal.Notify(osQuitChan, syscall.SIGINT, syscall.SIGTERM)
	internalQuitChan := make(chan struct{})
	var wg sync.WaitGroup

	// Start P2P Manager
	wg.Add(1)
	go runServerP2PManager(serverP2PMan, internalQuitChan, &wg, serverGlobalLogger)

	// Start LogStore Manager
	wg.Add(1)
	pruneInterval := time.Duration(CfgLogDeletionCheckIntervalHrs) * time.Hour
	if CfgLogDeletionCheckIntervalHrs == 0 { // Handle case where interval is 0 (effectively disable)
		pruneInterval = 24 * 365 * time.Hour // A very long time
		serverGlobalLogger.Println("Server LogStore DB pruning effectively disabled (interval is 0).")
	}
	go runServerLogStoreManager(serverLogStore, internalQuitChan, &wg, CfgLogRetentionDays, pruneInterval)

	// Setup and Start Web UI Server
	webUIEnv := &WebUIEnv{
		LogStore:     serverLogStore,
		ServerPeerID: serverP2PMan.host.ID().String(), // Get PeerID from the active P2PManager host
		Logger:       serverGlobalLogger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler) // Redirects to /logs
	mux.HandleFunc("/logs", webUIEnv.viewLogsHandler)

	staticFileDir := filepath.Join(filepath.Dir(exePath), "static")
	fileServer := http.FileServer(http.Dir(staticFileDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	httpServer := &http.Server{
		Addr:         CfgWebUIListenAddress,
		Handler:      mux,
		ReadTimeout:  15 * time.Second, // Adjusted timeouts
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	wg.Add(1) // For the HTTP server listener goroutine
	go func() {
		defer wg.Done()
		serverGlobalLogger.Printf("Web UI server starting to listen on http://%s", CfgWebUIListenAddress)
		if errSrv := httpServer.ListenAndServe(); errSrv != nil && errSrv != http.ErrServerClosed {
			serverGlobalLogger.Fatalf("Web UI ListenAndServe error: %v", errSrv) // Use Fatalf to exit if server can't start
		}
		serverGlobalLogger.Println("Web UI server listener stopped.")
	}()

	// Goroutine for graceful HTTP server shutdown
	go func() {
		<-internalQuitChan // Wait for internal quit signal
		serverGlobalLogger.Println("Web UI server: Shutdown signal received, attempting graceful shutdown...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if errShutdown := httpServer.Shutdown(shutdownCtx); errShutdown != nil {
			serverGlobalLogger.Printf("Web UI server graceful shutdown error: %v", errShutdown)
		}
		serverGlobalLogger.Println("Web UI server shut down.")
	}()

	serverGlobalLogger.Println("Server main components initialized. Waiting for shutdown signal...")
	<-osQuitChan // Wait for OS signal (Ctrl+C)
	serverGlobalLogger.Println("OS shutdown signal received. Signaling goroutines to stop...")
	close(internalQuitChan) // Signal all dependent goroutines to stop

	// Defer P2P manager close here to ensure it's one of the last things
	// This allows active connections to potentially finish sending/receiving before host closes
	defer func() {
		serverGlobalLogger.Println("Closing P2P Manager as part of main shutdown...")
		if err := serverP2PMan.Close(); err != nil {
			serverGlobalLogger.Printf("Error closing P2PManager during main shutdown: %v", err)
		}
		serverGlobalLogger.Println("P2P Manager closed.")
	}()

	serverGlobalLogger.Println("Waiting for server goroutines to complete...")
	wg.Wait() // Wait for all goroutines (HTTP server, log store manager, P2P manager controller)

	serverGlobalLogger.Println("All server components shut down gracefully.")
}
