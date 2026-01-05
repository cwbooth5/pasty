package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/gorilla/mux"
)

// Test randomString function
func TestRandomString(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"length 3", 3},
		{"length 5", 5},
		{"length 10", 10},
		{"length 0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := randomString(tt.length)
			if len(result) != tt.length {
				t.Errorf("randomString(%d) length = %d, want %d", tt.length, len(result), tt.length)
			}

			// Verify all characters are from snippetChars
			for _, char := range result {
				valid := false
				for _, validChar := range snippetChars {
					if char == validChar {
						valid = true
						break
					}
				}
				if !valid {
					t.Errorf("randomString() contained invalid character: %c", char)
				}
			}
		})
	}
}

// Test randomString produces different values
func TestRandomStringUniqueness(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 100; i++ {
		str := randomString(5)
		results[str] = true
	}

	// With 100 attempts, we should get at least 50 unique values
	if len(results) < 50 {
		t.Errorf("randomString() not random enough, got only %d unique values out of 100", len(results))
	}
}

// Test truncateText function
func TestTruncateText(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		maxLen int
		want   string
	}{
		{"empty string", "", 10, ""},
		{"shorter than max", "hello", 10, "hello"},
		{"exactly max length", "1234567890", 10, "1234567890"},
		{"longer than max", "hello world this is long", 10, "hello worl..."},
		{"zero max length", "hello", 0, "..."},
		{"single char truncate", "hello", 1, "h..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateText(tt.text, tt.maxLen)
			if result != tt.want {
				t.Errorf("truncateText(%q, %d) = %q, want %q", tt.text, tt.maxLen, result, tt.want)
			}
		})
	}
}

// Test buildSnippetsList function
func TestBuildSnippetsList(t *testing.T) {
	tests := []struct {
		name       string
		snippets   map[string]Snippet
		maxResults int
		wantCount  int
	}{
		{
			name:       "empty map",
			snippets:   map[string]Snippet{},
			maxResults: 10,
			wantCount:  0,
		},
		{
			name: "single snippet",
			snippets: map[string]Snippet{
				"abc": {Title: "Test", Text: "Hello world"},
			},
			maxResults: 10,
			wantCount:  1,
		},
		{
			name: "truncation applied",
			snippets: map[string]Snippet{
				"abc": {Title: "Test", Text: "This is a very long text that should be truncated"},
			},
			maxResults: 10,
			wantCount:  1,
		},
		{
			name: "max results limit",
			snippets: map[string]Snippet{
				"a": {Title: "1", Text: "text1"},
				"b": {Title: "2", Text: "text2"},
				"c": {Title: "3", Text: "text3"},
				"d": {Title: "4", Text: "text4"},
				"e": {Title: "5", Text: "text5"},
			},
			maxResults: 3,
			wantCount:  3,
		},
		{
			name: "zero max results returns all",
			snippets: map[string]Snippet{
				"a": {Title: "1", Text: "text1"},
				"b": {Title: "2", Text: "text2"},
			},
			maxResults: 0,
			wantCount:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := buildSnippetsList(tt.snippets, tt.maxResults)
			if len(results) != tt.wantCount {
				t.Errorf("buildSnippetsList() returned %d snippets, want %d", len(results), tt.wantCount)
			}

			// Verify truncation was applied
			for _, snippet := range results {
				if len(snippet.TruncatedText) > 13 { // 10 chars + "..."
					t.Errorf("TruncatedText too long: %q", snippet.TruncatedText)
				}
			}
		})
	}
}

// Test saveSnippetsToFile and loadSnippetsFromFile
func TestSaveAndLoadSnippets(t *testing.T) {
	tmpDir := t.TempDir()
	filename := filepath.Join(tmpDir, "test_snippets.json")

	testSnippets := map[string]Snippet{
		"abc": {
			Title:            "Test Title",
			Text:             "Test text content",
			BurnAfterReading: false,
		},
		"xyz": {
			Title:            "Another Test",
			Text:             "More content",
			BurnAfterReading: true,
		},
	}

	// Save the global snippets map
	originalSnippets := snippets
	snippets = testSnippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	// Test save
	saveSnippetsToFile(filename)

	// Verify file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Fatalf("saveSnippetsToFile() did not create file")
	}

	// Verify JSON content
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}

	var loaded map[string]Snippet
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Failed to parse saved JSON: %v", err)
	}

	if len(loaded) != len(testSnippets) {
		t.Errorf("Saved %d snippets, want %d", len(loaded), len(testSnippets))
	}

	// Test load
	snippets = make(map[string]Snippet) // Reset
	loadSnippetsFromFile(filename)

	if len(snippets) != len(testSnippets) {
		t.Errorf("Loaded %d snippets, want %d", len(snippets), len(testSnippets))
	}

	// Verify content matches
	for id, want := range testSnippets {
		got, exists := snippets[id]
		if !exists {
			t.Errorf("Snippet %s not loaded", id)
			continue
		}
		if got.Title != want.Title || got.Text != want.Text || got.BurnAfterReading != want.BurnAfterReading {
			t.Errorf("Snippet %s mismatch: got %+v, want %+v", id, got, want)
		}
	}
}

// Test loadSnippetsFromFile with non-existent file
func TestLoadSnippetsFromFile_NonExistent(t *testing.T) {
	originalSnippets := snippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	snippets = make(map[string]Snippet)
	loadSnippetsFromFile("/nonexistent/file.json")

	// Should not crash and snippets should be empty
	if len(snippets) != 0 {
		t.Errorf("Expected empty snippets map, got %d entries", len(snippets))
	}
}

// Test generateURL uniqueness
func TestGenerateURL(t *testing.T) {
	originalSnippets := snippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	snippets = make(map[string]Snippet)

	// Generate multiple URLs and verify uniqueness
	urls := make(map[string]bool)
	for i := 0; i < 10; i++ {
		url := generateURL()
		if urls[url] {
			t.Errorf("generateURL() produced duplicate: %s", url)
		}
		urls[url] = true
		snippets[url] = Snippet{} // Add to map to simulate usage
	}

	// Verify all URLs are 3 characters
	for url := range urls {
		if len(url) != 3 {
			t.Errorf("generateURL() produced URL with wrong length: %s (len=%d)", url, len(url))
		}
	}
}

// Helper to create test templates
func initTestTemplates(t *testing.T) {
	t.Helper()

	// Create minimal test templates if they don't exist
	if tmplDisplay == nil {
		tmplDisplay = template.Must(template.New("display").Parse(`{{.Title}}: {{.Text}}`))
	}
	if tmplIndex == nil {
		tmplIndex = template.Must(template.New("index").Parse(`Snippets: {{len .Snippets}}`))
	}
}

// Test handleSave HTTP handler
func TestHandleSave(t *testing.T) {
	originalSnippets := snippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	snippets = make(map[string]Snippet)

	form := url.Values{}
	form.Add("title", "Test Title")
	form.Add("text", "Test content")
	form.Add("burn", "true")

	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handleSave(w, req)

	// Check redirect status
	if w.Code != http.StatusSeeOther {
		t.Errorf("handleSave() status = %d, want %d", w.Code, http.StatusSeeOther)
	}

	// Check that snippet was created
	if len(snippets) != 1 {
		t.Errorf("handleSave() created %d snippets, want 1", len(snippets))
	}

	// Verify snippet content
	for _, snippet := range snippets {
		if snippet.Title != "Test Title" {
			t.Errorf("Snippet title = %s, want 'Test Title'", snippet.Title)
		}
		if snippet.Text != "Test content" {
			t.Errorf("Snippet text = %s, want 'Test content'", snippet.Text)
		}
		if !snippet.BurnAfterReading {
			t.Errorf("Snippet burn = %v, want true", snippet.BurnAfterReading)
		}
	}
}

// Test handleSave with empty title
func TestHandleSave_EmptyTitle(t *testing.T) {
	originalSnippets := snippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	snippets = make(map[string]Snippet)

	form := url.Values{}
	form.Add("text", "Test content")

	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	handleSave(w, req)

	// Verify default title "None" was used
	for _, snippet := range snippets {
		if snippet.Title != "None" {
			t.Errorf("Snippet title = %s, want 'None'", snippet.Title)
		}
	}
}

// Test displaySnippet HTTP handler
func TestDisplaySnippet(t *testing.T) {
	originalSnippets := snippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	initTestTemplates(t)

	snippets = map[string]Snippet{
		"abc": {
			Title:            "Test",
			Text:             "Content",
			BurnAfterReading: false,
		},
	}

	req := httptest.NewRequest("GET", "/display/abc", nil)
	req = mux.SetURLVars(req, map[string]string{"url": "abc"})
	w := httptest.NewRecorder()

	displaySnippet(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("displaySnippet() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify snippet still exists (not burned)
	if _, exists := snippets["abc"]; !exists {
		t.Error("Snippet was deleted but BurnAfterReading was false")
	}
}

// Test displaySnippet with burn after reading
func TestDisplaySnippet_BurnAfterReading(t *testing.T) {
	originalSnippets := snippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	initTestTemplates(t)

	snippets = map[string]Snippet{
		"xyz": {
			Title:            "Burn Me",
			Text:             "Secret",
			BurnAfterReading: true,
		},
	}

	req := httptest.NewRequest("GET", "/display/xyz", nil)
	req = mux.SetURLVars(req, map[string]string{"url": "xyz"})
	w := httptest.NewRecorder()

	displaySnippet(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("displaySnippet() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify snippet was deleted
	if _, exists := snippets["xyz"]; exists {
		t.Error("Snippet should have been deleted after reading")
	}
}

// Test displaySnippet with non-existent ID
func TestDisplaySnippet_NotFound(t *testing.T) {
	originalSnippets := snippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	snippets = make(map[string]Snippet)

	req := httptest.NewRequest("GET", "/display/nonexistent", nil)
	req = mux.SetURLVars(req, map[string]string{"url": "nonexistent"})
	w := httptest.NewRecorder()

	displaySnippet(w, req)

	// Should redirect to home
	if w.Code != http.StatusSeeOther {
		t.Errorf("displaySnippet() status = %d, want %d (redirect)", w.Code, http.StatusSeeOther)
	}
}

// Test deleteSnippet HTTP handler
func TestDeleteSnippet(t *testing.T) {
	originalSnippets := snippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	snippets = map[string]Snippet{
		"abc": {Title: "Test", Text: "Content"},
	}

	req := httptest.NewRequest("POST", "/delete/abc", nil)
	req = mux.SetURLVars(req, map[string]string{"url": "abc"})
	w := httptest.NewRecorder()

	deleteSnippet(w, req)

	// Check redirect
	if w.Code != http.StatusSeeOther {
		t.Errorf("deleteSnippet() status = %d, want %d", w.Code, http.StatusSeeOther)
	}

	// Verify snippet was deleted
	if _, exists := snippets["abc"]; exists {
		t.Error("Snippet should have been deleted")
	}
}

// Test serveIndex HTTP handler
func TestServeIndex(t *testing.T) {
	originalSnippets := snippets
	t.Cleanup(func() {
		snippets = originalSnippets
	})

	initTestTemplates(t)

	snippets = map[string]Snippet{
		"abc": {Title: "Test1", Text: "Content1"},
		"xyz": {Title: "Test2", Text: "Content2"},
	}

	// Create temp uploads directory
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	os.MkdirAll("uploads", 0755)
	t.Cleanup(func() {
		os.Chdir(originalWd)
	})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	serveIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("serveIndex() status = %d, want %d", w.Code, http.StatusOK)
	}
}
