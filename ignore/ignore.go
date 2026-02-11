package ignore

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type Matcher struct {
	patterns []string
}

// New creates a new ignore matcher with the given glob patterns
func New(patterns []string) *Matcher {
	return &Matcher{
		patterns: patterns,
	}
}

// Match returns true if the given path matches any ignore pattern
// Path should be relative to vault root and use forward slashes
func (m *Matcher) Match(path string) bool {
	// Normalize path to use forward slashes
	normalized := filepath.ToSlash(path)
	normalized = strings.TrimPrefix(normalized, "/")

	for _, pattern := range m.patterns {
		matched, err := doublestar.Match(pattern, normalized)
		if err == nil && matched {
			return true
		}
		// Try matching against any parent directory
		matched, err = doublestar.PathMatch(pattern, normalized)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// MatchDir returns true if any file in the given directory should be ignored
func (m *Matcher) MatchDir(dirPath string) bool {
	// Check if the directory itself matches
	if m.Match(dirPath) {
		return true
	}
	// Check if any pattern matches contents of this directory
	for _, pattern := range m.patterns {
		if strings.HasPrefix(pattern, dirPath+"/") || strings.HasPrefix(pattern, dirPath+"\\") {
			return true
		}
	}
	return false
}
