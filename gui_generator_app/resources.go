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
//
//go:embed all:embedded_go_sdk
var embeddedGoSDKFS embed.FS

const embeddedGoSDKZipName = "go1.24.3.windows-amd64.zip" // UPDATE THIS
const embeddedGoSDKZipPath = "embedded_go_sdk/" + embeddedGoSDKZipName
const embeddedGoSDKSHA256 = "be9787cb08998b1860fe3513e48a5fe5b96302d358a321b58e651184fa9638b3" // UPDATE THIS (for go1.22.0)

// --- Source Code Embedding ---
//
//go:embed all:generator/templates
var embeddedGeneratorTemplatesFS embed.FS

//go:embed all:pkg
var embeddedPkgFS embed.FS

// IMPORTANT: Replace "guikeygenapp" with your actual module path from gui_generator_app/go.mod
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

func GetGoExecutablePath(ctx context.Context, progress ProgressFunc) (exePath string, cleanupFunc func() error, err error) {
	extractedGoPathOnce.Do(func() {
		log.Println("[res-go] First call to GetGoExecutablePath, proceeding with extraction.")
		if progress == nil {
			progress = func(m string, p int) { log.Printf("[res-go-progress] %d%%: %s", p, m) }
		}
		progress("Reading embedded Go SDK...", 5)
		zipBytes, readErr := embeddedGoSDKFS.ReadFile(embeddedGoSDKZipPath)
		if readErr != nil {
			extractedGoPathErr = fmt.Errorf("reading embedded Go SDK '%s': %w", embeddedGoSDKZipPath, readErr)
			return
		}
		progress("Verifying Go SDK checksum...", 10)
		hasher := sha256.New()
		hasher.Write(zipBytes)
		actualSHA := hex.EncodeToString(hasher.Sum(nil))
		if actualSHA != embeddedGoSDKSHA256 {
			extractedGoPathErr = fmt.Errorf("Go SDK checksum MISMATCH! Expected: %s, Got: %s", embeddedGoSDKSHA256, actualSHA)
			return
		}
		progress("Go SDK checksum verified.", 15)
		tempDir, mkErr := os.MkdirTemp("", "guikey-go-sdk-")
		if mkErr != nil {
			extractedGoPathErr = fmt.Errorf("creating temp dir for Go SDK: %w", mkErr)
			return
		}
		extractedGoCleanupFunc = func() error { return os.RemoveAll(tempDir) }
		progress("Extracting Go SDK...", 20)
		zipReader, zipErr := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
		if zipErr != nil {
			extractedGoPathErr = fmt.Errorf("creating zip reader for Go SDK: %w", zipErr)
			return
		}
		totalFiles := uint64(len(zipReader.File))
		if totalFiles == 0 {
			extractedGoPathErr = fmt.Errorf("Go SDK zip empty")
			return
		}
		var filesExtracted uint64
		for _, f := range zipReader.File {
			// Corrected select for non-blocking context check
			select {
			case <-ctx.Done():
				log.Println("[res-go] Go SDK extraction cancelled by user via context.")
				extractedGoPathErr = ctx.Err()
				return
			default:
				// Continue if not cancelled
			}

			fpath := filepath.Join(tempDir, f.Name)
			if !strings.HasPrefix(fpath, filepath.Clean(tempDir)+string(os.PathSeparator)) {
				extractedGoPathErr = fmt.Errorf("illegal path in Go SDK zip: %s", fpath)
				return
			}
			if f.FileInfo().IsDir() {
				os.MkdirAll(fpath, os.ModePerm)
				continue
			}
			if errMkdir := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); errMkdir != nil {
				extractedGoPathErr = fmt.Errorf("creating dir for %s: %w", fpath, errMkdir)
				return
			}
			outFile, errOpen := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if errOpen != nil {
				extractedGoPathErr = fmt.Errorf("opening %s: %w", fpath, errOpen)
				return
			}
			rc, errOpenInZip := f.Open()
			if errOpenInZip != nil {
				outFile.Close()
				extractedGoPathErr = fmt.Errorf("opening in zip %s: %w", f.Name, errOpenInZip)
				return
			}
			_, errCopy := io.Copy(outFile, rc)
			outFile.Close()
			rc.Close()
			if errCopy != nil {
				extractedGoPathErr = fmt.Errorf("copying %s: %w", f.Name, errCopy)
				return
			}
			filesExtracted++
			progress(fmt.Sprintf("Extracting Go SDK: %s", filepath.Base(f.Name)), 20+int(float64(filesExtracted)*75.0/float64(totalFiles)))
		}
		exePathTemp := filepath.Join(tempDir, "go", "bin", "go.exe")
		if _, errStat := os.Stat(exePathTemp); os.IsNotExist(errStat) {
			extractedGoPathErr = fmt.Errorf("go.exe not found at %s", exePathTemp)
			return
		}
		extractedGoPath = exePathTemp
		progress("Go environment ready.", 100)
		log.Printf("[res-go] Extracted Go executable: %s", extractedGoPath)
	})
	return extractedGoPath, extractedGoCleanupFunc, extractedGoPathErr
}

func extractFSDirContents(ctx context.Context, embedFS embed.FS, srcRootInEmbedFS string, destDiskDir string, typeName string, progressSubOp ProgressFunc) error {
	log.Printf("[extractFS] Extracting %s from embedFS source root '%s' to disk dir '%s'", typeName, srcRootInEmbedFS, destDiskDir)
	var filesExtracted uint64
	var totalFiles uint64 = 0

	fs.WalkDir(embedFS, srcRootInEmbedFS, func(path string, d fs.DirEntry, errIn error) error {
		if errIn != nil {
			return fs.SkipDir
		}
		if !d.IsDir() {
			totalFiles++
		}
		return nil
	})
	if totalFiles == 0 {
		log.Printf("[extractFS] No files to extract for %s from embedFS source root '%s'. OK if %s is empty.", typeName, srcRootInEmbedFS, typeName)
		return nil
	}
	log.Printf("[extractFS] Total files to extract for %s: %d", typeName, totalFiles)

	return fs.WalkDir(embedFS, srcRootInEmbedFS, func(pathInEmbed string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Corrected select for non-blocking context check
		select {
		case <-ctx.Done():
			log.Printf("[extractFS] Extraction of %s cancelled via context for path %s", typeName, pathInEmbed)
			return ctx.Err()
		default:
			// Continue if not cancelled
		}

		relativePath, errRel := filepath.Rel(srcRootInEmbedFS, pathInEmbed)
		if errRel != nil {
			return fmt.Errorf("rel path for '%s' from '%s': %w", pathInEmbed, srcRootInEmbedFS, errRel)
		}
		if relativePath == "." {
			return nil
		}

		destPath := filepath.Join(destDiskDir, relativePath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		if errMkParent := os.MkdirAll(filepath.Dir(destPath), 0755); errMkParent != nil {
			return fmt.Errorf("creating parent for '%s': %w", destPath, errMkParent)
		}
		fileData, readErr := embedFS.ReadFile(pathInEmbed)
		if readErr != nil {
			return fmt.Errorf("reading embedded %s file '%s': %w", typeName, pathInEmbed, readErr)
		}
		if errWrite := os.WriteFile(destPath, fileData, 0644); errWrite != nil {
			return fmt.Errorf("writing %s file '%s': %w", typeName, destPath, errWrite)
		}
		filesExtracted++
		if progressSubOp != nil {
			progressSubOp(fmt.Sprintf("Extracting %s: %s", typeName, relativePath), int(float64(filesExtracted)*100.0/float64(totalFiles)))
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
		progress("Preparing temporary Go module...", 0)

		tempModuleRoot, mkErr := os.MkdirTemp("", "guikey-temp-module-")
		if mkErr != nil {
			temporaryModuleRootPathErr = fmt.Errorf("creating temp module root: %w", mkErr)
			return
		}
		log.Printf("[res-mod] Temporary module root created at: %s", tempModuleRoot)
		temporaryModuleRootCleanupFunc = func() error { return os.RemoveAll(tempModuleRoot) }

		tempGoModContent := fmt.Sprintf(`
module tempbuildmodule

go 1.24.3 // Match the Go version of your embedded compiler

require (
	github.com/google/uuid v1.6.0
	github.com/libp2p/go-libp2p v0.41.1
	github.com/libp2p/go-libp2p-kad-dht v0.33.1
	github.com/multiformats/go-multiaddr v0.15.0
	golang.org/x/sys v0.33.0
	modernc.org/sqlite v1.37.1
)

replace %[1]s/pkg => ./pkg
replace %[1]s/generator/templates => ./generator/templates
`, mainAppModulePath)

		log.Println("[res-mod] Writing generated go.mod to temp module root.")
		if errWriteMod := os.WriteFile(filepath.Join(tempModuleRoot, "go.mod"), []byte(strings.TrimSpace(tempGoModContent)), 0644); errWriteMod != nil {
			temporaryModuleRootPathErr = fmt.Errorf("writing generated go.mod: %w", errWriteMod)
			return
		}
		progress("Temporary module file ready.", 10)

		templatesDestDir := filepath.Join(tempModuleRoot, "generator", "templates")
		if errMk := os.MkdirAll(templatesDestDir, 0755); errMk != nil {
			temporaryModuleRootPathErr = fmt.Errorf("creating %s in temp module: %w", templatesDestDir, errMk)
			return
		}
		errExtractTpl := extractFSDirContents(ctx, embeddedGeneratorTemplatesFS, ".", templatesDestDir, "generator templates", func(msg string, p int) {
			progress(msg, 10+int(float64(p)*0.40))
		})
		if errExtractTpl != nil {
			temporaryModuleRootPathErr = fmt.Errorf("extracting generator/templates: %w", errExtractTpl)
			return
		}
		log.Printf("[res-mod] 'generator/templates' extracted to: %s", templatesDestDir)
		progress("Core templates extracted.", 50)

		pkgDestDir := filepath.Join(tempModuleRoot, "pkg")
		if errMk := os.MkdirAll(pkgDestDir, 0755); errMk != nil {
			temporaryModuleRootPathErr = fmt.Errorf("creating %s in temp module: %w", pkgDestDir, errMk)
			return
		}
		errExtractPkg := extractFSDirContents(ctx, embeddedPkgFS, ".", pkgDestDir, "shared pkg", func(msg string, p int) {
			progress(msg, 50+int(float64(p)*0.45))
		})
		if errExtractPkg != nil {
			temporaryModuleRootPathErr = fmt.Errorf("extracting pkg: %w", errExtractPkg)
			return
		}
		log.Printf("[res-mod] 'pkg' extracted to: %s", pkgDestDir)
		progress("Shared packages extracted.", 95)

		clientTplFinalPath := filepath.Join(templatesDestDir, "client_template")
		if _, errStat := os.Stat(clientTplFinalPath); os.IsNotExist(errStat) {
			temporaryModuleRootPathErr = fmt.Errorf("post-extraction check: %s missing", clientTplFinalPath)
			return
		}
		pkgConfigFinalPath := filepath.Join(pkgDestDir, "config")
		if _, errStat := os.Stat(pkgConfigFinalPath); os.IsNotExist(errStat) {
			temporaryModuleRootPathErr = fmt.Errorf("post-extraction check: %s missing", pkgConfigFinalPath)
			return
		}

		temporaryModuleRootPath = tempModuleRoot
		progress("Temporary Go module ready.", 100)
		log.Printf("[res-mod] Temporary module structure ready at: %s", temporaryModuleRootPath)
	})
	return temporaryModuleRootPath, temporaryModuleRootCleanupFunc, temporaryModuleRootPathErr
}
