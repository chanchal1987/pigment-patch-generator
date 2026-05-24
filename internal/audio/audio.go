package audio

import (
	"crypto/rand"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

const MaxFileSize = 10 * 1024 * 1024 // 10MB

// ValidMimeTypes defines the allowed audio formats
var ValidMimeTypes = map[string]bool{
	"audio/wav":   true,
	"audio/wave":  true,
	"audio/x-wav": true,
	"audio/mpeg":  true,
	"audio/mp3":   true,
}

// GenerateUUID generates a basic UUID v4 representation using crypto/rand
func GenerateUUID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	// Set version 4 (random) and variant bits
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// ValidateAndSave validates size, mime-type, and saves the file to the target directory
func ValidateAndSave(file multipart.File, header *multipart.FileHeader, destDir string) (string, string, error) {
	// 1. Check file size
	if header.Size > MaxFileSize {
		return "", "", fmt.Errorf("file size %d exceeds limit of 10MB", header.Size)
	}

	// 2. Validate MIME Type
	// We check the header's Content-Type, and also verify standard file extensions as fallback
	mimeType := strings.ToLower(header.Header.Get("Content-Type"))
	
	// If Content-Type is generic (e.g. application/octet-stream), check extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if mimeType == "application/octet-stream" || mimeType == "" {
		switch ext {
		case ".wav":
			mimeType = "audio/wav"
		case ".mp3":
			mimeType = "audio/mpeg"
		}
	}

	if !ValidMimeTypes[mimeType] {
		return "", "", fmt.Errorf("unsupported file type: %s (only .wav and .mp3 allowed)", mimeType)
	}

	// Ensure the destination directory exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create destination directory: %w", err)
	}

	// 3. Generate unique UUID filename
	uuid, err := GenerateUUID()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate file ID: %w", err)
	}

	savePath := filepath.Join(destDir, uuid+ext)
	outFile, err := os.Create(savePath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create destination file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, file); err != nil {
		os.Remove(savePath) // Cleanup on failure
		return "", "", fmt.Errorf("failed to write file content: %w", err)
	}

	return uuid, savePath, nil
}
