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

// downloadFileHandler streams the requested file to the client.
func downloadFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID := vars["id"]

	fi, exists := files[fileID]
	if !exists {
		http.NotFound(w, r)
		return
	}

	fullPath := filepath.Join("uploads", fi.StoredName)

	f, err := os.Open(fullPath)
	if err != nil {
		log.Printf("File open error: %v", err)
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fi.Name))
	w.Header().Set("Content-Type", "application/octet-stream")

	_, err = io.Copy(w, f)
	if err != nil {
		log.Printf("File copy error: %v", err)
	}
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

func displayFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID := vars["id"]

	fi, exists := files[fileID]
	if !exists {
		http.NotFound(w, r)
		return
	}

	downloadURL := fmt.Sprintf("%s://%s/download/%s", scheme(r), r.Host, fileID)

	// QR code generation
	png, err := qrcode.Encode(downloadURL, qrcode.Medium, 256)
	if err != nil {
		log.Printf("QR code generation error: %v", err)
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}

	// Convert PNG bytes to base64 for embedding in <img> tag
	base64QR := base64.StdEncoding.EncodeToString(png)

	data := struct {
		FileName    string
		DownloadURL string
		QRCodeData  string
	}{
		FileName:    fi.Name,
		DownloadURL: fmt.Sprintf("/download/%s", fileID),
		QRCodeData:  base64QR,
	}

	if err := tmplDisplayFile.Execute(w, data); err != nil {
		log.Printf("Template execute error: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}
