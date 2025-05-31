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
