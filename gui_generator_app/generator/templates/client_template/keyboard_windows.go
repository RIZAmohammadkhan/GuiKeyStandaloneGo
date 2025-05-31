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
