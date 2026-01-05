package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/template"

	"github.com/gorilla/mux"
	qrcode "github.com/skip2/go-qrcode"
)

// Snippet holds the title and text of a paste
type Snippet struct {
	Title            string `json:"title"`
	Text             string `json:"text"`
	BurnAfterReading bool   `json:"burn_after_reading"`
}

// Global map: snippet ID -> Snippet
var snippets = make(map[string]Snippet)

// Templates
var (
	tmplIndex       *template.Template
	tmplDisplay     *template.Template
	tmplDisplayFile *template.Template
	tmplView        *template.Template
)

// Data structures for templates
type DisplayData struct {
	ID         string
	Title      string
	Text       string
	Link       string
	HomeQRCode string
}

type FileEntry struct {
	ID   string
	Name string
}
type IndexData struct {
	Snippets   []SnippetInfo
	Files      []FileEntry
	HomeQRCode string
}

// For the index page table (snippet list)
type SnippetInfo struct {
	ID            string
	Title         string
	TruncatedText string
}

// Names of snippet URLs use these simple options
var snippetChars = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

// randomString generates a random string of length n from snippetChars.
func randomString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = snippetChars[rand.Intn(len(snippetChars))]
	}
	return string(b)
}

func main() {
	// Parse command-line flags
	host := flag.String("host", "localhost", "Host to listen on")
	port := flag.String("port", "3015", "Port to listen on")
	flag.Parse()

	loadSnippetsFromFile("snippets.json")

	tmplIndex = parseTemplate("templates/index.html")
	tmplDisplay = parseTemplate("templates/display.html")
	tmplDisplayFile = parseTemplate("templates/display_file.html")
	tmplView = parseTemplate("templates/view.html")

	r := mux.NewRouter()
	r.HandleFunc("/", serveIndex).Methods("GET")
	r.HandleFunc("/save", handleSave).Methods("POST")
	r.HandleFunc("/display/{url}", displaySnippet).Methods("GET")
	r.HandleFunc("/delete/{url}", deleteSnippet).Methods("POST")

	r.HandleFunc("/upload", uploadFileHandler).Methods("POST")
	r.HandleFunc("/file/{id}", displayFileHandler).Methods("GET")
	r.HandleFunc("/view/{id}", viewFileHandler).Methods("GET")
	r.HandleFunc("/stream/{id}", streamFileHandler).Methods("GET")
	r.HandleFunc("/download/{id}", downloadFileHandler).Methods("GET")

	setupGracefulShutdown()

	addr := fmt.Sprintf("%s:%s", *host, *port)
	fmt.Printf("Server is running at http://%s/\n", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

// setupGracefulShutdown sets up a handler for OS signals (Ctrl+C, SIGTERM)
// to save data before exiting.
func setupGracefulShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Gracefully shutting down...")
		saveSnippetsToFile("snippets.json")
		os.Exit(0)
	}()
}

// loadSnippetsFromFile loads snippet data from JSON into the global `snippets` map.
func loadSnippetsFromFile(filename string) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		log.Printf("No %s file found, starting with empty data.\n", filename)
		return
	}

	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Could not open %s: %v", filename, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&snippets)
	if err != nil {
		log.Fatalf("Failed to decode JSON from %s: %v", filename, err)
	}

	log.Printf("Loaded %d snippets from %s.\n", len(snippets), filename)
}

// saveSnippetsToFile saves the global `snippets` map to disk as JSON.
// This is a cheap storage option for now. Maybe use sqlite later IDK
func saveSnippetsToFile(filename string) {
	data, err := json.MarshalIndent(snippets, "", "  ")
	if err != nil {
		log.Printf("Error marshaling snippets data: %v", err)
		return
	}

	tmpFile := filename + ".tmp"
	if err = os.WriteFile(tmpFile, data, 0644); err != nil {
		log.Printf("Error writing temp file %s: %v", tmpFile, err)
		return
	}

	// try to be atomic and stuff
	if err = os.Rename(tmpFile, filename); err != nil {
		log.Printf("Error renaming temp file: %v", err)
		return
	}

	log.Printf("Successfully saved %d snippets to %s.\n", len(snippets), filename)
}

// parseTemplate is a helper to parse a single template file.
func parseTemplate(path string) *template.Template {
	tmpl, err := template.ParseFiles(filepath.Clean(path))
	if err != nil {
		log.Fatalf("Error parsing template %s: %v", path, err)
	}
	return tmpl
}

// generateHomeQRCode generates a QR code for the home page URL
func generateHomeQRCode(r *http.Request) string {
	// Build absolute URL to home page
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	homeURL := fmt.Sprintf("%s://%s/", scheme, r.Host)

	// Generate QR code
	png, err := qrcode.Encode(homeURL, qrcode.Medium, 256)
	if err != nil {
		log.Printf("QR code generation error: %v", err)
		return ""
	}
	return base64.StdEncoding.EncodeToString(png)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	snippets := getAllSnippetsDescending()

	var fileEntries []FileEntry

	entries, err := os.ReadDir("uploads")
	if err != nil {
		log.Printf("Error reading uploads directory: %v", err)
	} else {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			fileName := entry.Name()
			fileEntries = append(fileEntries, FileEntry{
				ID:   fileName,
				Name: fileName,
			})
		}
	}

	data := IndexData{
		Snippets:   snippets,
		Files:      fileEntries,
		HomeQRCode: generateHomeQRCode(r),
	}

	if err := tmplIndex.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// handleSave creates a new snippet, saves to map, and also saves to disk.
func handleSave(w http.ResponseWriter, r *http.Request) {
	title := r.FormValue("title")
	text := r.FormValue("text")
	if title == "" {
		title = "None"
	}

	// Check if the 'burn' checkbox was set
	burnValue := r.FormValue("burn") // will be "true" if checked, else ""
	burnAfterReading := (burnValue == "true")

	// Generate an ID and store the snippet
	url := generateURL()
	snippets[url] = Snippet{
		Title:            title,
		Text:             text,
		BurnAfterReading: burnAfterReading,
	}

	saveSnippetsToFile("snippets.json")

	http.Redirect(w, r, "/display/"+url, http.StatusSeeOther)
}

// displaySnippet shows the snippet in the display template.
func displaySnippet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	url := vars["url"]

	snippet, ok := snippets[url]
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	data := DisplayData{
		ID:         url,
		Title:      snippet.Title,
		Text:       snippet.Text,
		Link:       "/display/" + url,
		HomeQRCode: generateHomeQRCode(r),
	}

	if err := tmplDisplay.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// TODO, too aggressive
	if snippet.BurnAfterReading {
		delete(snippets, url)
		saveSnippetsToFile("snippets.json")
	}
}

// deleteSnippet removes a snippet and saves state to disk.
func deleteSnippet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	url := vars["url"]

	delete(snippets, url)

	saveSnippetsToFile("snippets.json")

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// generateURL is a simplistic ID generator (just numeric).
func generateURL() string {
	for {
		id := randomString(3) // 3-character string
		if _, exists := snippets[id]; !exists {
			return id
		}
		// Otherwise, loop again and generate a new ID
	}
}

// truncateText returns the text truncated to maxLen with "..." appended if needed
func truncateText(text string, maxLen int) string {
	if len(text) > maxLen {
		return text[:maxLen] + "..."
	}
	return text
}

// buildSnippetsList converts a snippets map to a list of SnippetInfo, with truncated text
func buildSnippetsList(snippetsMap map[string]Snippet, maxResults int) []SnippetInfo {
	var results []SnippetInfo

	for idStr, snippet := range snippetsMap {
		results = append(results, SnippetInfo{
			ID:            idStr,
			Title:         snippet.Title,
			TruncatedText: truncateText(snippet.Text, 10),
		})
	}

	// Return up to maxResults
	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

func getAllSnippetsDescending() []SnippetInfo {
	return buildSnippetsList(snippets, 10)
}
