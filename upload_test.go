package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/gorilla/mux"
)

// Test getContentType function
func TestGetContentType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"video.mp4", "video/mp4"},
		{"movie.mov", "video/quicktime"},
		{"clip.avi", "video/x-msvideo"},
		{"web.webm", "video/webm"},
		{"document.pdf", "application/pdf"},
		{"photo.jpg", "image/jpeg"},
		{"picture.jpeg", "image/jpeg"},
		{"image.png", "image/png"},
		{"animation.gif", "image/gif"},
		{"readme.txt", "text/plain"},
		{"page.html", "text/html"},
		{"data.json", "application/json"},
		{"song.mp3", "audio/mpeg"},
		{"sound.wav", "audio/wav"},
		{"unknown.xyz", "application/octet-stream"},
		{"noextension", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := getContentType(tt.filename)
			if result != tt.want {
				t.Errorf("getContentType(%q) = %q, want %q", tt.filename, result, tt.want)
			}
		})
	}
}

// Test scheme function
func TestScheme(t *testing.T) {
	tests := []struct {
		name string
		tls  *tls.ConnectionState
		want string
	}{
		{
			name: "HTTP request",
			tls:  nil,
			want: "http",
		},
		{
			name: "HTTPS request",
			tls:  &tls.ConnectionState{},
			want: "https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				TLS: tt.tls,
			}
			result := scheme(req)
			if result != tt.want {
				t.Errorf("scheme() = %s, want %s", result, tt.want)
			}
		})
	}
}

// Test generateQRCodeBase64 function
func TestGenerateQRCodeBase64(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid URL",
			url:     "https://example.com/file/123",
			wantErr: false,
		},
		{
			name:    "simple URL",
			url:     "http://localhost:3015/download/test",
			wantErr: false,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true, // QR library does not accept empty strings
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generateQRCodeBase64(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateQRCodeBase64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				// Verify it's valid base64
				_, decodeErr := base64.StdEncoding.DecodeString(result)
				if decodeErr != nil {
					t.Errorf("generateQRCodeBase64() did not return valid base64: %v", decodeErr)
				}
				// Verify it's not empty
				if len(result) == 0 {
					t.Error("generateQRCodeBase64() returned empty string")
				}
			}
		})
	}
}

// Test buildFileEntries function
func TestBuildFileEntries(t *testing.T) {
	tests := []struct {
		name      string
		filesMap  map[string]FileInfo
		wantCount int
	}{
		{
			name:      "empty map",
			filesMap:  map[string]FileInfo{},
			wantCount: 0,
		},
		{
			name: "single file",
			filesMap: map[string]FileInfo{
				"file1": {
					ID:         "file1",
					Name:       "test.txt",
					StoredName: "123-test.txt",
				},
			},
			wantCount: 1,
		},
		{
			name: "multiple files",
			filesMap: map[string]FileInfo{
				"file1": {ID: "file1", Name: "test1.txt", StoredName: "123-test1.txt"},
				"file2": {ID: "file2", Name: "test2.png", StoredName: "456-test2.png"},
				"file3": {ID: "file3", Name: "doc.pdf", StoredName: "789-doc.pdf"},
			},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := buildFileEntries(tt.filesMap)
			if len(results) != tt.wantCount {
				t.Errorf("buildFileEntries() returned %d entries, want %d", len(results), tt.wantCount)
			}

			// Verify each entry has correct data
			for _, entry := range results {
				original, exists := tt.filesMap[entry.ID]
				if !exists {
					t.Errorf("Entry ID %s not found in original map", entry.ID)
					continue
				}
				if entry.Name != original.Name {
					t.Errorf("Entry name = %s, want %s", entry.Name, original.Name)
				}
			}
		})
	}
}

// Test uploadFileHandler
func TestUploadFileHandler(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	files = make(map[string]FileInfo)

	// Create a temporary directory for uploads
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	// Create multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "testfile.txt")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	part.Write([]byte("test file content"))
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	w := httptest.NewRecorder()
	uploadFileHandler(w, req)

	// Check redirect status
	if w.Code != http.StatusSeeOther {
		t.Errorf("uploadFileHandler() status = %d, want %d", w.Code, http.StatusSeeOther)
	}

	// Check that file was added to files map
	if len(files) != 1 {
		t.Errorf("uploadFileHandler() created %d files, want 1", len(files))
	}

	// Verify file exists on disk
	uploadedFiles, _ := os.ReadDir("uploads")
	if len(uploadedFiles) != 1 {
		t.Errorf("Found %d files in uploads directory, want 1", len(uploadedFiles))
	}
}

// Test uploadFileHandler with wrong method
func TestUploadFileHandler_WrongMethod(t *testing.T) {
	req := httptest.NewRequest("GET", "/upload", nil)
	w := httptest.NewRecorder()

	uploadFileHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("uploadFileHandler() with GET status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// Test uploadFileHandler with no file
func TestUploadFileHandler_NoFile(t *testing.T) {
	req := httptest.NewRequest("POST", "/upload", nil)
	req.Header.Set("Content-Type", "multipart/form-data")

	w := httptest.NewRecorder()
	uploadFileHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("uploadFileHandler() with no file status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// Test downloadFileHandler
func TestDownloadFileHandler(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	os.MkdirAll("uploads", 0755)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	testContent := "test file content for download"
	testFileName := "test-download.txt"
	os.WriteFile(filepath.Join("uploads", testFileName), []byte(testContent), 0644)

	// Test with file in map (has original filename)
	files = map[string]FileInfo{
		testFileName: {
			ID:         testFileName,
			Name:       "original.txt",
			StoredName: testFileName,
		},
	}

	req := httptest.NewRequest("GET", "/download/"+testFileName, nil)
	req = mux.SetURLVars(req, map[string]string{"id": testFileName})
	w := httptest.NewRecorder()

	downloadFileHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("downloadFileHandler() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Check content type header (should be proper MIME type now)
	contentType := w.Header().Get("Content-Type")
	if contentType != "text/plain" {
		t.Errorf("Content-Type = %s, want text/plain", contentType)
	}

	// Check content disposition header contains "attachment"
	contentDisposition := w.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisposition, "attachment") {
		t.Errorf("Content-Disposition = %s, should contain 'attachment'", contentDisposition)
	}
	if !strings.Contains(contentDisposition, "original.txt") {
		t.Errorf("Content-Disposition = %s, should contain original.txt", contentDisposition)
	}

	// Check file content
	body := w.Body.String()
	if body != testContent {
		t.Errorf("Response body = %s, want %s", body, testContent)
	}
}

// Test downloadFileHandler with file not in map (direct from filesystem)
func TestDownloadFileHandler_DirectFromFilesystem(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	os.MkdirAll("uploads", 0755)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	testContent := "direct file content"
	testFileName := "direct-file.txt"
	os.WriteFile(filepath.Join("uploads", testFileName), []byte(testContent), 0644)

	// Empty files map - file not tracked
	files = make(map[string]FileInfo)

	req := httptest.NewRequest("GET", "/download/"+testFileName, nil)
	req = mux.SetURLVars(req, map[string]string{"id": testFileName})
	w := httptest.NewRecorder()

	downloadFileHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("downloadFileHandler() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Should use the stored filename since not in map
	contentDisposition := w.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisposition, testFileName) {
		t.Errorf("Content-Disposition = %s, should contain %s", contentDisposition, testFileName)
	}

	// Check file content
	body := w.Body.String()
	if body != testContent {
		t.Errorf("Response body = %s, want %s", body, testContent)
	}
}

// Test downloadFileHandler with non-existent file
func TestDownloadFileHandler_NotFound(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	// Create temp directory but no file
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	os.MkdirAll("uploads", 0755)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	files = make(map[string]FileInfo)

	req := httptest.NewRequest("GET", "/download/nonexistent.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "nonexistent.txt"})
	w := httptest.NewRecorder()

	downloadFileHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("downloadFileHandler() status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// Test file type detection functions
func TestFileTypeDetection(t *testing.T) {
	tests := []struct {
		filename string
		isVideo  bool
		isAudio  bool
		isImage  bool
		isPDF    bool
		isText   bool
	}{
		{"video.mp4", true, false, false, false, false},
		{"movie.mov", true, false, false, false, false},
		{"song.mp3", false, true, false, false, false},
		{"sound.wav", false, true, false, false, false},
		{"photo.jpg", false, false, true, false, false},
		{"image.png", false, false, true, false, false},
		{"doc.pdf", false, false, false, true, false},
		{"readme.txt", false, false, false, false, true},
		{"data.json", false, false, false, false, true},
		{"unknown.xyz", false, false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := isVideoFile(tt.filename); got != tt.isVideo {
				t.Errorf("isVideoFile(%q) = %v, want %v", tt.filename, got, tt.isVideo)
			}
			if got := isAudioFile(tt.filename); got != tt.isAudio {
				t.Errorf("isAudioFile(%q) = %v, want %v", tt.filename, got, tt.isAudio)
			}
			if got := isImageFile(tt.filename); got != tt.isImage {
				t.Errorf("isImageFile(%q) = %v, want %v", tt.filename, got, tt.isImage)
			}
			if got := isPDFFile(tt.filename); got != tt.isPDF {
				t.Errorf("isPDFFile(%q) = %v, want %v", tt.filename, got, tt.isPDF)
			}
			if got := isTextFile(tt.filename); got != tt.isText {
				t.Errorf("isTextFile(%q) = %v, want %v", tt.filename, got, tt.isText)
			}
		})
	}
}

// Test streamFileHandler
func TestStreamFileHandler(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	os.MkdirAll("uploads", 0755)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	testContent := "test video content"
	testFileName := "test.mp4"
	os.WriteFile(filepath.Join("uploads", testFileName), []byte(testContent), 0644)

	files = map[string]FileInfo{
		testFileName: {
			ID:         testFileName,
			Name:       "original-video.mp4",
			StoredName: testFileName,
		},
	}

	req := httptest.NewRequest("GET", "/stream/"+testFileName, nil)
	req = mux.SetURLVars(req, map[string]string{"id": testFileName})
	w := httptest.NewRecorder()

	streamFileHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("streamFileHandler() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Check content type header
	contentType := w.Header().Get("Content-Type")
	if contentType != "video/mp4" {
		t.Errorf("Content-Type = %s, want video/mp4", contentType)
	}

	// Check content disposition header contains "inline"
	contentDisposition := w.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisposition, "inline") {
		t.Errorf("Content-Disposition = %s, should contain 'inline'", contentDisposition)
	}

	// Check file content
	body := w.Body.String()
	if body != testContent {
		t.Errorf("Response body = %s, want %s", body, testContent)
	}
}

// Test viewFileHandler (now renders HTML template)
func TestViewFileHandler(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	os.MkdirAll("uploads", 0755)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	testContent := "test video content"
	testFileName := "test.mp4"
	os.WriteFile(filepath.Join("uploads", testFileName), []byte(testContent), 0644)

	// Initialize template
	if tmplView == nil {
		tmplView = template.Must(template.New("view").Parse(`{{.FileName}} - Video={{.IsVideo}} StreamURL={{.StreamURL}}`))
	}

	files = map[string]FileInfo{
		testFileName: {
			ID:         testFileName,
			Name:       "original-video.mp4",
			StoredName: testFileName,
		},
	}

	req := httptest.NewRequest("GET", "/view/"+testFileName, nil)
	req = mux.SetURLVars(req, map[string]string{"id": testFileName})
	w := httptest.NewRecorder()

	viewFileHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("viewFileHandler() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Check that response contains HTML with video indicator
	body := w.Body.String()
	if !strings.Contains(body, "Video=true") {
		t.Errorf("Response should indicate IsVideo=true, got: %s", body)
	}
	if !strings.Contains(body, "StreamURL=/stream/") {
		t.Errorf("Response should contain StreamURL, got: %s", body)
	}
}

// Test displayFileHandler
func TestDisplayFileHandler(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	os.MkdirAll("uploads", 0755)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	// Create a test file
	testFileName := "testfile.txt"
	os.WriteFile(filepath.Join("uploads", testFileName), []byte("test content"), 0644)

	// Initialize template
	if tmplDisplayFile == nil {
		tmplDisplayFile = template.Must(template.New("display_file").Parse(`{{.FileName}}: ViewURL={{.ViewURL}} DownloadURL={{.DownloadURL}}`))
	}

	files = map[string]FileInfo{
		testFileName: {
			ID:         testFileName,
			Name:       "example.txt",
			StoredName: testFileName,
		},
	}

	req := httptest.NewRequest("GET", "/file/"+testFileName, nil)
	req.Host = "localhost:3015"
	req = mux.SetURLVars(req, map[string]string{"id": testFileName})
	w := httptest.NewRecorder()

	displayFileHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("displayFileHandler() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify response contains file name and both URLs
	body := w.Body.String()
	if !strings.Contains(body, "example.txt") {
		t.Errorf("Response should contain filename example.txt, got: %s", body)
	}
	if !strings.Contains(body, "ViewURL=/view/") {
		t.Errorf("Response should contain ViewURL, got: %s", body)
	}
	if !strings.Contains(body, "DownloadURL=/download/") {
		t.Errorf("Response should contain DownloadURL, got: %s", body)
	}
}

// Test displayFileHandler with file not in map (direct from filesystem)
func TestDisplayFileHandler_DirectFromFilesystem(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	// Create a temporary directory and file
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	os.MkdirAll("uploads", 0755)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	// Create a test file
	testFileName := "direct-file.txt"
	os.WriteFile(filepath.Join("uploads", testFileName), []byte("test content"), 0644)

	// Initialize template
	if tmplDisplayFile == nil {
		tmplDisplayFile = template.Must(template.New("display_file").Parse(`{{.FileName}}: ViewURL={{.ViewURL}} DownloadURL={{.DownloadURL}}`))
	}

	// Empty files map - file not tracked
	files = make(map[string]FileInfo)

	req := httptest.NewRequest("GET", "/file/"+testFileName, nil)
	req.Host = "localhost:3015"
	req = mux.SetURLVars(req, map[string]string{"id": testFileName})
	w := httptest.NewRecorder()

	displayFileHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("displayFileHandler() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Should use the stored filename since not in map
	body := w.Body.String()
	if !strings.Contains(body, testFileName) {
		t.Errorf("Response should contain filename %s, got: %s", testFileName, body)
	}
}

// Test displayFileHandler with non-existent file
func TestDisplayFileHandler_NotFound(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	// Create temp directory but no file
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	os.MkdirAll("uploads", 0755)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	files = make(map[string]FileInfo)

	req := httptest.NewRequest("GET", "/file/nonexistent.txt", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "nonexistent.txt"})
	w := httptest.NewRecorder()

	displayFileHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("displayFileHandler() status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// Test file upload and download integration
func TestFileUploadDownloadIntegration(t *testing.T) {
	originalFiles := files
	t.Cleanup(func() {
		files = originalFiles
	})

	files = make(map[string]FileInfo)

	// Setup temp directory
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	testContent := "integration test file content"

	// Step 1: Upload file
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "integration.txt")
	io.WriteString(part, testContent)
	writer.Close()

	uploadReq := httptest.NewRequest("POST", "/upload", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadW := httptest.NewRecorder()

	uploadFileHandler(uploadW, uploadReq)

	if uploadW.Code != http.StatusSeeOther {
		t.Fatalf("Upload failed with status %d", uploadW.Code)
	}

	// Step 2: Download the file
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(files))
	}

	var fileID string
	for id := range files {
		fileID = id
		break
	}

	downloadReq := httptest.NewRequest("GET", "/download/"+fileID, nil)
	downloadReq = mux.SetURLVars(downloadReq, map[string]string{"id": fileID})
	downloadW := httptest.NewRecorder()

	downloadFileHandler(downloadW, downloadReq)

	if downloadW.Code != http.StatusOK {
		t.Errorf("Download failed with status %d", downloadW.Code)
	}

	// Verify downloaded content matches uploaded content
	downloadedContent := downloadW.Body.String()
	if downloadedContent != testContent {
		t.Errorf("Downloaded content = %s, want %s", downloadedContent, testContent)
	}
}
