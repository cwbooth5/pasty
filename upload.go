package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
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

	_, err = io.Copy(w, f)
	if err != nil {
		log.Printf("File copy error: %v", err)
	}
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
