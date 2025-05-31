// GuiKeyStandaloneGo/generator/main.go
package main

import (
	"flag"
	"log"
	"path/filepath"
	"strings" // Import strings

	"guikeystandalonego/generator/core" // Corrected import
	"guikeystandalonego/pkg/config"     // Corrected import
)

func main() {
	outputDir := flag.String("output", "./_generated_packages", "Directory to save generated packages") // Changed default to avoid conflict with previous example
	bootstrapAddrs := flag.String("bootstrap", strings.Join(config.DefaultClientSettings().BootstrapAddresses, ","), "Comma-separated libp2p bootstrap multiaddresses")
	// Add more flags for client/server config overrides later

	flag.Parse()

	// It's good practice to use absolute paths for clarity
	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		log.Fatalf("Failed to get absolute path for output directory: %v", err)
	}

	// Create a default GeneratorSettings instance.
	// In a real GUI/more complex CLI, you'd populate these from user input.
	settings := core.GeneratorSettings{
		OutputDirPath:      absOutputDir,
		BootstrapAddresses: *bootstrapAddrs,
		// ClientConfigOverrides and ServerConfigOverrides would be populated here
		// if you had flags for individual settings.
		// For now, they'll use defaults or what's hardcoded in PerformGeneration.
	}

	if _, err := core.PerformGeneration(&settings); err != nil {
		log.Fatalf("Generation failed: %v", err)
	}
}
