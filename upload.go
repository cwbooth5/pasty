package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	qrcode "github.com/skip2/go-qrcode"
)

// FileInfo holds metadata about an uploaded file
type FileInfo struct {
	ID         string // e.g. "1674490732123456-MyPic.png"
	Name       string // original file name from user
	StoredName string // actual name used on disk
}

var files = make(map[string]FileInfo)

// buildFileEntries converts a files map to a list of FileEntry for display
func buildFileEntries(filesMap map[string]FileInfo) []FileEntry {
	var entries []FileEntry
	for id, info := range filesMap {
		entries = append(entries, FileEntry{
			ID:   id,
			Name: info.Name,
		})
	}
	return entries
}

// getContentType returns the MIME type based on file extension
func getContentType(filename string) string {
	ext := filepath.Ext(filename)
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".webm":
		return "video/webm"
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".txt":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

// serveFile is a helper that serves a file with specified content disposition
func serveFile(w http.ResponseWriter, r *http.Request, fileID string, inline bool) {
	// Clean the filename to prevent directory traversal attacks
	fileID = filepath.Base(fileID)
	fullPath := filepath.Join("uploads", fileID)

	// Check if file exists
	stat, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		log.Printf("File not found: %s", fullPath)
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(fullPath)
	if err != nil {
		log.Printf("File open error: %v", err)
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	// Try to get original filename from files map, otherwise use the stored name
	filename := fileID
	if fi, exists := files[fileID]; exists {
		filename = fi.Name
	}

	// Set appropriate headers
	contentType := getContentType(filename)
	w.Header().Set("Content-Type", contentType)

	if inline {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", filename))
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	}

	// Critical for iOS: set Content-Length header
	fileSize := stat.Size()
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))

	// Set cache control headers for media files
	w.Header().Set("Cache-Control", "public, max-age=3600")

	// Handle HTTP Range requests (HTTP 206 Partial Content)
	// This is important for iOS to support seeking and streaming
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		// Parse Range header (e.g., "bytes=0-1023" or "bytes=1024-")
		start, end, err := parseRange(rangeHeader, fileSize)
		if err == nil {
			// Seek to start position
			if _, err := f.Seek(start, 0); err == nil {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
				w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
				w.Header().Set("Accept-Ranges", "bytes")
				w.WriteHeader(http.StatusPartialContent)
				log.Printf("Serving range request for %s: bytes %d-%d/%d", filename, start, end, fileSize)
				io.CopyN(w, f, end-start+1)
				return
			}
		}
	}

	// Set Accept-Ranges header to indicate we support range requests
	w.Header().Set("Accept-Ranges", "bytes")

	log.Printf("Serving file: %s (size: %d bytes, inline: %v)", filename, fileSize, inline)

	_, err = io.Copy(w, f)
	if err != nil {
		log.Printf("File copy error: %v", err)
	}
}

// parseRange parses an HTTP Range header and returns start and end byte positions
func parseRange(rangeHeader string, fileSize int64) (int64, int64, error) {
	// Simple parser for "bytes=start-end" format
	if len(rangeHeader) < 6 || rangeHeader[:6] != "bytes=" {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	rangeSpec := rangeHeader[6:]
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	var start, end int64
	var err error

	if parts[0] == "" {
		// Suffix range: "-500" means last 500 bytes
		if parts[1] == "" {
			return 0, 0, fmt.Errorf("invalid range format")
		}
		suffixLength, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffixLength < 0 {
			return 0, 0, fmt.Errorf("invalid suffix length")
		}
		start = fileSize - suffixLength
		if start < 0 {
			start = 0
		}
		end = fileSize - 1
	} else {
		// Normal range: "100-200"
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || start < 0 {
			return 0, 0, fmt.Errorf("invalid start position")
		}

		if parts[1] == "" {
			// Open-ended range: "100-" means from 100 to end
			end = fileSize - 1
		} else {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil || end < start {
				return 0, 0, fmt.Errorf("invalid end position")
			}
		}
	}

	if start >= fileSize {
		return 0, 0, fmt.Errorf("range start beyond file size")
	}

	if end >= fileSize {
		end = fileSize - 1
	}

	return start, end, nil
}

// isVideoFile checks if the file is a video based on extension
func isVideoFile(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".mp4" || ext == ".mov" || ext == ".avi" || ext == ".webm"
}

// isAudioFile checks if the file is audio
func isAudioFile(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".mp3" || ext == ".wav" || ext == ".ogg"
}

// isImageFile checks if the file is an image
func isImageFile(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp"
}

// isPDFFile checks if the file is a PDF
func isPDFFile(filename string) bool {
	return filepath.Ext(filename) == ".pdf"
}

// isTextFile checks if the file is text
func isTextFile(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".txt" || ext == ".html" || ext == ".htm" || ext == ".json" || ext == ".xml"
}

// viewFileHandler renders an HTML page with embedded media player/viewer
func viewFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID := vars["id"]

	// Clean the filename
	fileID = filepath.Base(fileID)
	fullPath := filepath.Join("uploads", fileID)

	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	// Try to get original filename from files map
	filename := fileID
	if fi, exists := files[fileID]; exists {
		filename = fi.Name
	}

	contentType := getContentType(filename)

	// Read text content if it's a text file
	var textContent string
	if isTextFile(filename) {
		data, err := os.ReadFile(fullPath)
		if err == nil && len(data) < 1024*1024 { // Only read if < 1MB
			textContent = string(data)
		}
	}

	data := struct {
		FileName    string
		StreamURL   string
		DownloadURL string
		ContentType string
		IsVideo     bool
		IsAudio     bool
		IsImage     bool
		IsPDF       bool
		IsText      bool
		TextContent string
	}{
		FileName:    filename,
		StreamURL:   fmt.Sprintf("/stream/%s", fileID),
		DownloadURL: fmt.Sprintf("/download/%s", fileID),
		ContentType: contentType,
		IsVideo:     isVideoFile(filename),
		IsAudio:     isAudioFile(filename),
		IsImage:     isImageFile(filename),
		IsPDF:       isPDFFile(filename),
		IsText:      isTextFile(filename),
		TextContent: textContent,
	}

	if err := tmplView.Execute(w, data); err != nil {
		log.Printf("Template execute error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

// streamFileHandler serves the raw file with inline disposition for embedding
func streamFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID := vars["id"]
	serveFile(w, r, fileID, true)
}

// downloadFileHandler streams the requested file as an attachment for download
func downloadFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID := vars["id"]
	serveFile(w, r, fileID, false)
}

// scheme tries to detect http vs https, for building absolute URLs in displayFileHandler
func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// uploadFileHandler handles the "POST /upload" route.
// Expects a multipart/form-data with a 'file' field.
func uploadFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse up to 10 MB
	r.ParseMultipartForm(10 << 20)

	file, handler, err := r.FormFile("file")
	if err != nil {
		log.Printf("Error retrieving file from form data: %v", err)
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Ensure uploads dir, used when run outside container
	os.MkdirAll("uploads", 0755)

	// Build a unique ID / filename for the stored file
	// For example, <timestamp>-<originalname>
	uniqueID := fmt.Sprintf("%d-%s", time.Now().UnixNano(), filepath.Base(handler.Filename))
	fullPath := filepath.Join("uploads", uniqueID)

	dst, err := os.Create(fullPath)
	if err != nil {
		log.Printf("Error creating file on server: %v", err)
		http.Error(w, "Cannot create file on server", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	if err != nil {
		log.Printf("Error saving file: %v", err)
		http.Error(w, "Cannot save file", http.StatusInternalServerError)
		return
	}

	fi := FileInfo{
		ID:         uniqueID,
		Name:       handler.Filename,
		StoredName: uniqueID,
	}
	files[uniqueID] = fi

	http.Redirect(w, r, "/file/"+uniqueID, http.StatusSeeOther)
}

// generateQRCodeBase64 generates a QR code for the given URL and returns it as base64-encoded string
func generateQRCodeBase64(url string) (string, error) {
	png, err := qrcode.Encode(url, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(png), nil
}

func displayFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID := vars["id"]

	// Clean the filename to prevent directory traversal attacks
	fileID = filepath.Base(fileID)
	fullPath := filepath.Join("uploads", fileID)

	// Check if file exists on disk
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	// Try to get original filename from files map, otherwise use the stored name
	filename := fileID
	if fi, exists := files[fileID]; exists {
		filename = fi.Name
	}

	// QR code points to view URL for inline viewing on mobile
	viewURL := fmt.Sprintf("%s://%s/view/%s", scheme(r), r.Host, fileID)

	// QR code generation
	base64QR, err := generateQRCodeBase64(viewURL)
	if err != nil {
		log.Printf("QR code generation error: %v", err)
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}

	data := struct {
		FileName    string
		ViewURL     string
		DownloadURL string
		QRCodeData  string
	}{
		FileName:    filename,
		ViewURL:     fmt.Sprintf("/view/%s", fileID),
		DownloadURL: fmt.Sprintf("/download/%s", fileID),
		QRCodeData:  base64QR,
	}

	if err := tmplDisplayFile.Execute(w, data); err != nil {
		log.Printf("Template execute error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}
