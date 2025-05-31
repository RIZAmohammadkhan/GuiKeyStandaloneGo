
## Building and Running the GUI Package Generator

### Prerequisites (for building the GUI Generator *itself*):

*   Go (e.g., version 1.21+).
*   Fyne command-line tool: `go install fyne.io/fyne/v2/cmd/fyne@latest`
*   (Windows) A C compiler like MinGW (e.g., via TDM-GCC or MSYS2) is recommended in your PATH for Fyne development and packaging, though often not strictly required for basic builds.

### Steps to Build `gui_generator_app.exe`:

1.  **Place Go SDK for Embedding:**
    *   Download the **Go ZIP archive for Windows (amd64)** (e.g., `go1.22.0.windows-amd64.zip`) from [https://go.dev/dl/](https://go.dev/dl/).
    *   Create the directory `GuiKeyStandaloneGo/gui_generator_app/embedded_go_sdk/`.
    *   Place the downloaded Go ZIP file into this `embedded_go_sdk/` directory.
    *   **CRITICAL:** Open `GuiKeyStandaloneGo/gui_generator_app/resources.go` and update the `embeddedGoSDKZipName` and `embeddedGoSDKSHA256` constants to match the exact filename and SHA256 checksum of the Go ZIP file you placed.

2.  **Navigate to the `gui_generator_app` directory:**
    ```bash
    cd path/to/your/project/GuiKeyStandaloneGo/gui_generator_app
    ```

3.  **Ensure Dependencies are Tidy:**
    ```bash
    go mod tidy
    ```
    (Also run `go mod tidy` in the project root `GuiKeyStandaloneGo/` if you've made changes to `pkg/` or `generator/core/` dependencies.)

4.  **Build the Executable:**
    *   **Recommended (for distribution, includes icon and manifest):**
        ```bash
        fyne package -os windows
        ```
        This will create `YourAppName.exe` (e.g., "GuiKey Standalone Package Generator.exe" based on `FyneApp.toml`) in the current directory (`gui_generator_app/`).
    *   **Simple Build (for testing):**
        ```bash
        go build -o GuiKeyGenerator.exe -ldflags="-H windowsgui"
        ```

### Running the GUI Package Generator (`GuiKeyGenerator.exe`):

1.  Locate the generated `GuiKeyGenerator.exe`.
2.  Double-click to run it. No prior Go installation is needed on the user's machine.
3.  **First Run:** The application will take some time during "Step 1: Preparing Environment" as it extracts the embedded Go compiler and source templates to temporary locations. This happens once per application session.
4.  Follow the on-screen steps:
    *   **Step 1:** Wait for environment preparation to complete.
    *   **Step 2:** Configure the output directory and (optionally) bootstrap peer addresses.
    *   **Step 3:** Click "Generate Packages" and wait for the client and server binaries to be compiled.
    *   **Step 4:** View the generated instructions. The packages will be in your chosen output directory.
5.  The application will attempt to clean up temporary files on exit. Check the application logs for details (path usually mentioned in console output on start or found in user's config directory).

## Using the Generated Client and Server Packages

After the GUI Generator creates the packages:

1.  Follow the instructions in the `README_IMPORTANT_INSTRUCTIONS.txt` file located in your chosen output directory.
2.  Typically, this involves:
    *   Deploying the `LocalLogServer_Package` contents to your server machine and running `local_log_server.exe`.
    *   Deploying the `ActivityMonitorClient_Package` contents to target Windows machines and running `activity_monitor_client.exe`.

## Key Design Choices & Features

*   **Standalone Generator:** The primary goal of making the generator usable by non-developers without needing a Go environment is achieved by embedding the compiler and templates.
*   **Pure Go Client/Server:** Utilizes `modernc.org/sqlite`, removing CGo dependencies from the generated client and server, simplifying their compilation by the embedded Go.
*   **Multi-Step GUI:** The Fyne application provides a clear, step-by-step process for package generation.
*   **Robust Error Handling & Logging:** The GUI application includes logging to a file to help diagnose issues during resource preparation or package generation.
*   **Temporary File Management:** Embedded resources are extracted to temporary locations and cleaned up on application exit.

## Future Work & Potential Enhancements

*   **GUI Generator:**
    *   More granular control over client/server configuration options in the GUI.
    *   Ability to save/load generation profiles.
    *   Cross-compilation options (if embedding Go toolchains for other OS/Arch becomes feasible and desired).
    *   "Open Output Folder" button with native OS integration.
*   **Client/Server:**
    *   Enhanced logging (structured, leveled logging like Zerolog/Zap).
    *   More advanced Web UI features for the server (filtering, searching, user accounts).
    *   Further hardening of P2P connectivity and security.
    *   Comprehensive automated testing suite.