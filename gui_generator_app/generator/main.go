// GuiKeyStandaloneGo/generator/main.go
package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/generator/core"
	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/config"
)

func main() {
	outputDir := flag.String("output", "./_generated_packages", "Directory to save generated packages")
	bootstrapAddrs := flag.String("bootstrap", strings.Join(config.DefaultClientSettings().BootstrapAddresses, ","), "Comma-separated libp2p bootstrap multiaddresses")
	goExePath := flag.String("goexec", "", "Path to Go executable (optional, defaults to 'go' in PATH)")

	flag.Parse()

	absOutputDir, err := filepath.Abs(*outputDir)
	if err != nil {
		log.Fatalf("Failed to get absolute path for output directory: %v", err)
	}

	cliProgressCallback := func(message string, percentage int) {
		log.Printf("[PROGRESS %d%%] %s", percentage, message)
	}

	settings := core.GeneratorSettings{
		OutputDirPath:      absOutputDir,
		BootstrapAddresses: *bootstrapAddrs,
		GoExecutablePath:   *goExePath, // Pass from flag
		ProgressCallback:   cliProgressCallback,
		// ClientConfigOverrides and ServerConfigOverrides can be added here if CLI needs them
	}

	generatedInfo, err := core.PerformGeneration(&settings)
	if err != nil {
		log.Fatalf("Generation failed: %v", err)
	}

	fmt.Printf("Generation successful. Packages created in: %s\n", generatedInfo.FullOutputDir)
	fmt.Printf("Server Peer ID: %s\n", generatedInfo.ServerPeerID)
	fmt.Println("\n--- README ---")
	fmt.Println(generatedInfo.ReadmeContent)
}
