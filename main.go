package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chanchal1987/pigment-patch-generator/internal/arturia"
	"github.com/chanchal1987/pigment-patch-generator/internal/audio"
	"github.com/chanchal1987/pigment-patch-generator/internal/gemini"
)

//go:embed web/index.html
var indexHTML []byte

var (
	uploadDir = filepath.Join(os.TempDir(), "pigment-patch-generator", "uploads")
	presetDir = filepath.Join(os.TempDir(), "pigment-patch-generator", "presets")
)

const port = ":8080"

func main() {
	// Initialize directories
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Fatalf("failed to create upload directory: %v", err)
	}
	if err := os.MkdirAll(presetDir, 0755); err != nil {
		log.Fatalf("failed to create presets directory: %v", err)
	}

	mux := http.NewServeMux()

	// Serve Static Files for Frontend (Self-contained embedded index.html)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(indexHTML)
	})

	// API Handlers
	mux.HandleFunc("POST /api/upload", handleUpload)
	mux.HandleFunc("GET /api/download/{id}", handleDownloadPGTX)
	mux.HandleFunc("GET /api/download-md/{id}", handleDownloadMD)
	mux.HandleFunc("GET /api/models", handleListModels)

	// Add CORS and Logging Middleware
	wrappedHandler := middlewareLogging(middlewareCORS(mux))

	log.Printf("Server starting on http://localhost%s", port)
	if err := http.ListenAndServe(port, wrappedHandler); err != nil {
		log.Fatalf("server failed to start: %v", err)
	}
}

// Response holds the API response for client UI
type UploadResponse struct {
	ID                   string                   `json:"id"`
	PatchName            string                   `json:"patch_name"`
	Osc1Waveform         string                   `json:"osc1_waveform"`
	Filter1Cutoff        float64                  `json:"filter1_cutoff"`
	Filter1Resonance     float64                  `json:"filter1_resonance"`
	Env1Attack           float64                  `json:"env1_attack"`
	Env1Decay            float64                  `json:"env1_decay"`
	Env1Sustain          float64                  `json:"env1_sustain"`
	Env1Release          float64                  `json:"env1_release"`
	AdditionalParameters []gemini.AdditionalParam `json:"additional_parameters"`
	Markdown             string                   `json:"markdown"`
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	// 1. Limit input size to 10MB + buffer
	r.Body = http.MaxBytesReader(w, r.Body, audio.MaxFileSize+512*1024)

	err := r.ParseMultipartForm(audio.MaxFileSize)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to parse form or file exceeds 10MB: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "missing 'audio' file parameter", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 2. Validate and Save File locally
	uuid, savePath, err := audio.ValidateAndSave(file, header, uploadDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Defer cleanup of the temporary uploaded file
	defer os.Remove(savePath)

	// Detect actual mime type for upload
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "application/octet-stream" || mimeType == "" {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext == ".wav" {
			mimeType = "audio/wav"
		} else {
			mimeType = "audio/mpeg"
		}
	}

	// Read model name from form
	modelName := r.FormValue("model")
	if modelName == "" {
		modelName = "gemini-3.5-flash"
	}

	// 3. Perform Gemini API Analysis
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	result, err := gemini.AnalyzeAudio(ctx, savePath, mimeType, modelName)
	if err != nil {
		log.Printf("Gemini analysis error: %v", err)
		http.Error(w, fmt.Sprintf("AI Analysis failed: %v", err), http.StatusInternalServerError)
		return
	}

	// 4. Generate Arturia Boost & ZIP Preset (.pgtx)
	pgtxData, err := arturia.GeneratePGTX(result)
	if err != nil {
		log.Printf("PGTX generation error: %v", err)
		http.Error(w, "failed to generate preset PGTX", http.StatusInternalServerError)
		return
	}

	pgtxPath := filepath.Join(presetDir, uuid+".pgtx")
	if err := arturia.SavePGTX(pgtxData, pgtxPath); err != nil {
		log.Printf("PGTX creation error: %v", err)
		http.Error(w, "failed to package PGTX preset", http.StatusInternalServerError)
		return
	}

	// 5. Generate Markdown Guide
	mdContent := arturia.GenerateMarkdown(result)
	mdPath := filepath.Join(presetDir, uuid+".md")
	if err := arturia.SaveMarkdown(mdContent, mdPath); err != nil {
		log.Printf("Markdown save error: %v", err)
		// Non-blocking, we can still return PGTX
	}

	// 6. Return response to UI
	resp := UploadResponse{
		ID:                   uuid,
		PatchName:            result.PatchName,
		Osc1Waveform:         result.Osc1Waveform,
		Filter1Cutoff:        result.Filter1Cutoff,
		Filter1Resonance:     result.Filter1Resonance,
		Env1Attack:           result.Env1Attack,
		Env1Decay:            result.Env1Decay,
		Env1Sustain:          result.Env1Sustain,
		Env1Release:          result.Env1Release,
		AdditionalParameters: result.AdditionalParameters,
		Markdown:             mdContent,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func handleDownloadPGTX(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing preset id", http.StatusBadRequest)
		return
	}

	filePath := filepath.Clean(filepath.Join(presetDir, id+".pgtx"))
	// Prevent path traversal
	if filepath.Dir(filePath) != filepath.Clean(presetDir) {
		http.Error(w, "forbidden access", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "preset not found", http.StatusNotFound)
		return
	}

	presetName := getPresetName(filePath)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pgtx"`, presetName))
	http.ServeFile(w, r, filePath)
}

func handleDownloadMD(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing preset id", http.StatusBadRequest)
		return
	}

	filePath := filepath.Clean(filepath.Join(presetDir, id+".md"))
	// Prevent path traversal
	if filepath.Dir(filePath) != filepath.Clean(presetDir) {
		http.Error(w, "forbidden access", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "guide not found", http.StatusNotFound)
		return
	}

	xmlPath := filepath.Clean(filepath.Join(presetDir, id+".pgtx"))
	presetName := getPresetName(xmlPath)
	w.Header().Set("Content-Type", "text/markdown")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.md"`, presetName))
	http.ServeFile(w, r, filePath)
}

func handleListModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	models, err := gemini.ListModels(ctx)
	if err != nil {
		log.Printf("failed to list models dynamically, using fallback: %v", err)
		fallback := []gemini.ModelInfo{
			{Name: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash"},
			{Name: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash"},
			{Name: "gemini-1.5-pro", DisplayName: "Gemini 1.5 Pro"},
			{Name: "gemini-1.5-flash", DisplayName: "Gemini 1.5 Flash"},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(fallback)
		return
	}

	// If the list is empty (can happen in some sandbox environments), return fallback
	if len(models) == 0 {
		models = []gemini.ModelInfo{
			{Name: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash"},
			{Name: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash"},
			{Name: "gemini-1.5-pro", DisplayName: "Gemini 1.5 Pro"},
			{Name: "gemini-1.5-flash", DisplayName: "Gemini 1.5 Flash"},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(models)
}

func getPresetName(filePath string) string {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "AI_Reconstructed_Patch"
	}
	defer r.Close()

	presetName := "AI_Reconstructed_Patch"
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "Pigments/User/") {
			parts := strings.Split(f.Name, "/")
			if len(parts) >= 4 {
				presetName = parts[len(parts)-1]
				break
			}
		}
	}

	// Clean filename (replace spaces/specials with underscores)
	var clean []rune
	for _, r := range presetName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			clean = append(clean, r)
		} else if r == ' ' || r == '-' || r == '_' {
			clean = append(clean, '_')
		}
	}
	res := string(clean)
	// Collapse multiple underscores
	for strings.Contains(res, "__") {
		res = strings.ReplaceAll(res, "__", "_")
	}
	return strings.Trim(res, "_")
}


func middlewareCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func middlewareLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("Started %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("Completed %s %s in %v", r.Method, r.URL.Path, time.Since(start))
	})
}
