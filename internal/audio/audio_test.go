package audio

import (
	"bytes"
	"mime/multipart"
	"net/textproto"
	"os"
	"testing"
)

func TestGenerateUUID(t *testing.T) {
	uuid1, err := GenerateUUID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	uuid2, err := GenerateUUID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if uuid1 == uuid2 {
		t.Errorf("expected unique UUIDs, got identical: %s", uuid1)
	}

	if len(uuid1) != 36 {
		t.Errorf("expected UUID length of 36, got %d", len(uuid1))
	}
}

// mockMultipartFile implements multipart.File interfaces for testing
type mockMultipartFile struct {
	*bytes.Reader
}

func (m *mockMultipartFile) Close() error { return nil }

func TestValidateAndSave(t *testing.T) {
	tempDir := "./tmp_test_audio"
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name       string
		fileData   []byte
		headerSize int64
		filename   string
		contentType string
		expectErr  bool
	}{
		{
			name:        "Valid WAV file",
			fileData:    []byte("riff....wavefmt "),
			headerSize:  16,
			filename:    "test.wav",
			contentType: "audio/wav",
			expectErr:   false,
		},
		{
			name:        "Valid MP3 file",
			fileData:    []byte("id3...mpeg"),
			headerSize:  10,
			filename:    "test.mp3",
			contentType: "audio/mpeg",
			expectErr:   false,
		},
		{
			name:        "Invalid file size",
			fileData:    []byte("too large"),
			headerSize:  MaxFileSize + 100,
			filename:    "huge.wav",
			contentType: "audio/wav",
			expectErr:   true,
		},
		{
			name:        "Invalid MIME type",
			fileData:    []byte("plain text"),
			headerSize:  10,
			filename:    "test.txt",
			contentType: "text/plain",
			expectErr:   true,
		},
		{
			name:        "Octet-stream fallback with wav extension",
			fileData:    []byte("some wav bytes"),
			headerSize:  14,
			filename:    "test.wav",
			contentType: "application/octet-stream",
			expectErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mFile := &mockMultipartFile{bytes.NewReader(tt.fileData)}
			mHeader := &multipart.FileHeader{
				Filename: tt.filename,
				Size:     tt.headerSize,
				Header:   make(textproto.MIMEHeader),
			}
			mHeader.Header.Set("Content-Type", tt.contentType)

			id, path, err := ValidateAndSave(mFile, mHeader, tempDir)
			if (err != nil) != tt.expectErr {
				t.Fatalf("expected error: %v, got: %v", tt.expectErr, err)
			}

			if !tt.expectErr {
				if id == "" {
					t.Error("expected generated id, got empty")
				}
				if _, err := os.Stat(path); os.IsNotExist(err) {
					t.Errorf("expected file to be saved to %s, but it does not exist", path)
				}
			}
		})
	}
}
