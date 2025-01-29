package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/template"

	"github.com/gorilla/mux"
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
)

var config Config

// Data structures for templates
type DisplayData struct {
	ID    string
	Title string
	Text  string
	Link  string
}

type FileEntry struct {
	ID   string
	Name string
}
type IndexData struct {
	Snippets []SnippetInfo
	Files    []FileEntry
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

func buildTLSConfig(cfg Config) (*tls.Config, error) {
	// var pemBlocks []*pem.Block
	// var v *pem.Block
	// var pkey []byte

	// for {
	// 	v, b = pem.Decode(b)
	// 	if v == nil {
	// 		break
	// 	}
	// 	if v.Type == "RSA PRIVATE KEY" {
	// 		if x509.IsEncryptedPEMBlock(v) {
	// 			pkey, _ = x509.DecryptPEMBlock(v, []byte("xxxxxxxxx"))
	// 			pkey = pem.EncodeToMemory(&pem.Block{
	// 				Type:  v.Type,
	// 				Bytes: pkey,
	// 			})
	// 		} else {
	// 			pkey = pem.EncodeToMemory(v)
	// 		}
	// 	} else {
	// 		pemBlocks = append(pemBlocks, v)
	// 	}
	// }
	// c, _ := tls.X509KeyPair(pem.EncodeToMemory(pemBlocks[0]), pkey)

	// Base TLS config
	tlsConfig := &tls.Config{}

	if cfg.AuthEnabled {
		// mTLS scenario
		// 1) Load the CA certificate(s) used to trust client certs
		caCert, err := ioutil.ReadFile("ca_cert.pem")
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert file: %v", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append CA cert")
		}

		// 2) Require client certificate
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = caPool

		// 3) Provide a custom verification function if we want to check the username in the client cert
		tlsConfig.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(verifiedChains) < 1 || len(verifiedChains[0]) < 1 {
				return fmt.Errorf("no verified certificate chain")
			}
			cert := verifiedChains[0][0]

			// Check the Common Name. (Or check Subject Alternative Name if your environment uses that.)
			cn := cert.Subject.CommonName
			if cn != cfg.Username {
				// Log the attempt
				log.Printf("Rejected client cert from CN=%s (expected CN=%s)", cn, cfg.Username)
				return fmt.Errorf("client cert CN does not match allowed username")
			}

			// Success
			log.Printf("Accepted client cert from CN=%s", cn)
			return nil
		}
	}

	return tlsConfig, nil
}

func main() {

	var err error
	config, err = LoadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	loadSnippetsFromFile("snippets.json")

	tmplIndex = parseTemplate("templates/index.html")
	tmplDisplay = parseTemplate("templates/display.html")
	tmplDisplayFile = parseTemplate("templates/display_file.html")

	r := mux.NewRouter()
	r.HandleFunc("/", serveIndex).Methods("GET")
	r.HandleFunc("/save", handleSave).Methods("POST")
	r.HandleFunc("/display/{url}", displaySnippet).Methods("GET")
	r.HandleFunc("/delete/{url}", deleteSnippet).Methods("POST")

	r.HandleFunc("/upload", uploadFileHandler).Methods("POST")
	r.HandleFunc("/file/{id}", displayFileHandler).Methods("GET")
	r.HandleFunc("/download/{id}", downloadFileHandler).Methods("GET")

	setupGracefulShutdown()

	// server startup logic
	if !config.SSLEnabled {
		// Plain HTTP
		log.Printf("Starting HTTP server on :8090 (no SSL)")
		if err := http.ListenAndServe(":8090", r); err != nil {
			log.Fatalf("ListenAndServe error: %v", err)
		}
	} else {
		// SSL is enabled
		// if auth_enabled == false => normal TLS
		// if auth_enabled == true  => mutual TLS

		// Build a tls.Config
		tlsCfg, err := buildTLSConfig(config)
		if err != nil {
			log.Fatalf("Error building TLS config: %v", err)
		}

		server := &http.Server{
			Addr:      ":8090",
			Handler:   r,
			TLSConfig: tlsCfg,
		}
		log.Printf("Starting HTTPS server on :8090 (SSL=%v, auth=%v)", config.SSLEnabled, config.AuthEnabled)
		if config.AuthEnabled {
			log.Println("mTLS is enforced; client must present certificate with CN=", config.Username)
		}

		// Provide server certificates (cert.pem, key.pem) if normal TLS or mTLS
		// The TLS handshake will enforce client cert if mTLS is set up.
		if err := server.ListenAndServeTLS("server_cert.pem", "server_key.pem"); err != nil {
			log.Fatalf("ListenAndServeTLS error: %v", err)
		}
	}

	// fmt.Println("Server is running at http://localhost:8090/")
	// log.Fatal(http.ListenAndServe(":8090", r)) // TODO: hardcoded
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
		Snippets: snippets,
		Files:    fileEntries,
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
		ID:    url,
		Title: snippet.Title,
		Text:  snippet.Text,
		Link:  "/display/" + url,
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

func getAllSnippetsDescending() []SnippetInfo {
	var results []SnippetInfo

	for idStr, snippet := range snippets {
		truncated := snippet.Text
		if len(truncated) > 10 {
			truncated = truncated[:10] + "..."
		}

		results = append(results, SnippetInfo{
			ID:            idStr,
			Title:         snippet.Title,
			TruncatedText: truncated,
		})
	}

	// Return up to 10
	if len(results) > 10 {
		results = results[:10]
	}

	return results
}
