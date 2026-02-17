// Package main implements the sovereign-notes web service
package main

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"

	"github.com/gorilla/mux"
)

// Note represents a note in the system
type Note struct {
	ID        int       `json:"id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// VersionInfo contains application version information
type VersionInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time,omitempty"`
	GitCommit string `json:"git_commit,omitempty"`
}

var (
	db      *sql.DB
	version = "1.0.0" // Set via build flags
)

func main() {
	// Get configuration from environment
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Connect to database
	var err error
	db, err = sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	// Initialize database schema
	if err := initializeSchema(); err != nil {
		log.Fatal("Failed to initialize database schema:", err)
	}

	// Setup routes
	r := mux.NewRouter()

	// Health and readiness endpoints
	r.HandleFunc("/healthz", healthHandler).Methods("GET")
	r.HandleFunc("/readyz", readinessHandler).Methods("GET")
	r.HandleFunc("/version", versionHandler).Methods("GET")

	// API endpoints
	r.HandleFunc("/notes", listNotesHandler).Methods("GET")
	r.HandleFunc("/notes", createNoteHandler).Methods("POST")
	r.HandleFunc("/notes/{id:[0-9]+}", getNoteHandler).Methods("GET")
	r.HandleFunc("/notes/{id:[0-9]+}", deleteNoteHandler).Methods("DELETE")

	// UI endpoint
	r.HandleFunc("/", uiHandler).Methods("GET")

	// ORD (Open Resource Discovery) endpoints
	r.HandleFunc("/.well-known/open-resource-discovery", ordConfigHandler).Methods("GET")
	r.HandleFunc("/ord/v1/document", ordDocumentHandler).Methods("GET")

	log.Printf("Starting sovereign-notes server on port %s", port)
	log.Printf("Version: %s", version)

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

// initializeSchema creates the notes table if it doesn't exist
func initializeSchema() error {
	query := `
		CREATE TABLE IF NOT EXISTS notes (
			id SERIAL PRIMARY KEY,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`
	_, err := db.Exec(query)
	return err
}

// Health check endpoint
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// Readiness check endpoint (includes database connectivity)
func readinessHandler(w http.ResponseWriter, r *http.Request) {
	if err := db.Ping(); err != nil {
		http.Error(w, "Database not ready: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}

// Version endpoint
func versionHandler(w http.ResponseWriter, r *http.Request) {
	versionInfo := VersionInfo{
		Version: version,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versionInfo)
}

// List all notes
func listNotesHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, content, created_at FROM notes ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, "Failed to query notes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.Content, &note.CreatedAt); err != nil {
			http.Error(w, "Failed to scan note: "+err.Error(), http.StatusInternalServerError)
			return
		}
		notes = append(notes, note)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notes)
}

// Create a new note
func createNoteHandler(w http.ResponseWriter, r *http.Request) {
	var note Note
	if err := json.NewDecoder(r.Body).Decode(&note); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if note.Content == "" {
		http.Error(w, "Content is required", http.StatusBadRequest)
		return
	}

	err := db.QueryRow(
		"INSERT INTO notes (content) VALUES ($1) RETURNING id, created_at",
		note.Content,
	).Scan(&note.ID, &note.CreatedAt)
	if err != nil {
		http.Error(w, "Failed to create note: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(note)
}

// Get a specific note
func getNoteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	var note Note
	err = db.QueryRow(
		"SELECT id, content, created_at FROM notes WHERE id = $1",
		id,
	).Scan(&note.ID, &note.Content, &note.CreatedAt)

	if err == sql.ErrNoRows {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Failed to query note: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(note)
}

// Delete a note
func deleteNoteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("DELETE FROM notes WHERE id = $1", id)
	if err != nil {
		http.Error(w, "Failed to delete note: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Failed to check deletion result", http.StatusInternalServerError)
		return
	}

	if rowsAffected == 0 {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Simple HTML UI
func uiHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>Sovereign Notes</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        .note { border: 1px solid #ddd; padding: 15px; margin: 10px 0; border-radius: 5px; }
        .form { background: #f5f5f5; padding: 20px; border-radius: 5px; margin: 20px 0; }
        input[type="text"] { width: 100%; padding: 8px; margin: 5px 0; }
        button { background: #007cba; color: white; padding: 10px 20px; border: none; border-radius: 3px; cursor: pointer; }
        .version { color: #666; font-size: 0.9em; }
    </style>
</head>
<body>
    <h1>Sovereign Notes</h1>
    <p class="version">Version: {{.Version}} | OCM Conformance Scenario</p>
    
    <div class="form">
        <h3>Add Note</h3>
        <input type="text" id="noteInput" placeholder="Enter your note..." />
        <button onclick="addNote()">Add Note</button>
    </div>
    
    <div id="notes"></div>
    
    <script>
        async function loadNotes() {
            const response = await fetch('/notes');
            const notes = await response.json();
            const notesDiv = document.getElementById('notes');
            notesDiv.innerHTML = notes.map(note => 
                '<div class="note">' +
                '<strong>Note #' + note.id + '</strong><br>' +
                note.content + '<br>' +
                '<small>' + new Date(note.created_at).toLocaleString() + '</small>' +
                '</div>'
            ).join('');
        }
        
        async function addNote() {
            const content = document.getElementById('noteInput').value;
            if (!content) return;
            
            await fetch('/notes', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ content })
            });
            
            document.getElementById('noteInput').value = '';
            loadNotes();
        }
        
        loadNotes();
    </script>
</body>
</html>
`

	t, err := template.New("ui").Parse(tmpl)
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	t.Execute(w, VersionInfo{Version: version})
}

// ORD configuration endpoint
func ordConfigHandler(w http.ResponseWriter, r *http.Request) {
	config := map[string]interface{}{
		"openResourceDiscoveryV1": map[string]interface{}{
			"documents": []map[string]any{
				{
					"url":         "/ord/v1/document",
					"systemTypes": []string{"sovereign-notes"},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// ORD document endpoint
func ordDocumentHandler(w http.ResponseWriter, r *http.Request) {
	// This would typically load from the ord-document resource
	// For now, return a basic document structure
	document := map[string]interface{}{
		"$schema":               "https://sap.github.io/open-resource-discovery/spec-v1/schemas/ord-document-v1.schema.json",
		"openResourceDiscovery": "1.9.0",
		"description":           "OCM Sovereign Notes API",
		"systemTypes": []map[string]interface{}{
			{
				"ordId":       "sovereign-notes",
				"title":       "Sovereign Notes System",
				"description": "A simple notes management system for OCM conformance testing",
			},
		},
		"apiResources": []map[string]interface{}{
			{
				"ordId":       "sovereign-notes:api:v1",
				"title":       "Notes API v1",
				"description": "REST API for managing notes",
				"entryPoints": []string{"/notes"},
				"apiProtocol": "rest",
				"version":     version,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(document)
}
