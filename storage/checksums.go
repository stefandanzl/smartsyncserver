package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"smartsyncserver/ignore"
)

// FileEntry represents a file with its hash and metadata
type FileEntry struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
	Mtime int64 `json:"mtime"` // seconds since epoch
}

// Checksums manages the checksums.json file
type Checksums struct {
	path      string
	vaultPath string
	matcher   *ignore.Matcher
	data      map[string]FileEntry
	mu        sync.RWMutex
}

// NewChecksums creates a new checksums manager
func NewChecksums(checksumPath, vaultPath string, matcher *ignore.Matcher) *Checksums {
	c := &Checksums{
		path:      checksumPath,
		vaultPath: vaultPath,
		matcher:   matcher,
		data:      make(map[string]FileEntry),
	}
	c.Load()
	return c
}

// Load reads the checksums file from disk
func (c *Checksums) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			c.data = make(map[string]FileEntry)
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &c.data); err != nil {
		c.data = make(map[string]FileEntry)
		return err
	}

	return nil
}

// saveLocked writes checksums to disk without acquiring locks (caller must hold lock)
func (c *Checksums) saveLocked() error {
	data, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return err
	}

	// Write to temporary file first, then rename for atomic write
	tmpPath := c.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, c.path)
}

// Save writes the checksums to disk
func (c *Checksums) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

// Get returns the checksum for a given path
func (c *Checksums) Get(path string) (FileEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.data[path]
	return entry, ok
}

// GetAll returns all checksums
func (c *Checksums) GetAll() map[string]FileEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent race conditions
	result := make(map[string]FileEntry, len(c.data))
	for k, v := range c.data {
		result[k] = v
	}
	return result
}

// Set sets the checksum for a path
func (c *Checksums) Set(path string, entry FileEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[path] = entry
}

// Delete removes a path from checksums
func (c *Checksums) Delete(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, path)
}

// Rename updates checksums when a file/folder is renamed
func (c *Checksums) Rename(from, to string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.data[from]; ok {
		c.data[to] = entry
		delete(c.data, from)
	}
}

// Scan walks the vault and updates checksums for all files
func (c *Checksums) Scan() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	newData := make(map[string]FileEntry)

	err := filepath.Walk(c.vaultPath, func(fullPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(c.vaultPath, fullPath)
		if err != nil {
			return nil
		}

		// Normalize path separators
		relPath = filepath.ToSlash(relPath)

		// Skip vault root directory
		if relPath == "." {
			return nil
		}

		// Handle directories
		if info.IsDir() {
			// Check if directory should be ignored
			if c.matcher != nil && c.matcher.MatchDir(relPath) {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if file should be ignored
		if c.matcher != nil && c.matcher.Match(relPath) {
			return nil
		}

		// Get file info (already have info from Walk, but need stat for metadata)
		fileInfo, err := os.Stat(fullPath)
		if err != nil {
			return nil
		}

		// Calculate checksum
		checksum, err := calculateFileChecksum(fullPath)
		if err != nil {
			return nil // Skip files that can't be read
		}

		// Create FileEntry with metadata
		entry := FileEntry{
			Hash: checksum,
			Size: fileInfo.Size(),
			Mtime: fileInfo.ModTime().Unix(),
		}
		newData[relPath] = entry
		return nil
	})

	if err != nil {
		return err
	}

	c.data = newData
	return c.saveLocked()
}

// GetFileCount returns the number of tracked files
func (c *Checksums) GetFileCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data)
}

// UpdateForFile calculates and stores the checksum for a specific file
func (c *Checksums) UpdateForFile(relPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	fullPath := filepath.Join(c.vaultPath, relPath)

	// Get file info
	info, err := os.Stat(fullPath)
	if err != nil {
		return err
	}

	// Calculate checksum
	checksum, err := calculateFileChecksum(fullPath)
	if err != nil {
		return err
	}

	// Store FileEntry
	c.data[relPath] = FileEntry{
		Hash: checksum,
		Size: info.Size(),
		Mtime: info.ModTime().Unix(),
	}

	return c.saveLocked()
}

// calculateFileChecksum computes the SHA256 hash of a file
func calculateFileChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// stringsHasPrefix is like strings.HasPrefix but handles path separators
func stringsHasPrefix(s, prefix string) bool {
	return stringsHasPrefixFunc(s, prefix, strings.HasPrefix)
}

func stringsHasPrefixFunc(s, prefix string, hasPrefixFunc func(string, string) bool) bool {
	if hasPrefixFunc(s, prefix) {
		return true
	}
	// Try with path separator
	return hasPrefixFunc(s, prefix+"/")
}
