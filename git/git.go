package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Manager handles git operations
type Manager struct {
	repoURL    string
	accessToken string
	vaultPath  string
	commitMsg  string
	interval   time.Duration
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewManager creates a new git manager
func NewManager(repoURL, accessToken, vaultPath, commitMsg string, intervalMinutes int) *Manager {
	if repoURL == "" {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		repoURL:    repoURL,
		accessToken: accessToken,
		vaultPath:  vaultPath,
		commitMsg:  commitMsg,
		interval:   time.Duration(intervalMinutes) * time.Minute,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Init initializes the git repository if it doesn't exist
func (m *Manager) Init() error {
	if m == nil {
		return nil
	}

	_, err := git.PlainOpen(m.vaultPath)
	if err == nil {
		// Repository already exists
		return nil
	}

	if err != git.ErrRepositoryNotExists {
		return err
	}

	// Clone the repository
	auth := &http.BasicAuth{
		Username: "oauth2", // GitHub uses "oauth2" as username for token auth
		Password: m.accessToken,
	}

	_, err = git.PlainClone(m.vaultPath, false, &git.CloneOptions{
		URL:  m.repoURL,
		Auth: auth,
	})

	return err
}

// Commit creates a git commit with all changes
func (m *Manager) Commit() error {
	if m == nil {
		return nil
	}

	repo, err := git.PlainOpen(m.vaultPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Add all changes
	_, err = worktree.Add(".")
	if err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}

	// Check if there are changes to commit
	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	if status.IsClean() {
		return nil // Nothing to commit
	}

	// Create commit
	_, err = worktree.Commit(m.commitMsg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "SmartSyncServer",
			Email: "smartsyncserver@local",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	// Push changes
	return m.Push()
}

// Push pushes commits to the remote repository
func (m *Manager) Push() error {
	if m == nil {
		return nil
	}

	repo, err := git.PlainOpen(m.vaultPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	auth := &http.BasicAuth{
		Username: "oauth2",
		Password: m.accessToken,
	}

	err = repo.Push(&git.PushOptions{
		Auth: auth,
	})

	if err == git.ErrNonFastForwardUpdate {
		// Pull first, then push
		if err := m.Pull(); err != nil {
			return err
		}
		return repo.Push(&git.PushOptions{Auth: auth})
	}

	return err
}

// Pull pulls changes from the remote repository
func (m *Manager) Pull() error {
	if m == nil {
		return nil
	}

	repo, err := git.PlainOpen(m.vaultPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	auth := &http.BasicAuth{
		Username: "oauth2",
		Password: m.accessToken,
	}

	err = worktree.Pull(&git.PullOptions{
		Auth:    auth,
		Progress: nil,
		Force:    true,
	})

	// Handle case where there's nothing to pull
	if err == git.NoErrAlreadyUpToDate {
		return nil
	}

	return err
}

// StartAutoCommit starts the background auto-commit routine
func (m *Manager) StartAutoCommit() {
	if m == nil || m.interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				_ = m.Commit()
			case <-m.ctx.Done():
				return
			}
		}
	}()
}

// Stop stops the auto-commit routine
func (m *Manager) Stop() {
	if m != nil {
		m.cancel()
	}
}

// EnsureIgnore ensures that the .gitignore file exists with vault-specific ignores
func (m *Manager) EnsureIgnore(vaultPath string) {
	if m == nil {
		return
	}

	gitignorePath := filepath.Join(vaultPath, ".gitignore")

	// Default gitignore for vault
	defaultIgnores := []string{
		"checksums.json",
		"config.yaml",
		".git/",
	}

	// Read existing gitignore
	var existing []string
	if data, err := os.ReadFile(gitignorePath); err == nil {
		lines := string(data)
		for _, line := range splitLines(lines) {
			line = trimSpace(line)
			if line != "" && !stringSliceContains(existing, line) {
				existing = append(existing, line)
			}
		}
	}

	// Add missing ignores
	changed := false
	for _, ignore := range defaultIgnores {
		if !stringSliceContains(existing, ignore) {
			existing = append(existing, ignore)
			changed = true
		}
	}

	if changed {
		_ = os.WriteFile(gitignorePath, joinLines(existing), 0644)
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func joinLines(lines []string) []byte {
	if len(lines) == 0 {
		return nil
	}
	result := make([]byte, 0, len(lines)+len(lines)-1)
	for i, line := range lines {
		if i > 0 {
			result = append(result, '\n')
		}
		result = append(result, line...)
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

func stringSliceContains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
