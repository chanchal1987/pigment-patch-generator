package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCORSHeaders(t *testing.T) {
	handler := middlewareCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/api/upload", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected OPTIONS response to be 200, got %d", rec.Code)
	}

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("missing CORS Origin header")
	}
}

func TestDownloadSecurity(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/download/{id}", handleDownloadPGTX)
	mux.HandleFunc("GET /api/download-md/{id}", handleDownloadMD)

	// Create a dummy test file in the presetDir
	_ = os.MkdirAll(presetDir, 0755)
	defer os.RemoveAll(presetDir)

	dummyFile := filepath.Join(presetDir, "test-id.pgtx")
	_ = os.WriteFile(dummyFile, []byte("fake zip"), 0644)

	dummyMdFile := filepath.Join(presetDir, "test-id.md")
	_ = os.WriteFile(dummyMdFile, []byte("# Manual Guide"), 0644)

	// Test successful downloads and not founds using mux routing
	routeTests := []struct {
		name       string
		url        string
		expectCode int
	}{
		{
			name:       "Download existing preset",
			url:        "/api/download/test-id",
			expectCode: http.StatusOK,
		},
		{
			name:       "Download existing guide",
			url:        "/api/download-md/test-id",
			expectCode: http.StatusOK,
		},
		{
			name:       "Preset not found",
			url:        "/api/download/nonexistent",
			expectCode: http.StatusNotFound,
		},
		{
			name:       "Guide not found",
			url:        "/api/download-md/nonexistent",
			expectCode: http.StatusNotFound,
		},
	}

	for _, tt := range routeTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.expectCode {
				t.Errorf("expected response code %d, got %d for %s", tt.expectCode, rec.Code, tt.url)
			}
		})
	}

	// Test path traversal protection directly on the handlers using SetPathValue
	traversalTests := []struct {
		name       string
		handler    http.HandlerFunc
		idVal      string
		expectCode int
	}{
		{
			name:       "Path traversal block PGTX",
			handler:    handleDownloadPGTX,
			idVal:      "../../etc/passwd",
			expectCode: http.StatusForbidden,
		},
		{
			name:       "Path traversal block MD",
			handler:    handleDownloadMD,
			idVal:      "../../etc/passwd",
			expectCode: http.StatusForbidden,
		},
	}

	for _, tt := range traversalTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/download/ignored", nil)
			req.SetPathValue("id", tt.idVal)
			rec := httptest.NewRecorder()
			tt.handler(rec, req)

			if rec.Code != tt.expectCode {
				t.Errorf("expected response code %d, got %d for id %s", tt.expectCode, rec.Code, tt.idVal)
			}
		})
	}
}

func TestUploadInvalidForm(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/upload", strings.NewReader("bad content"))
	req.Header.Set("Content-Type", "application/json") // Should trigger form parsing failure since it's not multipart

	rec := httptest.NewRecorder()
	handleUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected upload response to be 400 Bad Request, got %d", rec.Code)
	}
}
