// File: GuiKeyStandaloneGo/gui_generator_app/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/generator/core"
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/config"
)

type guiApp struct {
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
	resultsCard      *widget.Card
	readmeDisplay    *widget.Entry
	openOutputButton *widget.Button
	finishButton     *widget.Button

	// Shared data
	extractedGoExePath     string
	extractedTemplatesPath string // This will hold the temporary module root path
	cleanupGoSdkFunc       func() error
	cleanupTemplatesFunc   func() error
	selectedOutputDir      string
	generatedInfo          core.GeneratedInfo

	operationCtx    context.Context
	operationCancel context.CancelFunc
}

func main() {
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
	w := fa.NewWindow("GuiKey Standalone Package Generator")

	ui := &guiApp{
		fyneApp: fa,
		mainWin: w,
	}

	w.SetCloseIntercept(func() {
		log.Println("[main] Window close intercepted. Performing resource cleanup...")
		ui.cleanupResources()
		w.Close()
	})

	ui.setupUI()
	w.Resize(fyne.NewSize(800, 700))
	w.CenterOnScreen()

	// Start environment preparation immediately
	go ui.prepareEnvironment()

	log.Print("[main] Entering Fyne main loop")
	w.ShowAndRun()
	log.Print("[main] Application exited.")
}

func (ui *guiApp) setupUI() {
	log.Print("[ui] Setting up single page UI")

	// 1. Environment Preparation Section
	ui.prepStatusLabel = widget.NewLabel("Initializing: Preparing required resources...")
	ui.prepProgressBar = widget.NewProgressBar()
	ui.prepProgressBar.SetValue(0)
	ui.prepErrorLabel = widget.NewLabel("")
	ui.prepErrorLabel.Hide()

	prepContent := container.NewVBox(
		ui.prepStatusLabel,
		ui.prepProgressBar,
		ui.prepErrorLabel,
	)
	ui.prepCard = widget.NewCard("Environment Setup", "", prepContent)

	// 2. Configuration Section (Initially disabled)
	ui.outputDirEntry = widget.NewEntry()
	ui.outputDirEntry.SetPlaceHolder("Output directory will be selected here...")
	ui.outputDirEntry.Disable()

	ui.browseButton = widget.NewButton("Browse...", func() {
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

	ui.generateButton = widget.NewButton("Generate Packages", func() {
		ui.startGeneration()
	})
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

	// 3. Generation Progress Section (Initially hidden)
	ui.genStatusLabel = widget.NewLabel("Generation will start here...")
	ui.genProgressBar = widget.NewProgressBar()
	ui.genProgressBar.SetValue(0)

	genContent := container.NewVBox(
		ui.genStatusLabel,
		ui.genProgressBar,
	)
	ui.genCard = widget.NewCard("Generation Progress", "", genContent)
	ui.genCard.Hide()

	// 4. Results Section (Initially hidden)
	ui.readmeDisplay = widget.NewMultiLineEntry()
	ui.readmeDisplay.Wrapping = fyne.TextWrapWord
	ui.readmeDisplay.Disable()

	ui.openOutputButton = widget.NewButton("Open Output Folder", func() {
		if ui.generatedInfo.FullOutputDir != "" {
			dialog.ShowInformation("Open Folder", "Please navigate to:\n"+ui.generatedInfo.FullOutputDir, ui.mainWin)
		}
	})

	ui.finishButton = widget.NewButton("Finish", func() {
		log.Print("[ui] Finish button clicked, cleaning up and exiting")
		ui.cleanupResources()
		ui.fyneApp.Quit()
	})

	readmeScroll := container.NewScroll(ui.readmeDisplay)
	readmeScroll.SetMinSize(fyne.NewSize(0, 200))

	buttonBox := container.NewHBox(
		layout.NewSpacer(),
		ui.openOutputButton,
		ui.finishButton,
		layout.NewSpacer(),
	)

	resultsContent := container.NewVBox(
		widget.NewLabel("Generation completed successfully! Instructions:"),
		readmeScroll,
		buttonBox,
	)
	ui.resultsCard = widget.NewCard("Results", "", resultsContent)
	ui.resultsCard.Hide()

	// Main layout - all sections in one scrollable container
	mainContent := container.NewVBox(
		ui.prepCard,
		ui.configCard,
		ui.genCard,
		ui.resultsCard,
	)

	scrollContainer := container.NewScroll(mainContent)
	ui.mainWin.SetContent(container.NewPadded(scrollContainer))
}

func (ui *guiApp) prepareEnvironment() {
	log.Println("[prep] Starting environment preparation")
	ui.operationCtx, ui.operationCancel = context.WithCancel(context.Background())

	fyne.Do(func() {
		ui.updatePrepStatus("Preparing Go compiler...", 0)
	})

	// Step 1: Prepare Go SDK
	goExe, cleanupGo, errGo := GetGoExecutablePath(ui.operationCtx, func(msg string, pct int) {
		displayPct := int(float64(pct) * 0.5) // Go SDK prep is 0-50%
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

	// Step 2: Prepare Templates using GetTemporaryModulePath
	// templatesPath is the root of the temporary module
	templatesPath, cleanupTpl, errTpl := GetTemporaryModulePath(ui.operationCtx, func(msg string, pct int) {
		displayPct := 50 + int(float64(pct)*0.5) // Scale to 50-100%
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
	ui.extractedTemplatesPath = templatesPath // This IS the temporary module root path
	ui.cleanupTemplatesFunc = cleanupTpl

	// Environment ready - enable configuration section
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

	// Update card title to show it's ready
	ui.configCard.SetTitle("Configuration (Ready)")
}

func (ui *guiApp) startGeneration() {
	log.Print("[gen] Starting package generation")

	if ui.selectedOutputDir == "" {
		dialog.ShowInformation("Input Required", "Please select an output directory.", ui.mainWin)
		return
	}

	// Create directory if it doesn't exist
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

	// Show generation section and disable configuration
	ui.genCard.Show()
	ui.generateButton.Disable()
	ui.browseButton.Disable()
	ui.bootstrapAddrsEntry.Disable()

	// Process bootstrap addresses
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
		// ui.extractedTemplatesPath IS the temporary module root.
		// TemplatesBasePath for core.PerformGeneration should be <temp_module_root>/generator/templates
		templatesBasePath := filepath.Join(ui.extractedTemplatesPath, "generator", "templates")

		settings := core.GeneratorSettings{
			OutputDirPath:      ui.selectedOutputDir,
			BootstrapAddresses: bootstrapFinal,
			GoExecutablePath:   ui.extractedGoExePath,
			TempModuleRootPath: ui.extractedTemplatesPath, // Pass the actual temporary module root
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

	// Re-enable configuration for retry
	ui.generateButton.Enable()
	ui.browseButton.Enable()
	ui.bootstrapAddrsEntry.Enable()
}

func (ui *guiApp) showResults() {
	log.Print("[ui] Showing generation results")

	ui.genStatusLabel.SetText("Generation completed successfully!")
	ui.genProgressBar.SetValue(1.0)

	// Set up results display
	if ui.generatedInfo.ReadmeContent == "" {
		ui.readmeDisplay.SetText("Error: README content not available. Check logs.")
	} else {
		ui.readmeDisplay.SetText(ui.generatedInfo.ReadmeContent)
	}

	// Update results card title with info
	resultsTitle := "Results - Generation Complete!"
	if ui.generatedInfo.FullOutputDir != "" {
		resultsTitle += fmt.Sprintf("\nOutput: %s", filepath.Base(ui.generatedInfo.FullOutputDir))
	}
	if ui.generatedInfo.ServerPeerID != "" {
		resultsTitle += fmt.Sprintf("\nServer PeerID: %s", ui.generatedInfo.ServerPeerID)
	}
	ui.resultsCard.SetTitle(resultsTitle)

	ui.resultsCard.Show()

	// Scroll to results
	time.AfterFunc(100*time.Millisecond, func() {
		// This ensures the UI updates before scrolling
		fyne.Do(func() {
			if scroll, ok := ui.mainWin.Content().(*container.Scroll); ok {
				scroll.ScrollToBottom()
			}
		})
	})
}

func (ui *guiApp) cleanupResources() {
	log.Println("[cleanup] Cleaning up resources")
	if ui.operationCancel != nil {
		ui.operationCancel()
	}
	if ui.cleanupGoSdkFunc != nil {
		if err := ui.cleanupGoSdkFunc(); err != nil {
			log.Printf("[cleanup] Error cleaning up Go SDK: %v", err)
		}
	}
	if ui.cleanupTemplatesFunc != nil {
		if err := ui.cleanupTemplatesFunc(); err != nil {
			log.Printf("[cleanup] Error cleaning up templates: %v", err)
		}
	}
}
