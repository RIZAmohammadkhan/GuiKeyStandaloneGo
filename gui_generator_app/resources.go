// File: GuiKeyStandaloneGo/gui_generator_app/resources.go
package main

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
	"strings"
	"sync"
)

// --- Go SDK Embedding ---

//go:embed all:embedded_go_sdk
var embeddedGoSDKFS embed.FS

const embeddedGoSDKZipName = "go1.24.3.windows-amd64.zip"
const embeddedGoSDKZipPath = "embedded_go_sdk/" + embeddedGoSDKZipName
const embeddedGoSDKSHA256 = "be9787cb08998b1860fe3513e48a5fe5b96302d358a321b58e651184fa9638b3"

// --- Source Code Embedding ---
//
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

// progressMessageMaxDisplayLength is the target maximum length for messages passed to ProgressFunc.
// This helps keep progress updates concise for display in UIs or logs.
// Adjusted for typical log prefixes like "[TAG] XX%: " on an 80-char line, leaving ~55 chars for the message itself.
const progressMessageMaxDisplayLength = 55

// truncateString shortens a string to a maximum length, appending "..." if truncation occurs
// and if maxLength allows for it (>=3 characters). If maxLength is too small for "...",
// it truncates to maxLength characters. If maxLength <= 0, returns an empty string.
func truncateString(s string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}
	if len(s) <= maxLength {
		return s
	}
	if maxLength < 3 { // Not enough space for "..."
		return s[:maxLength]
	}
	return s[:maxLength-3] + "..."
}

// formatProgressMessage formats a message string that has a single dynamic argument (%s).
// It ensures the total length of the resulting string attempts to stay within maxTotalLen.
// It prioritizes showing static parts of the format string and truncates dynamicArg to fit.
// If static parts alone exceed maxTotalLen, the entire formatted string will be truncated.
// Assumes 'format' contains exactly one '%s' placeholder for dynamicArg.
func formatProgressMessage(format string, dynamicArg string, maxTotalLen int) string {
	staticLen := 0
	// Ensure format string actually contains "%s"
	parts := strings.SplitN(format, "%s", 2)
	if len(parts) < 2 { // %s not found in format
		log.Printf("[WARN] formatProgressMessage used with format string lacking %%s: '%s'", format)
		// Treat format as fully static, dynamicArg is ignored.
		return truncateString(format, maxTotalLen)
	}
	staticLen += len(parts[0])
	staticLen += len(parts[1]) // parts[1] is empty if %s is at the end of the format string

	availableForDynamic := maxTotalLen - staticLen
	// truncateString handles non-positive availableForDynamic correctly (e.g., returns "" or clips)

	truncatedDynamicArg := truncateString(dynamicArg, availableForDynamic)

	finalMessage := fmt.Sprintf(format, truncatedDynamicArg)

	// Final check: if static parts were too long, Sprintf added unexpected length,
	// or availableForDynamic was negative, ensure the final message respects maxTotalLen.
	if len(finalMessage) > maxTotalLen {
		return truncateString(finalMessage, maxTotalLen)
	}

	return finalMessage
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
		hasher.Write(zipBytes)
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
				os.MkdirAll(fpath, os.ModePerm)
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
				extractedGoPathErr = fmt.Errorf("opening %s: %w", fpath, errOpen)
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
			outFile.Close()
			rc.Close()
			if errCopy != nil {
				extractedGoPathErr = fmt.Errorf("copying %s: %w", f.Name, errCopy)
				progressMsg := formatProgressMessage("Error copying file %s", f.Name, progressMessageMaxDisplayLength)
				progress(progressMsg, 100)
				return
			}

			filesExtracted++
			currentProgress := 15 + int(float64(filesExtracted)*80.0/float64(totalFiles))
			progressMsg := formatProgressMessage("Extracting Go SDK: %s", filepath.Base(f.Name), progressMessageMaxDisplayLength)
			progress(progressMsg, currentProgress)
		}

		exePathTemp := filepath.Join(tempDir, "go", "bin", "go.exe")
		if _, errStat := os.Stat(exePathTemp); os.IsNotExist(errStat) {
			extractedGoPathErr = fmt.Errorf("go.exe not found at expected path %s after extraction. Check embedded zip structure", exePathTemp)
			progress("go.exe not found post-extraction.", 100)
			return
		}
		extractedGoPath = exePathTemp
		progress("Go environment ready.", 100)
		log.Printf("[res-go] Extracted Go executable: %s", extractedGoPath)
	})

	return extractedGoPath, extractedGoCleanupFunc, extractedGoPathErr
}

func extractFSDirContents(ctx context.Context, embedFS embed.FS, srcRootInEmbedFS string, destDiskDir string, extractionName string, progressSubOp ProgressFunc) error {
	log.Printf("[extractFS-%s] Extracting from embedFS (src root in embed: '%s') to disk dir '%s'", extractionName, srcRootInEmbedFS, destDiskDir)

	if err := os.MkdirAll(destDiskDir, 0755); err != nil {
		return fmt.Errorf("creating base destination directory '%s' for %s: %w", destDiskDir, extractionName, err)
	}

	var filesProcessed uint64
	var totalFilesToProcess uint64

	fs.WalkDir(embedFS, srcRootInEmbedFS, func(path string, d fs.DirEntry, errIn error) error {
		if errIn != nil {
			log.Printf("[extractFS-%s] Warning: error accessing path '%s' during count: %v", extractionName, path, errIn)
			return fs.SkipDir
		}
		if !d.IsDir() {
			totalFilesToProcess++
		}
		return nil
	})

	if totalFilesToProcess == 0 {
		log.Printf("[extractFS-%s] No files to extract from embedFS (src root in embed: '%s').", extractionName, srcRootInEmbedFS)
		if progressSubOp != nil {
			// Message is short, direct call is fine.
			progressSubOp(fmt.Sprintf("No files in %s.", extractionName), 100)
		}
		return nil
	}
	log.Printf("[extractFS-%s] Total files to extract: %d", extractionName, totalFilesToProcess)

	return fs.WalkDir(embedFS, srcRootInEmbedFS, func(pathInEmbed string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("[extractFS-%s] Error walking at '%s': %v", extractionName, pathInEmbed, walkErr)
			return walkErr
		}

		select {
		case <-ctx.Done():
			log.Printf("[extractFS-%s] Extraction cancelled via context for path %s", extractionName, pathInEmbed)
			return ctx.Err()
		default:
		}

		relativePath, errRel := filepath.Rel(srcRootInEmbedFS, pathInEmbed)
		if errRel != nil {
			return fmt.Errorf("calculating relative path for '%s' from '%s' in %s: %w", pathInEmbed, srcRootInEmbedFS, extractionName, errRel)
		}
		if relativePath == "." {
			return nil
		}

		destPathOnDisk := filepath.Join(destDiskDir, relativePath)

		if d.IsDir() {
			if errMk := os.MkdirAll(destPathOnDisk, 0755); errMk != nil {
				return fmt.Errorf("creating directory '%s' for %s: %w", destPathOnDisk, extractionName, errMk)
			}
			return nil
		}

		if errMkParent := os.MkdirAll(filepath.Dir(destPathOnDisk), 0755); errMkParent != nil {
			return fmt.Errorf("creating parent directory for file '%s' for %s: %w", destPathOnDisk, extractionName, errMkParent)
		}

		fileData, readErr := embedFS.ReadFile(pathInEmbed)
		if readErr != nil {
			return fmt.Errorf("reading embedded %s file '%s': %w", extractionName, pathInEmbed, readErr)
		}

		if errWrite := os.WriteFile(destPathOnDisk, fileData, 0644); errWrite != nil {
			return fmt.Errorf("writing %s file '%s' to disk: %w", extractionName, destPathOnDisk, errWrite)
		}

		filesProcessed++
		if progressSubOp != nil {
			percent := 0
			if totalFilesToProcess > 0 {
				percent = int(float64(filesProcessed) * 100.0 / float64(totalFilesToProcess))
			}

			// Custom handling for "Extracting %s: %s" format
			// extractionName is e.g., "client templates", "pkg/config" - usually short.
			// relativePath can be long.
			firstPart := fmt.Sprintf("Extracting %s: ", extractionName)

			availableForRelativePath := progressMessageMaxDisplayLength - len(firstPart)
			// Ensure availableForRelativePath is not excessively small if firstPart is unexpectedly long
			if availableForRelativePath < 0 { // Should ideally not happen with known extractionNames
				availableForRelativePath = 0
			}
			truncatedRelativePath := truncateString(relativePath, availableForRelativePath)

			message := firstPart + truncatedRelativePath

			// Final safety truncate, in case firstPart was already too long (unlikely for known extractionNames)
			// or if Sprintf added unexpected length.
			if len(message) > progressMessageMaxDisplayLength {
				message = truncateString(message, progressMessageMaxDisplayLength)
			}

			progressSubOp(message, percent)
		}
		return nil
	})
}

func GetTemporaryModulePath(ctx context.Context, progress ProgressFunc) (moduleRootPath string, cleanupFunc func() error, err error) {
	temporaryModuleRootPathOnce.Do(func() {
		log.Println("[res-mod] First call to GetTemporaryModulePath, setting up temp module.")
		if progress == nil {
			progress = func(m string, p int) { log.Printf("[res-mod-progress] %d%%: %s", p, m) }
		}
		// Static progress messages are generally short and don't need truncation.
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

		var goVersionForMod = "1.24.3"
		if strings.HasPrefix(embeddedGoSDKZipName, "go") && strings.Contains(embeddedGoSDKZipName, ".windows-amd64.zip") {
			versionPart := strings.TrimPrefix(embeddedGoSDKZipName, "go")
			versionPart = strings.TrimSuffix(versionPart, ".windows-amd64.zip")
			if len(versionPart) > 0 && strings.Count(versionPart, ".") >= 1 {
				goVersionForMod = versionPart
			} else {
				log.Printf("[res-mod] Could not parse Go version from SDK name '%s', using default '%s'", embeddedGoSDKZipName, goVersionForMod)
			}
		}
		log.Printf("[res-mod] Using Go version '%s' for generated go.mod files.", goVersionForMod)

		tempGoModContent := fmt.Sprintf(`
module tempbuildmodule

go %s

require (
	github.com/google/uuid v1.6.0
	github.com/libp2p/go-libp2p v0.37.0 
	github.com/libp2p/go-libp2p-kad-dht v0.28.0 
	github.com/multiformats/go-multiaddr v0.13.0 
	golang.org/x/sys v0.26.0 
	modernc.org/sqlite v1.30.2 
)

replace %[2]s/pkg => ./pkg
`, goVersionForMod, mainAppModulePath)

		log.Println("[res-mod] Writing generated go.mod to temp module root.")
		if errWriteMod := os.WriteFile(filepath.Join(tempModuleRoot, "go.mod"), []byte(strings.TrimSpace(tempGoModContent)), 0644); errWriteMod != nil {
			temporaryModuleRootPathErr = fmt.Errorf("writing generated go.mod: %w", errWriteMod)
			progress("Error writing temp go.mod.", 100)
			return
		}
		progress("Temporary go.mod created.", 5)

		overallTemplatesBaseDir := filepath.Join(tempModuleRoot, "generator", "templates")
		clientTemplatesDestDir := filepath.Join(overallTemplatesBaseDir, "client_template")
		progress("Extracting client templates...", 5) // Static message
		errExtractClientTpl := extractFSDirContents(ctx, embeddedClientTemplateFS, clientTemplateEmbedRelPath, clientTemplatesDestDir, "client templates", func(msg string, p int) {
			progress(msg, 5+int(float64(p)*0.15)) // msg is already truncated by extractFSDirContents
		})
		if errExtractClientTpl != nil {
			temporaryModuleRootPathErr = fmt.Errorf("extracting client_template: %w", errExtractClientTpl)
			progress("Error extracting client templates.", 100)
			return
		}
		log.Printf("[res-mod] 'client_template' extracted to: %s", clientTemplatesDestDir)
		progress("Client templates extracted.", 20)

		serverTemplatesDestDir := filepath.Join(overallTemplatesBaseDir, "server_template")
		progress("Extracting server templates...", 20) // Static message
		errExtractServerTpl := extractFSDirContents(ctx, embeddedServerTemplateFS, serverTemplateEmbedRelPath, serverTemplatesDestDir, "server templates", func(msg string, p int) {
			progress(msg, 20+int(float64(p)*0.15)) // msg is already truncated by extractFSDirContents
		})
		if errExtractServerTpl != nil {
			temporaryModuleRootPathErr = fmt.Errorf("extracting server_template: %w", errExtractServerTpl)
			progress("Error extracting server templates.", 100)
			return
		}
		log.Printf("[res-mod] 'server_template' extracted to: %s", serverTemplatesDestDir)
		progress("Server templates extracted.", 35)

		basePkgDestDir := filepath.Join(tempModuleRoot, "pkg")

		pkgSubDirs := []struct {
			fsVar          embed.FS
			embedRelPath   string
			name           string
			progressOffset int
			progressWeight float64
		}{
			{embeddedPkgConfigFS, pkgConfigEmbedRelPath, "config", 35, 0.13},
			{embeddedPkgCryptoFS, pkgCryptoEmbedRelPath, "crypto", 48, 0.13},
			{embeddedPkgP2PFS, pkgP2PEmbedRelPath, "p2p", 61, 0.13},
			{embeddedPkgTypesFS, pkgTypesEmbedRelPath, "types", 74, 0.13},
		}

		for _, subDir := range pkgSubDirs {
			destPath := filepath.Join(basePkgDestDir, subDir.name)
			// Static part of this message is fine.
			progress(fmt.Sprintf("Extracting pkg/%s...", subDir.name), subDir.progressOffset)
			errExtract := extractFSDirContents(ctx, subDir.fsVar, subDir.embedRelPath, destPath, "pkg/"+subDir.name, func(msg string, p int) {
				// msg is already truncated by extractFSDirContents
				progress(msg, subDir.progressOffset+int(float64(p)*subDir.progressWeight))
			})
			if errExtract != nil {
				temporaryModuleRootPathErr = fmt.Errorf("extracting pkg/%s: %w", subDir.name, errExtract)
				progress(fmt.Sprintf("Error extracting pkg/%s.", subDir.name), 100)
				return
			}
			log.Printf("[res-mod] 'pkg/%s' extracted to: %s", subDir.name, destPath)
		}
		progress("All shared packages (pkg) extracted.", 87)

		pkgGoModContent := fmt.Sprintf(`
module %s/pkg

go %s
`, mainAppModulePath, goVersionForMod)

		pkgGoModPath := filepath.Join(basePkgDestDir, "go.mod")
		log.Printf("[res-mod] Writing go.mod for the 'pkg' module to: %s", pkgGoModPath)
		if errWritePkgMod := os.WriteFile(pkgGoModPath, []byte(strings.TrimSpace(pkgGoModContent)), 0644); errWritePkgMod != nil {
			temporaryModuleRootPathErr = fmt.Errorf("writing pkg/go.mod: %w", errWritePkgMod)
			progress("Error writing temporary pkg/go.mod.", 100)
			return
		}
		progress("Temporary pkg/go.mod created.", 95)

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

		pkgConfigFinalPath := filepath.Join(basePkgDestDir, "config")
		if _, errStat := os.Stat(pkgConfigFinalPath); os.IsNotExist(errStat) {
			temporaryModuleRootPathErr = fmt.Errorf("post-extraction check: pkg/config directory '%s' missing", pkgConfigFinalPath)
			progress("pkg/config missing post-extraction.", 100)
			return
		}
		log.Printf("[res-mod-check] Verified existence of: %s", pkgConfigFinalPath)

		temporaryModuleRootPath = tempModuleRoot
		progress("Temporary Go module ready for compilation.", 100)
		log.Printf("[res-mod] Temporary module structure fully ready at: %s", temporaryModuleRootPath)
	})
	return temporaryModuleRootPath, temporaryModuleRootCleanupFunc, temporaryModuleRootPathErr
}
