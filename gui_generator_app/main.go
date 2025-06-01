// File: GuiKeyStandaloneGo/gui_generator_app/main.go
package main // Ensure this is package main

import (
	"context"
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strconv"
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

	// core and config are from your project structure, relative to this main.go
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/generator/core"
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/config"
	// No import for "resources" package needed here.
	// The functions GetGoExecutablePath and GetTemporaryModulePath are now part of package main.
)

// --- Custom Theme Definition (copied from your previous code) ---
var (
	colorBeige           = color.NRGBA{R: 0xF5, G: 0xF5, B: 0xDC, A: 0xff}
	colorSoftBlack       = color.NRGBA{R: 0x21, G: 0x21, B: 0x21, A: 0xff}
	colorLightBrown      = color.NRGBA{R: 0xD2, G: 0xB4, B: 0x8C, A: 0xff}
	colorDarkBrown       = color.NRGBA{R: 0x8B, G: 0x45, B: 0x13, A: 0xff}
	colorMediumBrown     = color.NRGBA{R: 0xA0, G: 0x52, B: 0x2D, A: 0xff}
	colorMutedGrayBrown  = color.NRGBA{R: 0x9B, G: 0x87, B: 0x70, A: 0xff}
	colorInputBackground = color.NRGBA{R: 0xFA, G: 0xF0, B: 0xE6, A: 0xff}
	colorCardBackground  = color.NRGBA{R: 0xFD, G: 0xF5, B: 0xE6, A: 0xff}
)

type myAppTheme struct {
	fyne.Theme
}

func newAppTheme() fyne.Theme {
	return &myAppTheme{Theme: theme.LightTheme()}
}

func (m *myAppTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colorBeige
	case theme.ColorNameButton:
		return colorBeige
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 0xE0, G: 0xD5, B: 0xC0, A: 0xff}
	case theme.ColorNameDisabled:
		return colorMutedGrayBrown
	case theme.ColorNameFocus:
		return colorDarkBrown
	case theme.ColorNameForeground:
		return colorSoftBlack
	case theme.ColorNameHover:
		return colorMediumBrown
	case theme.ColorNameInputBackground:
		return colorInputBackground
	case theme.ColorNameInputBorder:
		return colorLightBrown
	case theme.ColorNamePlaceHolder:
		return colorMutedGrayBrown
	case theme.ColorNamePrimary:
		return colorDarkBrown
	case theme.ColorNameScrollBar:
		return colorLightBrown
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0xD3, G: 0xC8, B: 0xB6, A: 0x60}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0xD2, G: 0xB4, B: 0x8C, A: 0x80}
	default:
		return m.Theme.Color(name, variant)
	}
}
func (m *myAppTheme) Font(style fyne.TextStyle) fyne.Resource    { return m.Theme.Font(style) }
func (m *myAppTheme) Icon(name fyne.ThemeIconName) fyne.Resource { return m.Theme.Icon(name) }
func (m *myAppTheme) Size(name fyne.ThemeSizeName) float32       { return m.Theme.Size(name) }

// --- End Custom Theme Definition ---

type guiApp struct {
	fyneApp fyne.App
	mainWin fyne.Window

	prepCard           *widget.Card
	prepStatusLabel    *widget.Label
	prepProgressBar    *widget.ProgressBar
	prepErrorLabel     *widget.Label
	cleanupStatusLabel *widget.Label

	configCard          *widget.Card
	outputDirEntry      *widget.Entry
	browseButton        *widget.Button
	bootstrapAddrsEntry *widget.Entry
	serverP2PPortEntry  *widget.Entry // For server's P2P listen port
	generateButton      *widget.Button

	genCard        *widget.Card
	genStatusLabel *widget.Label
	genProgressBar *widget.ProgressBar

	resultsCard            *widget.Card
	resultsTitleLabel      *widget.Label
	resultsPeerIDText      *widget.Label
	peerIDInfoDisplay      *widget.Entry
	copyPeerIDButton       *widget.Button
	resultsOutputPathLabel *widget.Label
	readmeDisplay          *widget.Label
	openOutputButton       *widget.Button
	finishButton           *widget.Button

	// Data fields
	extractedGoExePath     string
	extractedTemplatesPath string // This will be the root of the temporary module
	cleanupGoSdkFunc       func() error
	cleanupTemplatesFunc   func() error
	selectedOutputDir      string
	generatedInfo          core.GeneratedInfo

	// Operation management
	operationCtx    context.Context
	operationCancel context.CancelFunc
	isShuttingDown  bool
	shutdownMutex   sync.Mutex
}

func main() {
	// Logging Setup
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("CRITICAL: Failed to get user config directory: %v", err)
	}
	logDir := filepath.Join(configDir, "GuiKeyStandaloneGo", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		log.Fatalf("CRITICAL: Failed to create log directory %s: %v", logDir, err)
	}
	logFilePath := filepath.Join(logDir, "guikeygen_ui_embed.log")
	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("CRITICAL: Failed to open log file %s: %v", logFilePath, err)
	}
	defer f.Close()
	log.SetOutput(f) // Direct log output to the file
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	log.Printf("[main] Application starting. UI Log: %s\n", logFilePath)

	// Fyne App Initialization
	fa := app.NewWithID("com.rizamohammadkhan.guikeygen.embed")
	fa.Settings().SetTheme(newAppTheme()) // Apply custom theme
	w := fa.NewWindow("GenMon - Activity Monitor Suite Generator")

	ui := &guiApp{
		fyneApp: fa,
		mainWin: w,
	}

	w.SetCloseIntercept(func() {
		log.Println("[main] Window close intercepted by user.")
		ui.triggerShutdown()
	})

	ui.setupUI()
	w.Resize(fyne.NewSize(800, 750))
	w.CenterOnScreen()

	// Start environment preparation in a goroutine
	go ui.prepareEnvironment()

	log.Print("[main] Showing window and entering Fyne main loop.")
	w.ShowAndRun() // This blocks until the app exits
	log.Print("[main] Application exited Fyne main loop.")
}

func (ui *guiApp) setupUI() {
	log.Print("[ui] Setting up UI elements")

	// --- Environment Preparation Section ---
	ui.prepStatusLabel = widget.NewLabel("Initializing: Preparing required resources...")
	ui.prepProgressBar = widget.NewProgressBar()
	ui.prepErrorLabel = widget.NewLabel("")
	ui.prepErrorLabel.Wrapping = fyne.TextWrapWord
	ui.prepErrorLabel.Hide()
	ui.cleanupStatusLabel = widget.NewLabel("")
	ui.cleanupStatusLabel.Alignment = fyne.TextAlignCenter
	ui.cleanupStatusLabel.Hide()
	prepContent := container.NewVBox(ui.prepStatusLabel, ui.prepProgressBar, ui.prepErrorLabel, ui.cleanupStatusLabel)
	ui.prepCard = widget.NewCard("1. Environment Setup", "Extracting necessary tools and templates...", prepContent)

	// --- Configuration Section ---
	ui.outputDirEntry = widget.NewEntry()
	ui.outputDirEntry.SetPlaceHolder("Select output directory for generated packages...")
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
			} // User cancelled
			path := uri.Path()
			ui.outputDirEntry.SetText(path)
			ui.selectedOutputDir = path
			log.Printf("[ui-cfg] Output directory selected: %s", path)
		}, ui.mainWin)
	})
	ui.browseButton.Disable()
	outputDirBox := container.NewBorder(nil, nil, nil, ui.browseButton, ui.outputDirEntry)

	defaultBootstrap := strings.Join(config.DefaultClientSettings().BootstrapAddresses, ",\n")
	ui.bootstrapAddrsEntry = widget.NewMultiLineEntry()
	ui.bootstrapAddrsEntry.SetText(defaultBootstrap)
	ui.bootstrapAddrsEntry.Wrapping = fyne.TextWrapWord
	ui.bootstrapAddrsEntry.SetMinRowsVisible(3)
	ui.bootstrapAddrsEntry.Disable()

	ui.serverP2PPortEntry = widget.NewEntry()
	ui.serverP2PPortEntry.SetPlaceHolder("Optional (e.g., 25500 for manual P2P port forwarding)")
	ui.serverP2PPortEntry.Validator = func(s string) error {
		if s == "" {
			return nil
		} // Empty is valid (dynamic port)
		val, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("must be a number")
		}
		if val <= 1024 || val > 65535 {
			return fmt.Errorf("port: 1025-65535")
		}
		return nil
	}
	ui.serverP2PPortEntry.Disable()

	ui.generateButton = widget.NewButtonWithIcon("Generate Packages", theme.ConfirmIcon(), ui.startGeneration)
	ui.generateButton.Importance = widget.HighImportance
	ui.generateButton.Disable()

	configForm := widget.NewForm(
		widget.NewFormItem("Output Directory:", outputDirBox),
		widget.NewFormItem("Bootstrap Addresses:", ui.bootstrapAddrsEntry),
		widget.NewFormItem("Server P2P TCP Port:", ui.serverP2PPortEntry),
	)
	configContent := container.NewVBox(configForm, layout.NewSpacer(), container.NewCenter(ui.generateButton))
	ui.configCard = widget.NewCard("2. Configuration", "Specify settings for the generated client and server.", configContent)

	// --- Generation Progress Section ---
	ui.genStatusLabel = widget.NewLabel("Generation will start after configuration.")
	ui.genProgressBar = widget.NewProgressBar()
	genContent := container.NewVBox(ui.genStatusLabel, ui.genProgressBar)
	ui.genCard = widget.NewCard("3. Generation Progress", "", genContent)
	ui.genCard.Hide()

	// --- Results Section ---
	ui.resultsTitleLabel = widget.NewLabelWithStyle("Generation Status", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	ui.resultsPeerIDText = widget.NewLabel("Generated Server PeerID:")
	ui.peerIDInfoDisplay = widget.NewEntry()
	ui.peerIDInfoDisplay.Disable()
	ui.copyPeerIDButton = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		if ui.peerIDInfoDisplay.Text != "" {
			ui.mainWin.Clipboard().SetContent(ui.peerIDInfoDisplay.Text)
			log.Printf("[ui-results] Copied PeerID: %s", ui.peerIDInfoDisplay.Text)
		}
	})
	peerIDLine := container.NewBorder(nil, nil, ui.resultsPeerIDText, ui.copyPeerIDButton, container.NewHScroll(ui.peerIDInfoDisplay))
	ui.resultsOutputPathLabel = widget.NewLabel("Output Path: Not yet generated")
	ui.resultsOutputPathLabel.Wrapping = fyne.TextWrapBreak
	ui.readmeDisplay = widget.NewLabel("README content will appear here.")
	ui.readmeDisplay.Wrapping = fyne.TextWrapWord
	readmeScroll := container.NewScroll(ui.readmeDisplay)
	readmeScroll.SetMinSize(fyne.NewSize(0, 250)) // Ensure it's reasonably sized

	ui.openOutputButton = widget.NewButtonWithIcon("Open Output Folder", theme.FolderOpenIcon(), func() {
		if ui.generatedInfo.FullOutputDir != "" {
			dialog.ShowInformation("Output Folder Location",
				"Packages generated in:\n"+ui.generatedInfo.FullOutputDir+"\n\nPlease navigate there using your file explorer.", ui.mainWin)
		} else {
			dialog.ShowInformation("Output Folder", "Output folder not available yet or generation failed.", ui.mainWin)
		}
	})
	ui.finishButton = widget.NewButtonWithIcon("Finish & Exit", theme.LogoutIcon(), ui.triggerShutdown)
	actionButtons := container.NewHBox(layout.NewSpacer(), ui.openOutputButton, ui.finishButton, layout.NewSpacer())

	resultsContent := container.NewVBox(
		ui.resultsTitleLabel, widget.NewSeparator(),
		peerIDLine, ui.resultsOutputPathLabel,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Instructions & Details (from generated README):", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		readmeScroll, layout.NewSpacer(), actionButtons,
	)
	ui.resultsCard = widget.NewCard("4. Results", "Review generated package details.", resultsContent)
	ui.resultsCard.Hide() // Initially hide results

	// Main layout
	mainLayout := container.NewVBox(ui.prepCard, ui.configCard, ui.genCard, ui.resultsCard)
	scrollableMain := container.NewScroll(mainLayout)
	ui.mainWin.SetContent(container.NewPadded(scrollableMain))
}

func (ui *guiApp) prepareEnvironment() {
	log.Println("[prep] Starting environment preparation goroutine")
	ui.operationCtx, ui.operationCancel = context.WithCancel(context.Background())

	// Use fyne.Do for all UI updates from this goroutine
	fyne.Do(func() {
		ui.updatePrepStatus("Preparing Go compiler...", 0)
	})

	// Call functions from package main (defined in resources.go)
	goExe, cleanupGo, errGo := GetGoExecutablePath(ui.operationCtx, func(msg string, pct int) {
		displayPct := int(float64(pct) * 0.5)
		fyne.Do(func() { ui.updatePrepStatus(msg, displayPct) })
	})

	if errGo != nil {
		fyne.Do(func() {
			if errGo == context.Canceled {
				ui.handlePrepError("Environment preparation canceled by user.")
			} else {
				ui.handlePrepError(fmt.Sprintf("Failed to prepare Go compiler: %v", errGo))
			}
		})
		return
	}
	ui.extractedGoExePath = goExe
	ui.cleanupGoSdkFunc = cleanupGo

	fyne.Do(func() {
		ui.updatePrepStatus("Preparing source templates and module structure...", 50)
	})

	// This returns the root of the temporary module structure
	tempModuleRoot, cleanupTpl, errTpl := GetTemporaryModulePath(ui.operationCtx, func(msg string, pct int) {
		displayPct := 50 + int(float64(pct)*0.5)
		fyne.Do(func() { ui.updatePrepStatus(msg, displayPct) })
	})

	if errTpl != nil {
		fyne.Do(func() {
			if errTpl == context.Canceled {
				ui.handlePrepError("Environment preparation canceled by user.")
			} else {
				ui.handlePrepError(fmt.Sprintf("Failed to prepare source templates: %v", errTpl))
			}
		})
		return
	}
	ui.extractedTemplatesPath = tempModuleRoot // Store the root path of the temp module
	ui.cleanupTemplatesFunc = cleanupTpl

	fyne.Do(func() {
		ui.updatePrepStatus("Environment ready! You can now configure generation settings.", 100)
		ui.prepCard.SetSubTitle("Preparation complete.") // Update card subtitle
		ui.enableConfigurationSection()
	})
	log.Println("[prep] Environment preparation completed successfully")
}

func (ui *guiApp) updatePrepStatus(message string, percentage int) {
	if ui.prepStatusLabel != nil {
		ui.prepStatusLabel.SetText(message)
	}
	if ui.prepProgressBar != nil && percentage >= 0 && percentage <= 100 {
		ui.prepProgressBar.SetValue(float64(percentage) / 100.0)
	}
}

func (ui *guiApp) handlePrepError(errMsg string) {
	log.Printf("[prep] Error: %s", errMsg)
	if ui.prepStatusLabel != nil {
		ui.prepStatusLabel.SetText("Environment setup FAILED!")
	}
	if ui.prepProgressBar != nil {
		ui.prepProgressBar.Hide()
	}
	if ui.prepErrorLabel != nil {
		ui.prepErrorLabel.SetText("Error: " + errMsg + "\nPlease check logs and restart the application.")
		ui.prepErrorLabel.Show()
	}
	// Optionally disable further actions if prep fails critically
	if ui.generateButton != nil {
		ui.generateButton.Disable()
	}
}

func (ui *guiApp) enableConfigurationSection() {
	log.Print("[ui] Enabling configuration section")
	if ui.outputDirEntry != nil {
		ui.outputDirEntry.Enable()
	}
	if ui.browseButton != nil {
		ui.browseButton.Enable()
	}
	if ui.bootstrapAddrsEntry != nil {
		ui.bootstrapAddrsEntry.Enable()
	}
	if ui.serverP2PPortEntry != nil {
		ui.serverP2PPortEntry.Enable()
	}
	if ui.generateButton != nil {
		ui.generateButton.Enable()
	}
	if ui.configCard != nil {
		ui.configCard.SetSubTitle("Ready to configure your packages.")
		ui.configCard.Refresh()
	}
}

func (ui *guiApp) startGeneration() {
	log.Print("[gen] 'Generate Packages' button clicked")

	if ui.selectedOutputDir == "" {
		dialog.ShowInformation("Input Required", "Please select an output directory.", ui.mainWin)
		return
	}

	serverP2PPortStr := ui.serverP2PPortEntry.Text
	if err := ui.serverP2PPortEntry.Validate(); err != nil {
		dialog.ShowError(fmt.Errorf("invalid Server P2P Port: %w. Please enter a valid port number (e.g., 25500) or leave blank for dynamic port selection by the server.", err), ui.mainWin)
		return
	}

	// Check if output directory exists, offer to create
	if _, err := os.Stat(ui.selectedOutputDir); os.IsNotExist(err) {
		dialog.ShowConfirm("Create Directory?",
			fmt.Sprintf("The output directory '%s' does not exist.\nWould you like to create it?", ui.selectedOutputDir),
			func(create bool) {
				if !create {
					return
				}
				if errMkdir := os.MkdirAll(ui.selectedOutputDir, 0755); errMkdir != nil {
					dialog.ShowError(fmt.Errorf("failed to create directory '%s': %w", ui.selectedOutputDir, errMkdir), ui.mainWin)
					return
				}
				ui.performGeneration(serverP2PPortStr)
			}, ui.mainWin)
	} else {
		ui.performGeneration(serverP2PPortStr)
	}
}

func (ui *guiApp) performGeneration(serverP2PPort string) {
	log.Print("[gen] Starting generation process in goroutine")

	fyne.Do(func() {
		ui.configCard.SetSubTitle("Generation in progress...")
		ui.configCard.Refresh()
		ui.genCard.Show()
		ui.generateButton.Disable()
		ui.browseButton.Disable()
		ui.bootstrapAddrsEntry.Disable()
		ui.outputDirEntry.Disable()
		ui.serverP2PPortEntry.Disable()
		ui.resultsCard.Hide() // Hide previous results if any
		ui.updateGenStatus("Preparing for generation...", 0)
	})

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
		// The TemplatesBasePath should be the 'templates' subdirectory within the temporary module root.
		// e.g., <ui.extractedTemplatesPath>/generator/templates/
		templatesActualBasePath := filepath.Join(ui.extractedTemplatesPath, "generator", "templates")
		log.Printf("[gen-goroutine] Using TempModuleRootPath: %s", ui.extractedTemplatesPath)
		log.Printf("[gen-goroutine] Using TemplatesBasePath for core: %s", templatesActualBasePath)

		settings := core.GeneratorSettings{
			OutputDirPath:         ui.selectedOutputDir,
			BootstrapAddresses:    bootstrapFinal,
			ServerExplicitP2PPort: serverP2PPort,
			GoExecutablePath:      ui.extractedGoExePath,
			TempModuleRootPath:    ui.extractedTemplatesPath, // This is the root for `go mod tidy`
			TemplatesBasePath:     templatesActualBasePath,   // This is where client_template & server_template subdirs live
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
				// Re-enable config UI on error
				ui.enableConfigurationSection() // Make sure this correctly re-enables
				ui.configCard.SetSubTitle("Ready to configure your packages.")
				ui.configCard.Refresh()
				if ui.genCard != nil {
					ui.genCard.Hide()
				}
				return
			}
			ui.generatedInfo = generatedInfo
			ui.showResults()
			ui.configCard.SetSubTitle("Generation complete. See results below.")
			ui.configCard.Refresh()
		})
	}()
}

func (ui *guiApp) updateGenStatus(message string, percentage int) {
	if ui.genStatusLabel != nil {
		ui.genStatusLabel.SetText(message)
	}
	if ui.genProgressBar != nil && percentage >= 0 && percentage <= 100 {
		ui.genProgressBar.SetValue(float64(percentage) / 100.0)
	}
}

func (ui *guiApp) handleGenError(errMsg string) {
	log.Printf("[gen] Error: %s", errMsg)
	if ui.genStatusLabel != nil {
		ui.genStatusLabel.SetText("Generation FAILED!")
	}
	if ui.genProgressBar != nil {
		// Optionally set progress to 0 or keep last value, or make it indeterminate if Fyne supports
	}
	dialog.ShowError(fmt.Errorf(errMsg), ui.mainWin)
	// UI re-enabling is now handled in performGeneration's error path.
}

func (ui *guiApp) showResults() {
	log.Print("[ui] Displaying generation results")
	fyne.Do(func() {
		if ui.genStatusLabel != nil {
			ui.genStatusLabel.SetText("Generation completed successfully!")
		}
		if ui.genProgressBar != nil {
			ui.genProgressBar.SetValue(1.0)
		}

		if ui.resultsTitleLabel != nil {
			ui.resultsTitleLabel.SetText("Generation Completed Successfully!")
		}

		if ui.generatedInfo.ServerPeerID != "" {
			if ui.peerIDInfoDisplay != nil {
				ui.peerIDInfoDisplay.SetText(ui.generatedInfo.ServerPeerID)
				ui.peerIDInfoDisplay.Show()
			}
			if ui.copyPeerIDButton != nil {
				ui.copyPeerIDButton.Show()
			}
		} else {
			if ui.peerIDInfoDisplay != nil {
				ui.peerIDInfoDisplay.SetText("Not available")
				ui.peerIDInfoDisplay.Show()
			}
			if ui.copyPeerIDButton != nil {
				ui.copyPeerIDButton.Hide()
			}
		}
		if ui.peerIDInfoDisplay != nil {
			ui.peerIDInfoDisplay.Refresh()
		}
		if ui.copyPeerIDButton != nil {
			ui.copyPeerIDButton.Refresh()
		}

		if ui.generatedInfo.FullOutputDir != "" {
			if ui.resultsOutputPathLabel != nil {
				ui.resultsOutputPathLabel.SetText(fmt.Sprintf("Output Path: %s", ui.generatedInfo.FullOutputDir))
			}
		} else {
			if ui.resultsOutputPathLabel != nil {
				ui.resultsOutputPathLabel.SetText("Output Path: Not available")
			}
		}
		if ui.resultsOutputPathLabel != nil {
			ui.resultsOutputPathLabel.Refresh()
		}

		if ui.generatedInfo.ReadmeContent == "" {
			if ui.readmeDisplay != nil {
				ui.readmeDisplay.SetText("Error: README content not available. Check logs.")
			}
		} else {
			if ui.readmeDisplay != nil {
				ui.readmeDisplay.SetText(ui.generatedInfo.ReadmeContent)
			}
		}
		if ui.readmeDisplay != nil {
			ui.readmeDisplay.Refresh()
		}

		if ui.resultsCard != nil {
			ui.resultsCard.Show()
			ui.resultsCard.Refresh()
		}

		// Scroll to bottom to show results card fully
		time.AfterFunc(200*time.Millisecond, func() { // Slight delay to ensure layout is complete
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
	})
}

func (ui *guiApp) cleanupResources() {
	log.Println("[cleanup] Starting resource cleanup process.")
	if ui.operationCancel != nil {
		ui.operationCancel() // Signal context cancellation to ongoing prep operations
	}

	log.Println("[cleanup] Cleaning up extracted Go SDK...")
	if ui.cleanupGoSdkFunc != nil {
		if err := ui.cleanupGoSdkFunc(); err != nil {
			log.Printf("[cleanup] Error cleaning up Go SDK: %v", err)
		} else {
			log.Println("[cleanup] Go SDK cleanup successful.")
		}
	} else {
		log.Println("[cleanup] No Go SDK cleanup function registered.")
	}

	log.Println("[cleanup] Cleaning up extracted templates and temporary module...")
	if ui.cleanupTemplatesFunc != nil {
		if err := ui.cleanupTemplatesFunc(); err != nil {
			log.Printf("[cleanup] Error cleaning up templates/module: %v", err)
		} else {
			log.Println("[cleanup] Templates/module cleanup successful.")
		}
	} else {
		log.Println("[cleanup] No templates/module cleanup function registered.")
	}
	log.Println("[cleanup] Resource cleanup attempt finished.")
}

func (ui *guiApp) triggerShutdown() {
	ui.shutdownMutex.Lock()
	if ui.isShuttingDown {
		ui.shutdownMutex.Unlock()
		log.Println("[shutdown] Shutdown sequence already in progress.")
		return
	}
	ui.isShuttingDown = true
	ui.shutdownMutex.Unlock()

	log.Println("[shutdown] Triggering application shutdown sequence.")

	fyne.Do(func() {
		// Disable all interactive elements immediately
		if ui.finishButton != nil {
			ui.finishButton.Disable()
		}
		if ui.openOutputButton != nil {
			ui.openOutputButton.Disable()
		}
		if ui.generateButton != nil {
			ui.generateButton.Disable()
		}
		if ui.browseButton != nil {
			ui.browseButton.Disable()
		}
		if ui.outputDirEntry != nil {
			ui.outputDirEntry.Disable()
		}
		if ui.bootstrapAddrsEntry != nil {
			ui.bootstrapAddrsEntry.Disable()
		}
		if ui.serverP2PPortEntry != nil {
			ui.serverP2PPortEntry.Disable()
		}

		if ui.cleanupStatusLabel != nil {
			ui.cleanupStatusLabel.SetText("Cleaning up resources, please wait...\nThe application will close automatically.")
			ui.cleanupStatusLabel.Show()
		}
		// Ensure the prepCard (where cleanupStatusLabel is) is visible or refresh it if it's part of layout
		if ui.prepCard != nil {
			ui.prepCard.Refresh()
		}

		// Hide other cards to simplify UI during shutdown
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

	// Perform cleanup in a background goroutine so UI doesn't freeze
	go func() {
		ui.cleanupResources()
		// After cleanup, quit the application on the Fyne/main goroutine
		fyne.Do(func() {
			log.Println("[shutdown] Cleanup finished. Quitting Fyne application.")
			ui.fyneApp.Quit()
		})
	}()
}
