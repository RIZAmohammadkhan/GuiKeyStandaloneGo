// File: GuiKeyStandaloneGo/gui_generator_app/main.go
package main

import (
	"context"
	"fmt"
	"image/color" // Import for color definitions
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/generator/core"
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/config"
)

// --- Custom Theme Definition ---

// Define our custom colors
var (
	colorBeige           = color.NRGBA{R: 0xF5, G: 0xF5, B: 0xDC, A: 0xff} // Main background
	colorSoftBlack       = color.NRGBA{R: 0x21, G: 0x21, B: 0x21, A: 0xff} // Text
	colorLightBrown      = color.NRGBA{R: 0xD2, G: 0xB4, B: 0x8C, A: 0xff} // Progress track, borders
	colorDarkBrown       = color.NRGBA{R: 0x8B, G: 0x45, B: 0x13, A: 0xff} // Progress value, primary
	colorMediumBrown     = color.NRGBA{R: 0xA0, G: 0x52, B: 0x2D, A: 0xff} // Hover
	colorMutedGrayBrown  = color.NRGBA{R: 0x9B, G: 0x87, B: 0x70, A: 0xff} // Disabled text, placeholder
	colorInputBackground = color.NRGBA{R: 0xFA, G: 0xF0, B: 0xE6, A: 0xff} // Linen for input fields
	colorCardBackground  = color.NRGBA{R: 0xFD, G: 0xF5, B: 0xE6, A: 0xff} // Old Lace, slightly off-beige for cards
)

// Custom Theme Definition - Compatible with older Fyne versions
type myAppTheme struct {
	fyne.Theme // Embed default theme (LightTheme in this case)
}

func newAppTheme() fyne.Theme {
	return &myAppTheme{Theme: theme.LightTheme()}
}

func (m *myAppTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	// We are building a light theme, so we mostly ignore 'variant' or handle it minimally.
	// If you wanted a dark variant of this theme, you'd check theme.VariantDark.

	switch name {
	case theme.ColorNameBackground:
		return colorBeige
	case theme.ColorNameButton: // Standard button face color
		return colorBeige // Could be a slightly different beige or light brown
	case theme.ColorNameDisabledButton:
		// A more desaturated/lighter version of the button color or light brown
		return color.NRGBA{R: 0xE0, G: 0xD5, B: 0xC0, A: 0xff}
	case theme.ColorNameDisabled:
		return colorMutedGrayBrown
	case theme.ColorNameError:
		return m.Theme.Color(name, variant) // Inherit error color from base LightTheme
	case theme.ColorNameFocus: // Focus highlight (e.g., around input fields)
		return colorDarkBrown
	case theme.ColorNameForeground: // Main text color
		return colorSoftBlack
	case theme.ColorNameHover: // Hover over interactive elements
		return colorMediumBrown
	case theme.ColorNameInputBackground:
		return colorInputBackground
	case theme.ColorNameInputBorder:
		return colorLightBrown
	case theme.ColorNamePlaceHolder:
		return colorMutedGrayBrown
	case theme.ColorNamePrimary: // Primary actions, progress bar value (fallback)
		return colorDarkBrown
	case theme.ColorNameScrollBar:
		return colorLightBrown
	case theme.ColorNameShadow:
		// A subtle darker beige or very light brown for shadows
		return color.NRGBA{R: 0xD3, G: 0xC8, B: 0xB6, A: 0x60} // Softer shadow
	case theme.ColorNameSelection: // Text selection background
		return color.NRGBA{R: 0xD2, G: 0xB4, B: 0x8C, A: 0x80} // LightBrown with some transparency
	case theme.ColorNameSuccess:
		return m.Theme.Color(name, variant) // Inherit success color
	case theme.ColorNameWarning:
		return m.Theme.Color(name, variant) // Inherit warning color
	default:
		// Fall back to the embedded theme (LightTheme) for any unhandled color names
		return m.Theme.Color(name, variant)
	}
}

// Implement other fyne.Theme interface methods by delegating to the embedded theme
func (m *myAppTheme) Font(style fyne.TextStyle) fyne.Resource {
	return m.Theme.Font(style)
}

func (m *myAppTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return m.Theme.Icon(name)
}

func (m *myAppTheme) Size(name fyne.ThemeSizeName) float32 {
	return m.Theme.Size(name)
}

// --- End Custom Theme Definition ---

type guiApp struct {
	// ... (rest of your guiApp struct remains the same)
	fyneApp fyne.App
	mainWin fyne.Window

	// Environment Preparation Section
	prepCard        *widget.Card
	prepStatusLabel *widget.Label
	prepProgressBar *widget.ProgressBar
	prepErrorLabel  *widget.Label

	// Configuration Section
	configCard          *widget.Card
	outputDirEntry      *widget.Entry
	browseButton        *widget.Button
	bootstrapAddrsEntry *widget.Entry
	generateButton      *widget.Button

	// Generation Progress Section
	genCard        *widget.Card
	genStatusLabel *widget.Label
	genProgressBar *widget.ProgressBar

	// Results Section
	resultsCard            *widget.Card
	resultsTitleLabel      *widget.Label
	resultsPeerIDText      *widget.Label
	peerIDInfoDisplay      *widget.Entry
	copyPeerIDButton       *widget.Button
	resultsOutputPathLabel *widget.Label
	readmeDisplay          *widget.Label // Changed from Entry to Label
	openOutputButton       *widget.Button
	finishButton           *widget.Button
	cleanupStatusLabel     *widget.Label // For "Cleaning up..." message

	// Shared data
	extractedGoExePath     string
	extractedTemplatesPath string
	cleanupGoSdkFunc       func() error
	cleanupTemplatesFunc   func() error
	selectedOutputDir      string
	generatedInfo          core.GeneratedInfo

	operationCtx    context.Context
	operationCancel context.CancelFunc

	isShuttingDown bool
	shutdownMutex  sync.Mutex
}

func main() {
	// ... (logging setup remains the same)
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("Failed to get user config directory: %v", err)
	}
	logDir := filepath.Join(configDir, "GuiKeyStandaloneGo", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}
	logFilePath := filepath.Join(logDir, "guikeygen_ui_embed.log")
	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("Failed to open log file %s: %v", logFilePath, err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	log.Printf("[main] Application starting with embedded resources. Logging to %s\n", logFilePath)

	fa := app.NewWithID("com.rizamohammadkhan.guikeygen.embed")
	fa.Settings().SetTheme(newAppTheme()) // APPLY THE CUSTOM THEME
	w := fa.NewWindow("GuiKey Standalone Package Generator")

	ui := &guiApp{
		fyneApp: fa,
		mainWin: w,
	}

	w.SetCloseIntercept(func() {
		log.Println("[main] Window close intercepted.")
		ui.triggerShutdown()
	})

	ui.setupUI()
	w.Resize(fyne.NewSize(800, 750))
	w.CenterOnScreen()

	go ui.prepareEnvironment()

	log.Print("[main] Entering Fyne main loop")
	w.ShowAndRun()
	log.Print("[main] Application exited.")
}

func (ui *guiApp) setupUI() {
	log.Print("[ui] Setting up single page UI")

	// Environment Preparation Section
	ui.prepStatusLabel = widget.NewLabel("Initializing: Preparing required resources...")
	ui.prepProgressBar = widget.NewProgressBar()
	ui.prepProgressBar.SetValue(0)
	ui.prepErrorLabel = widget.NewLabel("")
	ui.prepErrorLabel.Hide()
	ui.cleanupStatusLabel = widget.NewLabel("")
	ui.cleanupStatusLabel.Alignment = fyne.TextAlignCenter
	ui.cleanupStatusLabel.Hide()

	prepContent := container.NewVBox(
		ui.prepStatusLabel,
		ui.prepProgressBar,
		ui.prepErrorLabel,
		ui.cleanupStatusLabel,
	)
	ui.prepCard = widget.NewCard("Environment Setup", "", prepContent)

	// Configuration Section
	ui.outputDirEntry = widget.NewEntry()
	ui.outputDirEntry.SetPlaceHolder("Select output directory...")
	ui.outputDirEntry.Disable()

	ui.browseButton = widget.NewButtonWithIcon("Browse...", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				log.Printf("[ui-cfg] FolderOpenDialog error: %v", err)
				dialog.ShowError(err, ui.mainWin)
				return
			}
			if uri == nil {
				return
			}
			path := uri.Path()
			ui.outputDirEntry.SetText(path)
			ui.selectedOutputDir = path
			log.Printf("[ui-cfg] Output directory selected: %s", path)
		}, ui.mainWin)
	})
	ui.browseButton.Disable()

	defaultBootstrap := strings.Join(config.DefaultClientSettings().BootstrapAddresses, ",\n")
	ui.bootstrapAddrsEntry = widget.NewMultiLineEntry()
	ui.bootstrapAddrsEntry.SetText(defaultBootstrap)
	ui.bootstrapAddrsEntry.Wrapping = fyne.TextWrapWord
	ui.bootstrapAddrsEntry.SetMinRowsVisible(3)
	ui.bootstrapAddrsEntry.Disable()

	ui.generateButton = widget.NewButtonWithIcon("Generate Packages", theme.ConfirmIcon(), func() {
		ui.startGeneration()
	})
	ui.generateButton.Importance = widget.HighImportance // Make it use PrimaryColor
	ui.generateButton.Disable()

	outputDirBox := container.NewBorder(nil, nil, nil, ui.browseButton, ui.outputDirEntry)
	configContent := container.NewVBox(
		widget.NewLabel("Output Directory:"),
		outputDirBox,
		widget.NewLabel("Bootstrap Addresses (comma or newline separated):"),
		ui.bootstrapAddrsEntry,
		layout.NewSpacer(),
		container.NewCenter(ui.generateButton),
	)
	ui.configCard = widget.NewCard("Configuration", "Configure package generation settings", configContent)

	// Generation Progress Section
	ui.genStatusLabel = widget.NewLabel("Generation will start here...")
	ui.genProgressBar = widget.NewProgressBar()
	ui.genProgressBar.SetValue(0)

	genContent := container.NewVBox(
		ui.genStatusLabel,
		ui.genProgressBar,
	)
	ui.genCard = widget.NewCard("Generation Progress", "", genContent)
	ui.genCard.Hide()

	// Results Section
	ui.resultsTitleLabel = widget.NewLabel("Generation Status")
	ui.resultsTitleLabel.TextStyle.Bold = true
	ui.resultsTitleLabel.Alignment = fyne.TextAlignCenter

	ui.resultsPeerIDText = widget.NewLabel("Server PeerID:")
	ui.peerIDInfoDisplay = widget.NewEntry()
	ui.peerIDInfoDisplay.Disable()
	ui.peerIDInfoDisplay.Hide()

	ui.copyPeerIDButton = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		currentPeerID := ui.peerIDInfoDisplay.Text
		if currentPeerID != "" && currentPeerID != "Not available" {
			ui.mainWin.Clipboard().SetContent(currentPeerID)
			log.Printf("[ui] Copied PeerID to clipboard: %s", currentPeerID)
		}
	})
	ui.copyPeerIDButton.Hide()

	hscrollPeerID := container.NewHScroll(ui.peerIDInfoDisplay)
	peerIDLine := container.NewBorder(nil, nil, ui.resultsPeerIDText, ui.copyPeerIDButton, hscrollPeerID)

	ui.resultsOutputPathLabel = widget.NewLabel("Output Path: ")
	ui.resultsOutputPathLabel.Wrapping = fyne.TextWrapBreak

	ui.readmeDisplay = widget.NewLabel("")
	ui.readmeDisplay.Wrapping = fyne.TextWrapWord

	readmeScroll := container.NewScroll(ui.readmeDisplay)
	readmeScroll.SetMinSize(fyne.NewSize(0, 250))

	ui.openOutputButton = widget.NewButtonWithIcon("Open Output Folder", theme.FolderIcon(), func() {
		if ui.generatedInfo.FullOutputDir != "" {
			dialog.ShowInformation("Output Folder Location",
				"Packages generated in:\n"+ui.generatedInfo.FullOutputDir+"\n\nPlease navigate there using your file explorer.", ui.mainWin)
		}
	})

	ui.finishButton = widget.NewButtonWithIcon("Finish", theme.LogoutIcon(), func() {
		log.Print("[ui] Finish button clicked.")
		ui.triggerShutdown()
	})

	buttonBox := container.NewHBox(
		layout.NewSpacer(),
		ui.openOutputButton,
		ui.finishButton,
		layout.NewSpacer(),
	)

	resultsContent := container.NewVBox(
		ui.resultsTitleLabel,
		widget.NewSeparator(),
		peerIDLine,
		ui.resultsOutputPathLabel,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Instructions & Details:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		readmeScroll,
		layout.NewSpacer(),
		buttonBox,
	)
	ui.resultsCard = widget.NewCard("Results", "", resultsContent)
	ui.resultsCard.Hide()

	mainContent := container.NewVBox(
		ui.prepCard,
		ui.configCard,
		ui.genCard,
		ui.resultsCard,
	)

	scrollContainer := container.NewScroll(mainContent)
	ui.mainWin.SetContent(container.NewPadded(scrollContainer))
}

// ... (rest of your methods: prepareEnvironment, updatePrepStatus, handlePrepError, enableConfigurationSection, startGeneration, performGeneration, updateGenStatus, handleGenError, showResults, cleanupResources, triggerShutdown)
// Ensure these methods remain as they were in the previous correct version, using fyne.Do for UI updates from goroutines.
// For brevity, I'm not repeating them here, but they should be the same as in the version before this theme change.
// Make sure to copy them from the previous correct response. I will paste them below this block.

func (ui *guiApp) prepareEnvironment() {
	log.Println("[prep] Starting environment preparation")
	ui.operationCtx, ui.operationCancel = context.WithCancel(context.Background())

	fyne.Do(func() {
		ui.updatePrepStatus("Preparing Go compiler...", 0)
	})

	goExe, cleanupGo, errGo := GetGoExecutablePath(ui.operationCtx, func(msg string, pct int) {
		displayPct := int(float64(pct) * 0.5)
		fyne.Do(func() { ui.updatePrepStatus(msg, displayPct) })
	})

	if errGo != nil {
		if errGo == context.Canceled {
			fyne.Do(func() { ui.handlePrepError("Environment preparation canceled.") })
		} else {
			fyne.Do(func() { ui.handlePrepError(fmt.Sprintf("Failed to prepare Go compiler: %v", errGo)) })
		}
		return
	}
	ui.extractedGoExePath = goExe
	ui.cleanupGoSdkFunc = cleanupGo

	fyne.Do(func() {
		ui.updatePrepStatus("Preparing source templates...", 50)
	})

	templatesPath, cleanupTpl, errTpl := GetTemporaryModulePath(ui.operationCtx, func(msg string, pct int) {
		displayPct := 50 + int(float64(pct)*0.5)
		fyne.Do(func() { ui.updatePrepStatus(msg, displayPct) })
	})

	if errTpl != nil {
		if errTpl == context.Canceled {
			fyne.Do(func() { ui.handlePrepError("Environment preparation canceled.") })
		} else {
			fyne.Do(func() { ui.handlePrepError(fmt.Sprintf("Failed to prepare source templates: %v", errTpl)) })
		}
		return
	}
	ui.extractedTemplatesPath = templatesPath
	ui.cleanupTemplatesFunc = cleanupTpl

	fyne.Do(func() {
		ui.updatePrepStatus("Environment ready! You can now configure generation settings.", 100)
		ui.enableConfigurationSection()
	})
	log.Println("[prep] Environment preparation completed successfully")
}

func (ui *guiApp) updatePrepStatus(message string, percentage int) {
	if ui.prepStatusLabel == nil || ui.prepProgressBar == nil {
		return
	}
	ui.prepStatusLabel.SetText(message)
	if percentage >= 0 && percentage <= 100 {
		ui.prepProgressBar.SetValue(float64(percentage) / 100.0)
	}
}

func (ui *guiApp) handlePrepError(errMsg string) {
	log.Printf("[prep] Error: %s", errMsg)
	ui.prepStatusLabel.SetText("Environment setup failed!")
	ui.prepProgressBar.Hide()
	ui.prepErrorLabel.SetText("Error: " + errMsg + "\n\nPlease restart the application.")
	ui.prepErrorLabel.Show()
}

func (ui *guiApp) enableConfigurationSection() {
	log.Print("[ui] Enabling configuration section")
	ui.outputDirEntry.Enable()
	ui.browseButton.Enable()
	ui.bootstrapAddrsEntry.Enable()
	ui.generateButton.Enable()
	ui.configCard.SetTitle("Configuration (Ready)")
	ui.configCard.Refresh()
}

func (ui *guiApp) startGeneration() {
	log.Print("[gen] Starting package generation")

	if ui.selectedOutputDir == "" {
		dialog.ShowInformation("Input Required", "Please select an output directory.", ui.mainWin)
		return
	}

	if _, err := os.Stat(ui.selectedOutputDir); os.IsNotExist(err) {
		dialog.ShowConfirm("Create Directory?",
			fmt.Sprintf("The output directory '%s' does not exist.\nWould you like to create it?", ui.selectedOutputDir),
			func(create bool) {
				if !create {
					return
				}
				if errMkdir := os.MkdirAll(ui.selectedOutputDir, 0755); errMkdir != nil {
					dialog.ShowError(fmt.Errorf("failed to create directory: %w", errMkdir), ui.mainWin)
					return
				}
				ui.performGeneration()
			}, ui.mainWin)
	} else {
		ui.performGeneration()
	}
}

func (ui *guiApp) performGeneration() {
	log.Print("[gen] Performing package generation")

	ui.genCard.Show()
	ui.generateButton.Disable()
	ui.browseButton.Disable()
	ui.bootstrapAddrsEntry.Disable()
	ui.outputDirEntry.Disable()
	ui.configCard.Refresh()

	bootstrapSplit := strings.FieldsFunc(ui.bootstrapAddrsEntry.Text, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	var bootstrapCleaned []string
	for _, addr := range bootstrapSplit {
		trimmed := strings.TrimSpace(addr)
		if trimmed != "" {
			bootstrapCleaned = append(bootstrapCleaned, trimmed)
		}
	}
	bootstrapFinal := strings.Join(bootstrapCleaned, ",")

	go func() {
		templatesBasePath := filepath.Join(ui.extractedTemplatesPath, "generator", "templates")
		settings := core.GeneratorSettings{
			OutputDirPath:      ui.selectedOutputDir,
			BootstrapAddresses: bootstrapFinal,
			GoExecutablePath:   ui.extractedGoExePath,
			TempModuleRootPath: ui.extractedTemplatesPath,
			TemplatesBasePath:  templatesBasePath,
			ProgressCallback: func(message string, percentage int) {
				fyne.Do(func() {
					ui.updateGenStatus(message, percentage)
				})
			},
		}

		generatedInfo, err := core.PerformGeneration(&settings)

		fyne.Do(func() {
			if err != nil {
				ui.handleGenError(fmt.Sprintf("Generation failed: %v", err))
				return
			}
			ui.generatedInfo = generatedInfo
			ui.showResults()
		})
	}()
}
func (ui *guiApp) updateGenStatus(message string, percentage int) {
	if ui.genStatusLabel == nil || ui.genProgressBar == nil {
		return
	}
	ui.genStatusLabel.SetText(message)
	if percentage >= 0 && percentage <= 100 {
		ui.genProgressBar.SetValue(float64(percentage) / 100.0)
	}
}

func (ui *guiApp) handleGenError(errMsg string) {
	log.Printf("[gen] Error: %s", errMsg)
	ui.genStatusLabel.SetText("Generation failed!")
	dialog.ShowError(fmt.Errorf(errMsg), ui.mainWin)

	ui.generateButton.Enable()
	ui.browseButton.Enable()
	ui.bootstrapAddrsEntry.Enable()
	ui.outputDirEntry.Enable()
	ui.configCard.Refresh()
}

func (ui *guiApp) showResults() {
	log.Print("[ui] Showing generation results")

	ui.genStatusLabel.SetText("Generation completed successfully!")
	ui.genProgressBar.SetValue(1.0)

	if ui.generatedInfo.ReadmeContent == "" {
		ui.readmeDisplay.SetText("Error: README content not available. Check logs.")
	} else {
		ui.readmeDisplay.SetText(ui.generatedInfo.ReadmeContent)
	}
	ui.readmeDisplay.Refresh()

	ui.resultsTitleLabel.SetText("Generation Completed Successfully!")

	if ui.generatedInfo.ServerPeerID != "" {
		ui.peerIDInfoDisplay.SetText(ui.generatedInfo.ServerPeerID)
		ui.copyPeerIDButton.Show()
		ui.peerIDInfoDisplay.Show()
	} else {
		ui.peerIDInfoDisplay.SetText("Not available")
		ui.copyPeerIDButton.Hide()
		ui.peerIDInfoDisplay.Show()
	}
	ui.peerIDInfoDisplay.Refresh()
	ui.copyPeerIDButton.Refresh()

	if ui.generatedInfo.FullOutputDir != "" {
		ui.resultsOutputPathLabel.SetText(fmt.Sprintf("Output Path: %s", ui.generatedInfo.FullOutputDir))
	} else {
		ui.resultsOutputPathLabel.SetText("Output Path: Not available")
	}
	ui.resultsOutputPathLabel.Refresh()

	ui.resultsCard.Show()
	ui.resultsCard.Refresh()

	time.AfterFunc(100*time.Millisecond, func() {
		fyne.Do(func() {
			if paddedContainer, ok := ui.mainWin.Content().(*fyne.Container); ok {
				if len(paddedContainer.Objects) > 0 {
					if scrollContainer, ok := paddedContainer.Objects[0].(*container.Scroll); ok {
						scrollContainer.ScrollToBottom()
					}
				}
			}
		})
	})
}

func (ui *guiApp) cleanupResources() {
	log.Println("[cleanup] Cleaning up resources")
	if ui.operationCancel != nil {
		ui.operationCancel()
	}
	log.Println("[cleanup] Cleaning up Go SDK...")
	if ui.cleanupGoSdkFunc != nil {
		if err := ui.cleanupGoSdkFunc(); err != nil {
			log.Printf("[cleanup] Error cleaning up Go SDK: %v", err)
		}
	}
	log.Println("[cleanup] Cleaning up templates...")
	if ui.cleanupTemplatesFunc != nil {
		if err := ui.cleanupTemplatesFunc(); err != nil {
			log.Printf("[cleanup] Error cleaning up templates: %v", err)
		}
	}
	log.Println("[cleanup] Resource cleanup attempt finished.")
}

func (ui *guiApp) triggerShutdown() {
	ui.shutdownMutex.Lock()
	if ui.isShuttingDown {
		ui.shutdownMutex.Unlock()
		log.Println("[shutdown] Shutdown already in progress.")
		return
	}
	ui.isShuttingDown = true
	ui.shutdownMutex.Unlock()

	log.Println("[shutdown] Triggering application shutdown sequence.")

	if ui.finishButton != nil {
		ui.finishButton.Disable()
	}
	if ui.openOutputButton != nil {
		ui.openOutputButton.Disable()
	}
	if ui.generateButton != nil {
		ui.generateButton.Disable()
	}
	fyne.Do(func() {
		if ui.cleanupStatusLabel != nil {
			ui.cleanupStatusLabel.SetText("Cleaning up resources, please wait...\nThe application will close automatically.")
			ui.cleanupStatusLabel.Show()
		}
		if ui.prepCard != nil {
			ui.prepCard.Refresh()
		}
		if ui.configCard != nil {
			ui.configCard.Hide()
		}
		if ui.genCard != nil {
			ui.genCard.Hide()
		}
		if ui.resultsCard != nil {
			ui.resultsCard.Hide()
		}
	})

	go func() {
		ui.cleanupResources()
		fyne.Do(func() {
			log.Println("[shutdown] Cleanup finished. Quitting application.")
			ui.fyneApp.Quit()
		})
	}()
}
