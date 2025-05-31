// GuiKeyStandaloneGo/generator/templates/server_template/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http" // For Web UI
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

var serverGlobalLogger *log.Logger

func setupServerLogger() {
	logFileName := CfgInternalLogFileName
	logDir := CfgInternalLogFileDir

	absLogDir := logDir
	if !filepath.IsAbs(logDir) {
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "SERVER CRITICAL: Cannot get executable path for logger: %v. Using current directory for logs.\n", err)
			absLogDir = "." // Fallback
		} else {
			absLogDir = filepath.Join(filepath.Dir(exePath), logDir)
		}
	}

	if err := os.MkdirAll(absLogDir, 0755); err != nil {
		fallbackFile, ferr := os.OpenFile("server_critical_setup.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if ferr == nil {
			log.SetOutput(fallbackFile)
			defer fallbackFile.Close()
		}
		log.Fatalf("SERVER CRITICAL: Failed to create server log directory %s: %v", absLogDir, err)
	}

	logPath := filepath.Join(absLogDir, logFileName)
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("SERVER CRITICAL: Failed to open server log file %s: %v", logPath, err)
	}
	serverGlobalLogger = log.New(file, "SERVER: ", log.LstdFlags|log.Lshortfile)
	serverGlobalLogger.Printf("Server logger initialized (Level: %s). Log file: %s", CfgInternalLogLevel, logPath)
}

func main() {
	setupServerLogger()

	serverGlobalLogger.Printf("Local Log Server starting. Version: DEV. (Embedded Config Active)")
	serverGlobalLogger.Printf("P2P Listen Address: %s", CfgP2PListenAddress)
	serverGlobalLogger.Printf("Web UI Listen Address: %s", CfgWebUIListenAddress)
	serverGlobalLogger.Printf("Database Path: %s", CfgDatabasePath)
	serverGlobalLogger.Printf("Log Retention Days: %d", CfgLogRetentionDays)
	serverGlobalLogger.Printf("Server Bootstrap Addresses: %v", CfgBootstrapAddresses)

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
		CfgP2PListenAddress,
		CfgServerIdentityKeySeedHex,
		CfgBootstrapAddresses,
		serverLogStore,
		CfgEncryptionKeyHex,
	)
	if err != nil {
		serverGlobalLogger.Fatalf("SERVER CRITICAL: Failed to initialize ServerP2PManager: %v", err)
	}
	defer serverP2PMan.Close()

	exePath, _ := os.Executable()
	templateDir := filepath.Join(filepath.Dir(exePath), "web_templates")
	if err := LoadTemplates(templateDir, serverGlobalLogger); err != nil {
		serverGlobalLogger.Fatalf("SERVER CRITICAL: Failed to load HTML templates from %s: %v", templateDir, err)
	}

	osQuitChan := make(chan os.Signal, 1)
	signal.Notify(osQuitChan, syscall.SIGINT, syscall.SIGTERM)
	internalQuitChan := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go runServerP2PManager(serverP2PMan, internalQuitChan, &wg, serverGlobalLogger)

	wg.Add(1)
	pruneInterval := time.Duration(CfgLogDeletionCheckIntervalHrs) * time.Hour
	if CfgLogDeletionCheckIntervalHrs == 0 {
		pruneInterval = 24 * 365 * time.Hour
		serverGlobalLogger.Println("Server LogStore pruning disabled (interval is 0).")
	}
	go runServerLogStoreManager(serverLogStore, internalQuitChan, &wg, CfgLogRetentionDays, pruneInterval)

	webUIEnv := &WebUIEnv{
		LogStore:     serverLogStore,
		ServerPeerID: serverP2PMan.host.ID().String(),
		Logger:       serverGlobalLogger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/logs", webUIEnv.viewLogsHandler)

	staticFileDir := filepath.Join(filepath.Dir(exePath), "static")
	fileServer := http.FileServer(http.Dir(staticFileDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	httpServer := &http.Server{
		Addr:         CfgWebUIListenAddress,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		serverGlobalLogger.Printf("Web UI server starting to listen on http://%s", CfgWebUIListenAddress)
		if errSrv := httpServer.ListenAndServe(); errSrv != nil && errSrv != http.ErrServerClosed {
			serverGlobalLogger.Fatalf("Web UI ListenAndServe error: %v", errSrv)
		}
		serverGlobalLogger.Println("Web UI server listener stopped.")
	}()

	go func() {
		<-internalQuitChan
		serverGlobalLogger.Println("Web UI server: Shutdown signal received, attempting graceful shutdown...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if errShutdown := httpServer.Shutdown(shutdownCtx); errShutdown != nil {
			serverGlobalLogger.Printf("Web UI server graceful shutdown error: %v", errShutdown)
		}
		serverGlobalLogger.Println("Web UI server shut down.")
	}()

	serverGlobalLogger.Println("Server main components initialized. Waiting for shutdown signal...")
	<-osQuitChan
	serverGlobalLogger.Println("OS shutdown signal received. Signaling goroutines to stop...")
	close(internalQuitChan)
	serverGlobalLogger.Println("Waiting for server goroutines to complete...")
	wg.Wait()
	serverGlobalLogger.Println("All server components shut down gracefully.")
}
