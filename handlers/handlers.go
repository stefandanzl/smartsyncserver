package handlers

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"smartsyncserver/auth"
	"smartsyncserver/git"
	"smartsyncserver/storage"
)

// Server wraps all dependencies for HTTP handlers
type Server struct {
	vault     *storage.Vault
	checksums  *storage.Checksums
	git       *git.Manager
	auth      *auth.Middleware
	scanConfig ScanConfig
}

type ScanConfig struct {
	API bool
}

// New creates a new server with handlers
func New(vault *storage.Vault, checksums *storage.Checksums, gitMgr *git.Manager, authMgr *auth.Middleware, scanCfg ScanConfig) *Server {
	return &Server{
		vault:     vault,
		checksums:  checksums,
		git:       gitMgr,
		auth:      authMgr,
		scanConfig: scanCfg,
	}
}

// RegisterRoutes registers all HTTP routes
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Apply auth middleware to all routes except /status
	handler := s.auth.Handler(http.HandlerFunc(s.handleRequest))
	mux.Handle("/", handler)
}

// handleRequest routes requests to appropriate handlers
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/status":
		s.handleStatus(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/file/"):
		s.handleGetFile(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/file/"):
		s.handlePutFile(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/file/"):
		s.handleDeleteFile(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/folder/"):
		s.handleCreateFolder(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/folder/"):
		s.handleDeleteFolder(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/rename":
		s.handleRename(w, r)
	default:
		s.writeError(w, "Not found", http.StatusNotFound)
	}
}

// handleStatus returns server status (no auth required)
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"online":     true,
		"file_count": s.checksums.GetFileCount(),
	}
	s.writeJSON(w, response, http.StatusOK)
}

// handleGetFile downloads a file
func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/file/")
	if path == "" {
		s.writeError(w, "File path required", http.StatusBadRequest)
		return
	}

	// Normalize path
	path = filepath.ToSlash(path)

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

	// Check if client accepts gzip
	acceptsGzip := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")

	if acceptsGzip {
		w.Header().Set("Content-Encoding", "gzip")
		gzWriter := gzip.NewWriter(w)
		defer gzWriter.Close()
		http.ServeFile(w, r, fullPath)
		return
	}

	// Stream file directly
	http.ServeFile(w, r, fullPath)
}

// handlePutFile uploads or overwrites a file
func (s *Server) handlePutFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/file/")
	if path == "" {
		s.writeError(w, "File path required", http.StatusBadRequest)
		return
	}

	// Normalize path
	path = filepath.ToSlash(path)

	// Read request body
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
			// Log but don't fail the request
		}
	}

	s.writeJSON(w, map[string]bool{"success": true}, http.StatusOK)
}

// handleDeleteFile deletes a file
func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/file/")
	if path == "" {
		s.writeError(w, "File path required", http.StatusBadRequest)
		return
	}

	// Normalize path
	path = filepath.ToSlash(path)

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

// handleCreateFolder creates a folder
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/folder/")
	if path == "" {
		s.writeError(w, "Folder path required", http.StatusBadRequest)
		return
	}

	// Normalize path
	path = filepath.ToSlash(path)

	if err := s.vault.CreateFolder(path); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]bool{"created": true}, http.StatusOK)
}

// handleDeleteFolder deletes a folder
func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/folder/")
	if path == "" {
		s.writeError(w, "Folder path required", http.StatusBadRequest)
		return
	}

	// Normalize path
	path = filepath.ToSlash(path)

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
