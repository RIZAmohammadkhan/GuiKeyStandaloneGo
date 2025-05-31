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
