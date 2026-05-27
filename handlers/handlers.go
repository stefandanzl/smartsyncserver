package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"smartsyncserver/auth"
	"smartsyncserver/git"
	"smartsyncserver/storage"
)

// Server wraps all dependencies for HTTP handlers
type Server struct {
	vault      *storage.Vault
	checksums  *storage.Checksums
	git        *git.Manager
	auth       *auth.Middleware
	scanConfig ScanConfig
}

type ScanConfig struct {
	API bool
}

// New creates a new server with handlers
func New(vault *storage.Vault, checksums *storage.Checksums, gitMgr *git.Manager, authMgr *auth.Middleware, scanCfg ScanConfig) *Server {
	return &Server{
		vault:      vault,
		checksums:  checksums,
		git:        gitMgr,
		auth:       authMgr,
		scanConfig: scanCfg,
	}
}

// RegisterRoutes registers all HTTP routes
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Register specific routes (more specific routes first)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/checksums", s.handleChecksums)
	mux.HandleFunc("/snapshot", s.handleSnapshot)
	mux.HandleFunc("/git/pull", s.handleGitPull)
	mux.HandleFunc("/file/", s.handleFile)
	mux.HandleFunc("/folder/", s.handleFolder)
	mux.HandleFunc("/rename", s.handleRename)
	mux.HandleFunc("/empty/", s.handleEmpty)

	// Catch-all for auth middleware (applied to everything)
	handler := s.auth.Handler(mux)
	mux.Handle("/", handler)
}

// handleStatus returns server status (no auth required)
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"online":       true,
		"files_total":  s.checksums.GetFileCount(),
		"auth_success": !s.auth.IsEnabled() || s.auth.ValidateRequest(r),
		"git_error":    s.git != nil && s.git.GetLastError() != "",
	}
	s.writeJSON(w, response, http.StatusOK)
}

// handleChecksums handles checksum operations
func (s *Server) handleChecksums(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetChecksums(w, r)
	case http.MethodPut:
		s.handleScanChecksums(w, r)
	default:
		s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetChecksums returns all checksums, optionally filtered by mtime
func (s *Server) handleGetChecksums(w http.ResponseWriter, r *http.Request) {
	// Parse since parameter
	var sinceTime int64
	var hasFilter bool
	var filterError string

	sinceParam := r.URL.Query().Get("since")
	if sinceParam != "" {
		parsed, err := strconv.ParseInt(sinceParam, 10, 64)
		if err != nil || parsed < 0 {
			// Invalid filter - will return all files with error
			filterError = fmt.Sprintf("Invalid 'since' parameter: must be a non-negative integer, received '%s'", sinceParam)
		} else {
			sinceTime = parsed
			hasFilter = true
		}
	}

	// Get all checksums
	allChecksums := s.checksums.GetAll()
	totalCount := len(allChecksums)

	// Filter if since parameter is valid
	if hasFilter {
		filteredChecksums := make(map[string]storage.FileEntry)
		for path, entry := range allChecksums {
			if entry.Mtime >= sinceTime {
				filteredChecksums[path] = entry
			}
		}
		allChecksums = filteredChecksums
	}

	// Build response
	response := map[string]interface{}{}
	response["checksums"] = allChecksums

	// Add counts and filter info
	if hasFilter {
		response["files_total"] = totalCount
		response["filter"] = map[string]interface{}{
			"files_filtered": len(allChecksums),
			"mtime":          sinceTime,
		}
	} else if filterError != "" {
		// Invalid filter - show total count + error
		response["files_total"] = totalCount
		response["error"] = filterError
	} else {
		// No filter - keep backward compatible (simple number)
		response["files_total"] = totalCount
	}

	s.writeJSON(w, response, http.StatusOK)
}

// handleScanChecksums triggers a rescan of the vault
func (s *Server) handleScanChecksums(w http.ResponseWriter, r *http.Request) {
	if err := s.checksums.Scan(); err != nil {
		s.writeError(w, "Scan failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	allChecksums := s.checksums.GetAll()
	response := map[string]interface{}{
		"scanned":     true,
		"files_total": len(allChecksums),
		"checksums":   allChecksums,
	}
	s.writeJSON(w, response, http.StatusOK)
}

// handleSnapshot creates a git commit and pushes to remote
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result := s.git.Commit()
	if result.Success {
		s.writeJSON(w, result, http.StatusOK)
	} else {
		s.writeJSON(w, result, http.StatusInternalServerError)
	}
}

// handleGitPull pulls changes from remote repository
func (s *Server) handleGitPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result := s.git.PullDetailed()
	if result.Success {
		s.writeJSON(w, result, http.StatusOK)
	} else {
		s.writeJSON(w, result, http.StatusInternalServerError)
	}
}

// handleFile handles all file operations
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/file/")
	if path == "" {
		s.writeError(w, "File path required", http.StatusBadRequest)
		return
	}

	// URL-decode path
	path, err := url.PathUnescape(path)
	if err != nil {
		s.writeError(w, "Invalid path encoding", http.StatusBadRequest)
		return
	}

	// Normalize path
	path = filepath.ToSlash(path)

	switch r.Method {
	case http.MethodGet:
		s.handleGetFile(w, r, path)
	case http.MethodPut:
		s.handlePutFile(w, r, path)
	case http.MethodDelete:
		s.handleDeleteFile(w, r, path)
	default:
		s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetFile downloads a file
func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request, path string) {
	fullPath, err := s.vault.GetFile(path)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeError(w, "File not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// Detect content type
	mimeType := storage.MimeType(path)
	w.Header().Set("Content-Type", mimeType)

	// Stream file directly
	http.ServeFile(w, r, fullPath)
}

// handlePutFile uploads or overwrites a file
func (s *Server) handlePutFile(w http.ResponseWriter, r *http.Request, path string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Write file
	if err := s.vault.PutFile(path, body); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update checksums if API scan is enabled
	if s.scanConfig.API {
		if err := s.checksums.UpdateForFile(path); err != nil {
			// Log but don't fail request
		}
	}

	s.writeJSON(w, map[string]bool{"success": true}, http.StatusOK)
}

// handleDeleteFile deletes a file
func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request, path string) {
	// Delete file
	if err := s.vault.DeleteFile(path); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeError(w, "File not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Update checksums if API scan is enabled
	if s.scanConfig.API {
		s.checksums.Delete(path)
		_ = s.checksums.Save()
	}

	s.writeJSON(w, map[string]bool{"deleted": true}, http.StatusOK)
}

// handleFolder handles all folder operations
func (s *Server) handleFolder(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/folder/")
	if path == "" {
		s.writeError(w, "Folder path required", http.StatusBadRequest)
		return
	}

	// URL-decode path
	path, err := url.PathUnescape(path)
	if err != nil {
		s.writeError(w, "Invalid path encoding", http.StatusBadRequest)
		return
	}

	// Normalize path
	path = filepath.ToSlash(path)

	switch r.Method {
	case http.MethodPut:
		s.handleCreateFolder(w, r, path)
	case http.MethodDelete:
		s.handleDeleteFolder(w, r, path)
	default:
		s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreateFolder creates a folder
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request, path string) {
	if err := s.vault.CreateFolder(path); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]bool{"created": true}, http.StatusOK)
}

// handleDeleteFolder deletes a folder
func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request, path string) {
	if err := s.vault.DeleteFolder(path); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeError(w, "Folder not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Update checksums - remove all entries under this path
	s.checksums.Delete(path)
	_ = s.checksums.Save()

	s.writeJSON(w, map[string]bool{"deleted": true}, http.StatusOK)
}

// handleRename renames a file or folder
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type string `json:"type"`
		From string `json:"from"`
		To   string `json:"to"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate type
	if req.Type != "file" && req.Type != "folder" {
		s.writeError(w, "Type must be 'file' or 'folder'", http.StatusBadRequest)
		return
	}

	if req.From == "" || req.To == "" {
		s.writeError(w, "Both 'from' and 'to' paths are required", http.StatusBadRequest)
		return
	}

	// Normalize paths
	req.From = filepath.ToSlash(req.From)
	req.To = filepath.ToSlash(req.To)

	// Perform rename
	if err := s.vault.Rename(req.From, req.To); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeError(w, "Source not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Update checksums if API scan is enabled
	if s.scanConfig.API {
		s.checksums.Rename(req.From, req.To)
		_ = s.checksums.Save()
	}

	s.writeJSON(w, map[string]bool{"renamed": true}, http.StatusOK)
}

// handleEmpty handles listing and cleaning empty folders
func (s *Server) handleEmpty(w http.ResponseWriter, r *http.Request) {
	// Check if it's the clean endpoint (POST /empty/clean)
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/clean") {
		removed, err := s.vault.CleanEmptyFolders()
		if err != nil {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.writeJSON(w, map[string]any{"cleaned": removed, "count": removed}, http.StatusOK)
		return
	}

	// GET /empty - list empty folders
	if r.Method != http.MethodGet {
		s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	emptyFolders, err := s.vault.ListEmptyFolders()
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]any{"empty_folders": emptyFolders, "count": len(emptyFolders)}, http.StatusOK)
}

// writeJSON writes a JSON response
func (s *Server) writeJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes an error response
func (s *Server) writeError(w http.ResponseWriter, message string, status int) {
	s.writeJSON(w, map[string]string{"error": message}, status)
}
