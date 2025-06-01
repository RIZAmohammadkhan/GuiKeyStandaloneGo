// File: GuiKeyStandaloneGo/gui_generator_app/resources.go
package main // Important: This is part of package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	_ "embed" // Enable go:embed.
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// --- Go SDK Embedding ---

//go:embed all:embedded_go_sdk
var embeddedGoSDKFS embed.FS

// NOTE: Ensure these constants match EXACTLY what's in your embedded_go_sdk directory
const embeddedGoSDKZipName = "go1.24.3.windows-amd64.zip" // Example: UPDATE THIS
const embeddedGoSDKZipPath = "embedded_go_sdk/" + embeddedGoSDKZipName
const embeddedGoSDKSHA256 = "be9787cb08998b1860fe3513e48a5fe5b96302d358a321b58e651184fa9638b3" // Example: UPDATE THIS

// --- Source Code Embedding ---

//go:embed all:generator/templates/client_template
var embeddedClientTemplateFS embed.FS

const clientTemplateEmbedRelPath = "generator/templates/client_template"

//go:embed all:generator/templates/server_template
var embeddedServerTemplateFS embed.FS

const serverTemplateEmbedRelPath = "generator/templates/server_template"

//go:embed all:pkg/config
var embeddedPkgConfigFS embed.FS

const pkgConfigEmbedRelPath = "pkg/config"

//go:embed all:pkg/crypto
var embeddedPkgCryptoFS embed.FS

const pkgCryptoEmbedRelPath = "pkg/crypto"

//go:embed all:pkg/p2p
var embeddedPkgP2PFS embed.FS

const pkgP2PEmbedRelPath = "pkg/p2p"

//go:embed all:pkg/types
var embeddedPkgTypesFS embed.FS

const pkgTypesEmbedRelPath = "pkg/types"

// Module path for the generator app itself, used in generated go.mod files for 'replace' directives.
// This should match the module path defined in GuiKeyStandaloneGo/gui_generator_app/go.mod
const mainAppModulePath = "github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app"

var (
	extractedGoPath        string
	extractedGoCleanupFunc func() error
	extractedGoPathOnce    sync.Once
	extractedGoPathErr     error

	temporaryModuleRootPath        string
	temporaryModuleRootCleanupFunc func() error
	temporaryModuleRootPathOnce    sync.Once
	temporaryModuleRootPathErr     error
)

type ProgressFunc func(message string, percentage int)

const progressMessageMaxDisplayLength = 55

func truncateString(s string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}
	if len(s) <= maxLength {
		return s
	}
	if maxLength < 3 {
		return s[:maxLength]
	}
	return s[:maxLength-3] + "..."
}

func formatProgressMessage(format string, dynamicArg string, maxTotalLen int) string {
	staticLen := 0
	parts := strings.SplitN(format, "%s", 2)
	if len(parts) < 2 {
		log.Printf("[WARN] formatProgressMessage: format string lacks '%%s': '%s'", format)
		return truncateString(format, maxTotalLen)
	}
	staticLen += len(parts[0]) + len(parts[1])
	availableForDynamic := maxTotalLen - staticLen
	truncatedDynamicArg := truncateString(dynamicArg, availableForDynamic)
	finalMessage := fmt.Sprintf(format, truncatedDynamicArg)
	if len(finalMessage) > maxTotalLen {
		return truncateString(finalMessage, maxTotalLen)
	}
	return finalMessage
}

// GetGoExecutablePath extracts the embedded Go SDK if not already done,
// and returns the path to the go.exe and a cleanup function.
// It is thread-safe due to sync.Once.
func GetGoExecutablePath(ctx context.Context, progress ProgressFunc) (exePath string, cleanupFunc func() error, err error) {
	extractedGoPathOnce.Do(func() {
		log.Println("[res-go] First call to GetGoExecutablePath, proceeding with extraction.")
		if progress == nil {
			progress = func(m string, p int) { log.Printf("[res-go-progress] %d%%: %s", p, m) }
		}

		progress("Reading embedded Go SDK...", 1)
		zipBytes, readErr := embeddedGoSDKFS.ReadFile(embeddedGoSDKZipPath)
		if readErr != nil {
			extractedGoPathErr = fmt.Errorf("reading embedded Go SDK '%s': %w", embeddedGoSDKZipPath, readErr)
			progress("Error reading Go SDK.", 100)
			return
		}

		progress("Verifying Go SDK checksum...", 5)
		hasher := sha256.New()
		hasher.Write(zipBytes) // Best to write in chunks if SDK is huge, but for typical sizes this is fine.
		actualSHA := hex.EncodeToString(hasher.Sum(nil))
		if actualSHA != embeddedGoSDKSHA256 {
			extractedGoPathErr = fmt.Errorf("Go SDK checksum MISMATCH! Expected: %s, Got: %s for %s", embeddedGoSDKSHA256, actualSHA, embeddedGoSDKZipName)
			progress("Go SDK checksum mismatch!", 100)
			return
		}
		progress("Go SDK checksum verified.", 10)

		tempDir, mkErr := os.MkdirTemp("", "guikey-go-sdk-")
		if mkErr != nil {
			extractedGoPathErr = fmt.Errorf("creating temp dir for Go SDK: %w", mkErr)
			progress("Error creating temp directory for Go SDK.", 100)
			return
		}
		extractedGoCleanupFunc = func() error {
			log.Printf("[res-go-cleanup] Removing Go SDK temp directory: %s", tempDir)
			return os.RemoveAll(tempDir)
		}
		log.Printf("[res-go] Go SDK temp directory: %s", tempDir)

		progress("Extracting Go SDK...", 15)
		zipReader, zipErr := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
		if zipErr != nil {
			extractedGoPathErr = fmt.Errorf("creating zip reader for Go SDK: %w", zipErr)
			progress("Error reading Go SDK zip.", 100)
			return
		}

		totalFiles := uint64(len(zipReader.File))
		if totalFiles == 0 {
			extractedGoPathErr = fmt.Errorf("Go SDK zip file '%s' is empty or invalid", embeddedGoSDKZipName)
			progress("Go SDK zip empty.", 100)
			return
		}

		var filesExtracted uint64
		for _, f := range zipReader.File {
			select {
			case <-ctx.Done():
				log.Println("[res-go] Go SDK extraction cancelled by user via context.")
				extractedGoPathErr = ctx.Err()
				progress("Go SDK extraction cancelled.", 100)
				return
			default:
			}

			fpath := filepath.Join(tempDir, f.Name)
			if !strings.HasPrefix(fpath, filepath.Clean(tempDir)+string(os.PathSeparator)) {
				extractedGoPathErr = fmt.Errorf("illegal path in Go SDK zip: %s", fpath)
				progress("Invalid path in Go SDK zip.", 100)
				return
			}

			if f.FileInfo().IsDir() {
				if err := os.MkdirAll(fpath, os.ModePerm); err != nil {
					extractedGoPathErr = fmt.Errorf("creating directory structure %s: %w", fpath, err)
					progressMsg := formatProgressMessage("Error creating dir %s", filepath.Base(f.Name), progressMessageMaxDisplayLength)
					progress(progressMsg, 100)
					return
				}
				continue
			}

			if errMkdir := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); errMkdir != nil {
				extractedGoPathErr = fmt.Errorf("creating dir for %s: %w", fpath, errMkdir)
				progressMsg := formatProgressMessage("Error creating directory for %s", filepath.Base(f.Name), progressMessageMaxDisplayLength)
				progress(progressMsg, 100)
				return
			}

			outFile, errOpen := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if errOpen != nil {
				extractedGoPathErr = fmt.Errorf("opening %s for writing: %w", fpath, errOpen)
				progressMsg := formatProgressMessage("Error opening file %s for writing", filepath.Base(f.Name), progressMessageMaxDisplayLength)
				progress(progressMsg, 100)
				return
			}

			rc, errOpenInZip := f.Open()
			if errOpenInZip != nil {
				outFile.Close()
				extractedGoPathErr = fmt.Errorf("opening in zip %s: %w", f.Name, errOpenInZip)
				progressMsg := formatProgressMessage("Error opening file %s in zip", f.Name, progressMessageMaxDisplayLength)
				progress(progressMsg, 100)
				return
			}

			_, errCopy := io.Copy(outFile, rc)
			// Close both files regardless of copy error, but after checking the error.
			closeErrOut := outFile.Close()
			closeErrIn := rc.Close()

			if errCopy != nil {
				extractedGoPathErr = fmt.Errorf("copying %s: %w", f.Name, errCopy)
				progressMsg := formatProgressMessage("Error copying file %s", f.Name, progressMessageMaxDisplayLength)
				progress(progressMsg, 100)
				return
			}
			if closeErrOut != nil {
				log.Printf("[res-go] Warning: error closing output file %s: %v", fpath, closeErrOut)
			}
			if closeErrIn != nil {
				log.Printf("[res-go] Warning: error closing input stream for %s from zip: %v", f.Name, closeErrIn)
			}

			filesExtracted++
			currentProgress := 15 + int(float64(filesExtracted)*80.0/float64(totalFiles)) // 15% to 95% for extraction
			progressMsg := formatProgressMessage("Extracting Go SDK: %s", filepath.Base(f.Name), progressMessageMaxDisplayLength)
			progress(progressMsg, currentProgress)
		}

		exePathTemp := filepath.Join(tempDir, "go", "bin", "go.exe")
		if _, errStat := os.Stat(exePathTemp); os.IsNotExist(errStat) {
			extractedGoPathErr = fmt.Errorf("go.exe not found at expected path %s after extraction. Check embedded zip structure and `embeddedGoSDKZipName`", exePathTemp)
			progress("go.exe not found post-extraction.", 100)
			return
		}
		extractedGoPath = exePathTemp
		progress("Go environment ready.", 100)
		log.Printf("[res-go] Extracted Go executable: %s", extractedGoPath)
	})
	return extractedGoPath, extractedGoCleanupFunc, extractedGoPathErr
}

// GetTemporaryModulePath extracts embedded source templates and shared packages
// into a temporary directory structure suitable for `go build`.
// It is thread-safe due to sync.Once.
func GetTemporaryModulePath(ctx context.Context, progress ProgressFunc) (moduleRootPath string, cleanupFunc func() error, err error) {
	temporaryModuleRootPathOnce.Do(func() {
		log.Println("[res-mod] First call to GetTemporaryModulePath, setting up temp module.")
		if progress == nil {
			progress = func(m string, p int) { log.Printf("[res-mod-progress] %d%%: %s", p, m) }
		}
		progress("Preparing temporary Go module structure...", 0)

		tempModuleRoot, mkErr := os.MkdirTemp("", "guikey-temp-module-")
		if mkErr != nil {
			temporaryModuleRootPathErr = fmt.Errorf("creating temp module root: %w", mkErr)
			progress("Error creating temp module directory.", 100)
			return
		}
		log.Printf("[res-mod] Temporary module root created at: %s", tempModuleRoot)
		temporaryModuleRootCleanupFunc = func() error {
			log.Printf("[res-mod-cleanup] Removing temporary module root: %s", tempModuleRoot)
			return os.RemoveAll(tempModuleRoot)
		}

		// Determine Go version for go.mod from the SDK zip filename
		goVersionForMod := "1.22" // Fallback Go version
		if strings.HasPrefix(embeddedGoSDKZipName, "go") {
			parts := strings.Split(strings.TrimPrefix(embeddedGoSDKZipName, "go"), ".")
			if len(parts) >= 2 {
				majorMinor := parts[0] + "." + parts[1]
				if _, errConv := strconv.Atoi(parts[0]); errConv == nil { // Basic check
					if _, errConv2 := strconv.Atoi(parts[1]); errConv2 == nil {
						goVersionForMod = majorMinor
					}
				}
			}
		}
		log.Printf("[res-mod] Using Go version '%s' for generated go.mod files.", goVersionForMod)

		// Content for the main temporary module's go.mod
		// It replaces its own 'pkg' directory to point to the one we are about to extract.
		tempGoModContent := fmt.Sprintf(`
module tempbuildmodule

go %s

// These are direct dependencies of client_template and server_template main packages
// Ensure they match what's actually used.
require (
	github.com/google/uuid v1.6.0
	github.com/libp2p/go-libp2p v0.37.0
	github.com/libp2p/go-libp2p-kad-dht v0.28.0
	github.com/multiformats/go-multiaddr v0.13.0
	golang.org/x/sys v0.26.0
	modernc.org/sqlite v1.30.2
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // For logging
)

// This replace directive is crucial for the build process.
// It tells the Go compiler that any import of "github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/..."
// should resolve to the "./pkg" subdirectory within this temporary module.
replace %s/pkg => ./pkg
`, goVersionForMod, mainAppModulePath) // mainAppModulePath is the module path of gui_generator_app

		if errWriteMod := os.WriteFile(filepath.Join(tempModuleRoot, "go.mod"), []byte(strings.TrimSpace(tempGoModContent)), 0644); errWriteMod != nil {
			temporaryModuleRootPathErr = fmt.Errorf("writing main go.mod for temp module: %w", errWriteMod)
			progress("Error writing temp go.mod.", 100)
			return
		}
		progress("Temporary main go.mod created.", 5)

		// --- Extract generator/templates ---
		// Destination structure: <tempModuleRoot>/generator/templates/client_template
		//                                                            /server_template
		overallTemplatesBaseDir := filepath.Join(tempModuleRoot, "generator", "templates")

		// Extract client_template
		clientTemplatesDestDir := filepath.Join(overallTemplatesBaseDir, "client_template")
		progress("Extracting client templates...", 10)
		errExtractClientTpl := extractFSDirContents(ctx, embeddedClientTemplateFS, clientTemplateEmbedRelPath, clientTemplatesDestDir, "client templates", func(msg string, p int) {
			progress(msg, 10+int(float64(p)*0.20)) // 20% of total progress for client templates
		})
		if errExtractClientTpl != nil {
			temporaryModuleRootPathErr = fmt.Errorf("extracting client_template: %w", errExtractClientTpl)
			progress("Error extracting client templates.", 100)
			return
		}
		log.Printf("[res-mod] 'client_template' extracted to: %s", clientTemplatesDestDir)
		progress("Client templates extracted.", 30)

		// Extract server_template
		serverTemplatesDestDir := filepath.Join(overallTemplatesBaseDir, "server_template")
		progress("Extracting server templates...", 30)
		errExtractServerTpl := extractFSDirContents(ctx, embeddedServerTemplateFS, serverTemplateEmbedRelPath, serverTemplatesDestDir, "server templates", func(msg string, p int) {
			progress(msg, 30+int(float64(p)*0.20)) // 20% for server templates
		})
		if errExtractServerTpl != nil {
			temporaryModuleRootPathErr = fmt.Errorf("extracting server_template: %w", errExtractServerTpl)
			progress("Error extracting server templates.", 100)
			return
		}
		log.Printf("[res-mod] 'server_template' extracted to: %s", serverTemplatesDestDir)
		progress("Server templates extracted.", 50)

		// --- Extract shared pkg directory ---
		// Destination structure: <tempModuleRoot>/pkg/config, pkg/crypto, etc.
		basePkgDestDir := filepath.Join(tempModuleRoot, "pkg")
		pkgSubDirs := []struct {
			fsVar          embed.FS
			embedRelPath   string
			name           string // Name of the sub-package, e.g., "config"
			progressOffset int
			progressWeight float64
		}{
			{embeddedPkgConfigFS, pkgConfigEmbedRelPath, "config", 50, 0.10},
			{embeddedPkgCryptoFS, pkgCryptoEmbedRelPath, "crypto", 60, 0.10},
			{embeddedPkgP2PFS, pkgP2PEmbedRelPath, "p2p", 70, 0.10},
			{embeddedPkgTypesFS, pkgTypesEmbedRelPath, "types", 80, 0.10},
		}

		for _, subDir := range pkgSubDirs {
			// The destination path for each sub-package is basePkgDestDir + subDir.name
			destPath := filepath.Join(basePkgDestDir, subDir.name)
			progress(fmt.Sprintf("Extracting pkg/%s...", subDir.name), subDir.progressOffset)
			errExtract := extractFSDirContents(ctx, subDir.fsVar, subDir.embedRelPath, destPath, "pkg/"+subDir.name, func(msg string, p int) {
				progress(msg, subDir.progressOffset+int(float64(p)*subDir.progressWeight))
			})
			if errExtract != nil {
				temporaryModuleRootPathErr = fmt.Errorf("extracting pkg/%s: %w", subDir.name, errExtract)
				progress(fmt.Sprintf("Error extracting pkg/%s.", subDir.name), 100)
				return
			}
			log.Printf("[res-mod] 'pkg/%s' extracted to: %s", subDir.name, destPath)
		}
		progress("All shared packages (pkg) extracted.", 90)

		// Create a go.mod for the 'pkg' directory itself.
		// This is needed if 'pkg' contains its own sub-packages that import each other,
		// or if 'go mod tidy' on the root module needs to understand 'pkg' as a module.
		// The module path for this go.mod should be the one used in the 'replace' directive.
		pkgGoModContent := fmt.Sprintf(`
module %s/pkg

go %s
// No explicit requires here, as this is a library module.
// Dependencies are managed by the main tempbuildmodule's go.mod.
`, mainAppModulePath, goVersionForMod)

		pkgGoModPath := filepath.Join(basePkgDestDir, "go.mod")
		if errWritePkgMod := os.WriteFile(pkgGoModPath, []byte(strings.TrimSpace(pkgGoModContent)), 0644); errWritePkgMod != nil {
			temporaryModuleRootPathErr = fmt.Errorf("writing pkg/go.mod: %w", errWritePkgMod)
			progress("Error writing temporary pkg/go.mod.", 100)
			return
		}
		log.Printf("[res-mod] 'pkg/go.mod' created at: %s", pkgGoModPath)
		progress("Temporary pkg/go.mod created.", 95)

		// Final checks for key files
		clientMainGoPath := filepath.Join(clientTemplatesDestDir, "main.go")
		if _, errStat := os.Stat(clientMainGoPath); os.IsNotExist(errStat) {
			temporaryModuleRootPathErr = fmt.Errorf("post-extraction check: client_template/main.go ('%s') missing", clientMainGoPath)
			progress("Client main.go missing post-extraction.", 100)
			return
		}
		log.Printf("[res-mod-check] Verified existence of: %s", clientMainGoPath)

		serverMainGoPath := filepath.Join(serverTemplatesDestDir, "main.go")
		if _, errStat := os.Stat(serverMainGoPath); os.IsNotExist(errStat) {
			temporaryModuleRootPathErr = fmt.Errorf("post-extraction check: server_template/main.go ('%s') missing", serverMainGoPath)
			progress("Server main.go missing post-extraction.", 100)
			return
		}
		log.Printf("[res-mod-check] Verified existence of: %s", serverMainGoPath)

		pkgConfigFinalPath := filepath.Join(basePkgDestDir, "config", "models.go") // Check a specific file
		if _, errStat := os.Stat(pkgConfigFinalPath); os.IsNotExist(errStat) {
			temporaryModuleRootPathErr = fmt.Errorf("post-extraction check: pkg/config/models.go ('%s') missing", pkgConfigFinalPath)
			progress("pkg/config/models.go missing post-extraction.", 100)
			return
		}
		log.Printf("[res-mod-check] Verified existence of: %s", pkgConfigFinalPath)

		temporaryModuleRootPath = tempModuleRoot
		progress("Temporary Go module ready for compilation.", 100)
		log.Printf("[res-mod] Temporary module structure fully ready at: %s", temporaryModuleRootPath)
	})
	return temporaryModuleRootPath, temporaryModuleRootCleanupFunc, temporaryModuleRootPathErr
}

// extractFSDirContents extracts all files and directories from an embed.FS (starting at srcRootInEmbedFS)
// to a destination on disk (destDiskDir).
func extractFSDirContents(ctx context.Context, embedFS embed.FS, srcRootInEmbedFS string, destDiskDir string, extractionName string, progressSubOp ProgressFunc) error {
	log.Printf("[extractFS-%s] Extracting from embedFS (src root in embed: '%s') to disk dir '%s'", extractionName, srcRootInEmbedFS, destDiskDir)

	if err := os.MkdirAll(destDiskDir, 0755); err != nil {
		return fmt.Errorf("creating base destination directory '%s' for %s: %w", destDiskDir, extractionName, err)
	}

	var filesProcessed uint64
	var totalFilesToProcess uint64

	// Count total files for progress calculation
	errWalkCount := fs.WalkDir(embedFS, srcRootInEmbedFS, func(path string, d fs.DirEntry, errIn error) error {
		if errIn != nil {
			// Log and skip problematic entries during count, but don't fail the whole count.
			log.Printf("[extractFS-%s] Warning: error accessing path '%s' during count: %v", extractionName, path, errIn)
			if d != nil && d.IsDir() { // if it's a dir and error, skip it
				return fs.SkipDir
			}
			return nil // if it's a file and error, just skip this file from count
		}
		if !d.IsDir() {
			totalFilesToProcess++
		}
		return nil
	})
	if errWalkCount != nil { // Should not happen if WalkDirFunc returns nil for errors
		log.Printf("[extractFS-%s] Error during file count walk: %v", extractionName, errWalkCount)
		// Continue, progress might be inaccurate
	}

	if totalFilesToProcess == 0 && srcRootInEmbedFS != "." { // Check if srcRootInEmbedFS is valid
		// Check if the source root itself exists
		if _, errStat := fs.Stat(embedFS, srcRootInEmbedFS); os.IsNotExist(errStat) {
			return fmt.Errorf("source root '%s' does not exist in embedded FS for %s", srcRootInEmbedFS, extractionName)
		}
		log.Printf("[extractFS-%s] No files to extract from embedFS (src root in embed: '%s'). This might be okay if it's an empty directory.", extractionName, srcRootInEmbedFS)
		if progressSubOp != nil {
			progressSubOp(fmt.Sprintf("No files in %s.", extractionName), 100)
		}
		// return nil // Not an error if there are truly no files
	}
	log.Printf("[extractFS-%s] Total files to extract: %d", extractionName, totalFilesToProcess)

	return fs.WalkDir(embedFS, srcRootInEmbedFS, func(pathInEmbed string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("[extractFS-%s] Error walking at '%s': %v", extractionName, pathInEmbed, walkErr)
			// If it's a directory, try to skip it. If a file, this error will propagate.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return walkErr
		}

		select {
		case <-ctx.Done():
			log.Printf("[extractFS-%s] Extraction cancelled via context for path %s", extractionName, pathInEmbed)
			return ctx.Err() // Propagate context cancellation
		default:
		}

		// Calculate relative path from the srcRootInEmbedFS to the current pathInEmbed
		relativePath, errRel := filepath.Rel(srcRootInEmbedFS, pathInEmbed)
		if errRel != nil {
			return fmt.Errorf("calculating relative path for '%s' (base '%s') in %s: %w", pathInEmbed, srcRootInEmbedFS, extractionName, errRel)
		}

		// If relativePath is ".", it means pathInEmbed is the same as srcRootInEmbedFS.
		// We've already created destDiskDir which corresponds to srcRootInEmbedFS, so skip.
		if relativePath == "." {
			return nil
		}

		destPathOnDisk := filepath.Join(destDiskDir, relativePath)

		if d.IsDir() {
			if errMk := os.MkdirAll(destPathOnDisk, 0755); errMk != nil {
				return fmt.Errorf("creating directory '%s' for %s: %w", destPathOnDisk, extractionName, errMk)
			}
			return nil // Done with this directory entry
		}

		// It's a file, ensure parent directory on disk exists
		if errMkParent := os.MkdirAll(filepath.Dir(destPathOnDisk), 0755); errMkParent != nil {
			return fmt.Errorf("creating parent directory for file '%s' for %s: %w", destPathOnDisk, extractionName, errMkParent)
		}

		fileData, readErr := embedFS.ReadFile(pathInEmbed)
		if readErr != nil {
			return fmt.Errorf("reading embedded %s file '%s': %w", extractionName, pathInEmbed, readErr)
		}

		if errWrite := os.WriteFile(destPathOnDisk, fileData, 0644); errWrite != nil { // Using 0644 for files
			return fmt.Errorf("writing %s file '%s' to disk: %w", extractionName, destPathOnDisk, errWrite)
		}

		filesProcessed++
		if progressSubOp != nil {
			percent := 0
			if totalFilesToProcess > 0 { // Avoid division by zero
				percent = int(float64(filesProcessed) * 100.0 / float64(totalFilesToProcess))
			}
			// Format message carefully for progress display
			msg := formatProgressMessage(fmt.Sprintf("Extracting %s: %%s", extractionName), relativePath, progressMessageMaxDisplayLength)
			progressSubOp(msg, percent)
		}
		return nil
	})
}
