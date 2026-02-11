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

// Checksums manages the checksums.json file
type Checksums struct {
	path      string
	vaultPath string
	matcher   *ignore.Matcher
	data      map[string]string
	mu        sync.RWMutex
}

// NewChecksums creates a new checksums manager
func NewChecksums(checksumPath, vaultPath string, matcher *ignore.Matcher) *Checksums {
	c := &Checksums{
		path:      checksumPath,
		vaultPath: vaultPath,
		matcher:   matcher,
		data:      make(map[string]string),
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
			c.data = make(map[string]string)
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &c.data); err != nil {
		c.data = make(map[string]string)
		return err
	}

	return nil
}

// Save writes the checksums to disk
func (c *Checksums) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

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

// Get returns the checksum for a given path
func (c *Checksums) Get(path string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	sum, ok := c.data[path]
	return sum, ok
}

// GetAll returns all checksums
func (c *Checksums) GetAll() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent race conditions
	result := make(map[string]string, len(c.data))
	for k, v := range c.data {
		result[k] = v
	}
	return result
}

// Set sets the checksum for a path
func (c *Checksums) Set(path, checksum string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[path] = checksum
}

// Delete removes a path from checksums
func (c *Checksums) Delete(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, path)
	// Also remove any nested paths if this was a directory
	for p := range c.data {
		if stringsHasPrefix(p, path+"/") {
			delete(c.data, p)
		}
	}
}

// Rename updates checksums when a file/folder is renamed
func (c *Checksums) Rename(from, to string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Direct file/folder rename
	if checksum, ok := c.data[from]; ok {
		c.data[to] = checksum
		delete(c.data, from)
	}

	// Rename nested paths
	for path, checksum := range c.data {
		if stringsHasPrefix(path, from+"/") {
			newPath := to + path[len(from):]
			c.data[newPath] = checksum
			delete(c.data, path)
		}
	}
}

// Scan walks the vault and updates checksums for all files
func (c *Checksums) Scan() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	newData := make(map[string]string)

	err := filepath.Walk(c.vaultPath, func(fullPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			// Check if directory should be ignored
			relPath, err := filepath.Rel(c.vaultPath, fullPath)
			if err == nil && c.matcher != nil {
				if c.matcher.MatchDir(filepath.ToSlash(relPath)) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(c.vaultPath, fullPath)
		if err != nil {
			return nil
		}

		// Normalize path separators
		relPath = filepath.ToSlash(relPath)

		// Check if file should be ignored
		if c.matcher != nil && c.matcher.Match(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Calculate checksum
		checksum, err := calculateFileChecksum(fullPath)
		if err != nil {
			return nil // Skip files that can't be read
		}

		newData[relPath] = checksum
		return nil
	})

	if err != nil {
		return err
	}

	c.data = newData
	return c.Save()
}

// GetFileCount returns the number of tracked files
func (c *Checksums) GetFileCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.data)
}

// UpdateForFile calculates and stores the checksum for a specific file
func (c *Checksums) UpdateForFile(relPath string) error {
	fullPath := filepath.Join(c.vaultPath, relPath)

	checksum, err := calculateFileChecksum(fullPath)
	if err != nil {
		return err
	}

	c.Set(relPath, checksum)
	return c.Save()
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
