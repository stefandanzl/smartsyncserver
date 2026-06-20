package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Manager handles git operations
type Manager struct {
	repoURL       string
	accessToken   string
	vaultPath     string
	commitMsg     string
	interval      time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
	lastError     string
	lastErrorTime time.Time
}

// NewManager creates a new git manager
func NewManager(repoURL, accessToken, vaultPath, commitMsg string, intervalMinutes int) *Manager {
	if repoURL == "" {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		repoURL:     repoURL,
		accessToken: accessToken,
		vaultPath:   vaultPath,
		commitMsg:   commitMsg,
		interval:    time.Duration(intervalMinutes) * time.Minute,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// commitCounts holds per-category file counts for placeholder expansion.
// Renamed and Copied files are folded into Added; Deleted stands alone.
type commitCounts struct {
	total    int
	added    int
	modified int
	deleted  int
}

// expandCommitMessage expands placeholders in the configured commit message
// template using the commit time and the file counts. Supported placeholders:
//
//	{date_day}      -> "2026-06-20"            (date, server local time)
//	{date_time}     -> "2026-06-20 14:30"      (date + time, no seconds)
//	{date_daytime}  -> "14:30"                 (clock time, no seconds)
//	{date_epoch}    -> "1779000000"            (unix seconds)
//	{files_total}   -> "5"                     (total files in the commit)
//	{files_added}   -> "2"                     (added, incl. renamed/copied)
//	{files_modified}-> "2"                     (modified)
//	{files_deleted} -> "1"                     (deleted)
//
// Times use the server's local timezone (see TZ / time/tzdata).
func (m *Manager) expandCommitMessage(t time.Time, c commitCounts) string {
	msg := m.commitMsg
	msg = strings.ReplaceAll(msg, "{date_day}", t.Format("2006-01-02"))
	msg = strings.ReplaceAll(msg, "{date_daytime}", t.Format("2006-01-02 15:04"))
	msg = strings.ReplaceAll(msg, "{date_time}", t.Format("15:04"))
	msg = strings.ReplaceAll(msg, "{date_epoch}", strconv.FormatInt(t.Unix(), 10))
	msg = strings.ReplaceAll(msg, "{files_total}", strconv.Itoa(c.total))
	msg = strings.ReplaceAll(msg, "{files_added}", strconv.Itoa(c.added))
	msg = strings.ReplaceAll(msg, "{files_modified}", strconv.Itoa(c.modified))
	msg = strings.ReplaceAll(msg, "{files_deleted}", strconv.Itoa(c.deleted))
	return msg
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

// CommitResult contains details about a commit operation
type CommitResult struct {
	Success    bool     `json:"success"`
	HasChanges bool     `json:"has_changes"`
	Files      []string `json:"files_changed,omitempty"`
	CommitHash string   `json:"commit_hash,omitempty"`
	Pushed     bool     `json:"pushed"`
	PushError  string   `json:"push_error,omitempty"`
	Message    string   `json:"message"`
}

// Commit creates a git commit with all changes
func (m *Manager) Commit() *CommitResult {
	if m == nil {
		return &CommitResult{Success: false, Message: "Git not configured"}
	}

	repo, err := git.PlainOpen(m.vaultPath)
	if err != nil {
		m.lastError = fmt.Sprintf("failed to open repository: %v", err)
		m.lastErrorTime = time.Now()
		return &CommitResult{Success: false, Message: m.lastError}
	}

	worktree, err := repo.Worktree()
	if err != nil {
		m.lastError = fmt.Sprintf("failed to get worktree: %v", err)
		m.lastErrorTime = time.Now()
		return &CommitResult{Success: false, Message: m.lastError}
	}

	// Add all changes
	_, err = worktree.Add(".")
	if err != nil {
		m.lastError = fmt.Sprintf("failed to stage changes: %v", err)
		m.lastErrorTime = time.Now()
		return &CommitResult{Success: false, Message: m.lastError}
	}

	// Check if there are changes to commit
	status, err := worktree.Status()
	if err != nil {
		m.lastError = fmt.Sprintf("failed to get status: %v", err)
		m.lastErrorTime = time.Now()
		return &CommitResult{Success: false, Message: m.lastError}
	}

	if status.IsClean() {
		// Clear error on success
		m.lastError = ""
		return &CommitResult{Success: true, HasChanges: false, Message: "No changes to commit"}
	}

	// Collect changed files (status is a map[filePath]FileStatus)
	var files []string
	for path := range status {
		files = append(files, path)
	}

	// Tally per-category counts for placeholder expansion. Renamed and Copied
	// are folded into Added; Modified and Deleted stand alone.
	var counts commitCounts
	counts.total = len(files)
	for _, fs := range status {
		switch fs.Staging {
		case git.Added, git.Renamed, git.Copied:
			counts.added++
		case git.Modified:
			counts.modified++
		case git.Deleted:
			counts.deleted++
		}
	}

	// Create commit (placeholders in the message are expanded to the commit time)
	now := time.Now()
	commit, err := worktree.Commit(m.expandCommitMessage(now, counts), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "SmartSyncServer",
			Email: "smartsyncserver@local",
			When:  now,
		},
	})
	if err != nil {
		m.lastError = fmt.Sprintf("failed to commit: %v", err)
		m.lastErrorTime = time.Now()
		return &CommitResult{Success: false, Message: m.lastError}
	}

	// Push changes
	if err := m.Push(); err != nil {
		return &CommitResult{
			Success:    true,
			HasChanges: true,
			Files:      files,
			CommitHash: commit.String(),
			Pushed:     false,
			PushError:  err.Error(),
			Message:    "Committed locally but push failed",
		}
	}

	// Clear error on success
	m.lastError = ""
	return &CommitResult{
		Success:    true,
		HasChanges: true,
		Files:      files,
		CommitHash: commit.String(),
		Pushed:     true,
		Message:    "Changes committed and pushed",
	}
}

// PullResult contains details about a pull operation
type PullResult struct {
	Success    bool   `json:"success"`
	Updated    bool   `json:"updated"`
	UpToDate   bool   `json:"up_to_date"`
	Conflicted bool   `json:"conflicted"`
	Message    string `json:"message"`
	Error      string `json:"error,omitempty"`
}

// PullDetailed pulls changes and returns detailed result
func (m *Manager) PullDetailed() *PullResult {
	if m == nil {
		return &PullResult{Success: true, UpToDate: true, Message: "Git not configured"}
	}

	repo, err := git.PlainOpen(m.vaultPath)
	if err != nil {
		m.lastError = fmt.Sprintf("failed to open repository: %v", err)
		m.lastErrorTime = time.Now()
		return &PullResult{Success: false, Message: m.lastError}
	}

	worktree, err := repo.Worktree()
	if err != nil {
		m.lastError = fmt.Sprintf("failed to get worktree: %v", err)
		m.lastErrorTime = time.Now()
		return &PullResult{Success: false, Message: m.lastError}
	}

	auth := &http.BasicAuth{
		Username: "oauth2",
		Password: m.accessToken,
	}

	err = worktree.Pull(&git.PullOptions{
		Auth:     auth,
		Progress: nil,
		Force:    true,
	})

	// Handle case where there's nothing to pull
	if err == git.NoErrAlreadyUpToDate {
		// Clear error on success
		m.lastError = ""
		return &PullResult{Success: true, UpToDate: true, Message: "Already up to date"}
	}

	if err != nil {
		m.lastError = fmt.Sprintf("Pull failed: %v", err)
		m.lastErrorTime = time.Now()
		return &PullResult{Success: false, Message: m.lastError}
	}

	// Clear error on success
	m.lastError = ""
	return &PullResult{
		Success: true,
		Updated: true,
		Message: "Pull successful",
	}
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
		Auth:     auth,
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

// GetLastError returns the last git error (empty string if no error or last operation succeeded)
func (m *Manager) GetLastError() string {
	if m == nil {
		return ""
	}
	return m.lastError
}
