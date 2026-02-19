package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Vault handles file and folder operations within the vault directory
type Vault struct {
	path     string
	checksums *Checksums
	matcher  Matcher
}

// Matcher is an interface for ignore pattern matching
type Matcher interface {
	Match(path string) bool
	MatchDir(dirPath string) bool
}

// NewVault creates a new vault manager
func NewVault(path string, checksums *Checksums, matcher Matcher) *Vault {
	return &Vault{
		path:     path,
		checksums: checksums,
		matcher:  matcher,
	}
}

// ValidatePath checks that a path is safe and doesn't escape the vault
func (v *Vault) ValidatePath(relPath string) (string, error) {
	// Clean path
	relPath = filepath.Clean(relPath)
	relPath = filepath.ToSlash(relPath)

	// Check for path traversal attempts
	if strings.Contains(relPath, "..") {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}

	// Join with vault path and resolve
	fullPath := filepath.Join(v.path, relPath)

	// Ensure resolved path is within vault
	absVault, err := filepath.Abs(v.path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve vault path: %w", err)
	}

	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve file path: %w", err)
	}

	rel, err := filepath.Rel(absVault, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes vault: %s", relPath)
	}

	return fullPath, nil
}

// GetFile returns the absolute path to a file if it exists
func (v *Vault) GetFile(relPath string) (string, error) {
	fullPath, err := v.ValidatePath(relPath)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s", relPath)
		}
		return "", err
	}

	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", relPath)
	}

	return fullPath, nil
}

// PutFile writes a file to the vault
func (v *Vault) PutFile(relPath string, content []byte) error {
	fullPath, err := v.ValidatePath(relPath)
	if err != nil {
		return err
	}

	// Create parent directories if needed
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Write file
	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// DeleteFile removes a file from the vault
func (v *Vault) DeleteFile(relPath string) error {
	fullPath, err := v.ValidatePath(relPath)
	if err != nil {
		return err
	}

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", relPath)
		}
		return err
	}

	return nil
}

// CreateFolder creates a directory in the vault
func (v *Vault) CreateFolder(relPath string) error {
	fullPath, err := v.ValidatePath(relPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(fullPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return nil
}

// DeleteFolder removes a directory and all its contents
func (v *Vault) DeleteFolder(relPath string) error {
	fullPath, err := v.ValidatePath(relPath)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(fullPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("folder not found: %s", relPath)
		}
		return err
	}

	return nil
}

// Rename renames a file or folder
func (v *Vault) Rename(fromPath, toPath string) error {
	fromFull, err := v.ValidatePath(fromPath)
	if err != nil {
		return err
	}

	toFull, err := v.ValidatePath(toPath)
	if err != nil {
		return err
	}

	// Create parent directory for destination if needed
	toDir := filepath.Dir(toFull)
	if err := os.MkdirAll(toDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if err := os.Rename(fromFull, toFull); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source not found: %s", fromPath)
		}
		return err
	}

	return nil
}

// FileExists checks if a file exists
func (v *Vault) FileExists(relPath string) bool {
	fullPath, err := v.ValidatePath(relPath)
	if err != nil {
		return false
	}

	_, err = os.Stat(fullPath)
	return err == nil
}

// ListEmptyFolders returns a list of empty directories in the vault (respects ignore patterns)
func (v *Vault) ListEmptyFolders() ([]string, error) {
	var emptyDirs []string

	err := filepath.Walk(v.path, func(fullPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip the vault root
		if fullPath == v.path {
			return nil
		}

		// Only check directories
		if info.IsDir() {
			// Get relative path for ignore matching
			relPath, err := filepath.Rel(v.path, fullPath)
			if err != nil {
				return nil
			}
			relPath = filepath.ToSlash(relPath)

			// Skip ignored directories
			if v.matcher != nil && v.matcher.MatchDir(relPath) {
				return filepath.SkipDir
			}

			// Check if directory is empty (no files or subdirs)
			entries, err := os.ReadDir(fullPath)
			if err != nil {
				return nil
			}
			if len(entries) == 0 {
				emptyDirs = append(emptyDirs, relPath)
			}
		}

		return nil
	})

	return emptyDirs, err
}

// CleanEmptyFolders removes all empty directories from the vault (respects ignore patterns)
func (v *Vault) CleanEmptyFolders() (int, error) {
	removed := 0

	err := filepath.Walk(v.path, func(fullPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip the vault root
		if fullPath == v.path {
			return nil
		}

		// Only check directories
		if info.IsDir() {
			// Get relative path for ignore matching
			relPath, err := filepath.Rel(v.path, fullPath)
			if err != nil {
				return nil
			}
			relPath = filepath.ToSlash(relPath)

			// Skip ignored directories
			if v.matcher != nil && v.matcher.MatchDir(relPath) {
				return filepath.SkipDir
			}

			// Check if directory is empty
			entries, err := os.ReadDir(fullPath)
			if err != nil {
				return nil
			}
			if len(entries) == 0 {
				// Remove the empty directory
				if err := os.Remove(fullPath); err == nil {
					removed++
				}
			}
		}

		return nil
	})

	return removed, err
}

// MimeType detects the MIME type of a file based on extension
func MimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))

	mimeTypes := map[string]string{
		".md":     "text/markdown",
		".txt":    "text/plain",
		".html":    "text/html",
		".css":     "text/css",
		".js":      "application/javascript",
		".json":    "application/json",
		".xml":     "application/xml",
		".pdf":     "application/pdf",
		".zip":     "application/zip",
		".png":     "image/png",
		".jpg":     "image/jpeg",
		".jpeg":    "image/jpeg",
		".gif":     "image/gif",
		".svg":     "image/svg+xml",
		".webp":    "image/webp",
		".mp3":     "audio/mpeg",
		".mp4":     "video/mp4",
		".wav":     "audio/wav",
		".ogg":     "audio/ogg",
		".webm":    "video/webm",
		".tif":     "image/tiff",
		".tiff":    "image/tiff",
		".bmp":     "image/bmp",
		".ico":     "image/x-icon",
	}

	if mt, ok := mimeTypes[ext]; ok {
		return mt
	}

	return "application/octet-stream"
}
