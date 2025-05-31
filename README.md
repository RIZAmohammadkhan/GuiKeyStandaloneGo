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