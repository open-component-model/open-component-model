// Package main implements the sovereign-notes v1.0.0 web service.
// This version ships with the initial database schema directly —
// no migration tracking is used.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"

	"github.com/gorilla/mux"
)

// Note represents a note in the system (v1.0.0 — no title field).
type Note struct {
	ID        int       `json:"id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// VersionInfo contains application version information.
type VersionInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time,omitempty"`
	GitCommit string `json:"git_commit,omitempty"`
}

var (
	db      *sql.DB
	version = "1.0.0"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	var err error
	db, err = sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	if err := db.PingContext(context.Background()); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	// v1.0.0 ships with the initial schema directly.
	if err := initSchema(); err != nil {
		log.Fatal("Failed to initialize database schema:", err)
	}

	defer db.Close()

	r := mux.NewRouter()

	r.HandleFunc("/healthz", healthHandler).Methods("GET")
	r.HandleFunc("/readyz", readinessHandler).Methods("GET")
	r.HandleFunc("/version", versionHandler).Methods("GET")

	r.HandleFunc("/notes", listNotesHandler).Methods("GET")
	r.HandleFunc("/notes", createNoteHandler).Methods("POST")
	r.HandleFunc("/notes/{id:[0-9]+}", getNoteHandler).Methods("GET")
	r.HandleFunc("/notes/{id:[0-9]+}", deleteNoteHandler).Methods("DELETE")

	r.HandleFunc("/", uiHandler).Methods("GET")

	r.HandleFunc("/.well-known/open-resource-discovery", ordConfigHandler).Methods("GET")
	r.HandleFunc("/ord/v1/document", ordDocumentHandler).Methods("GET")

	log.Printf("Starting sovereign-notes server on port %s", port) //nolint:gosec // G706 - port from env var, logged intentionally
	log.Printf("Version: %s", version)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("Server failed to start:", err) //nolint:gocritic // db connections can fail here if we panic, but we dont mind for a demo
	}
}

// initSchema creates the initial database schema for v1.0.0.
// It records the schema version in a migrations table so that
// future versions (v1.1.0+) can apply incremental migrations.
func initSchema() error {
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock(1)"); err != nil {
		return fmt.Errorf("failed to acquire advisory lock: %w", err)
	}
	defer db.ExecContext(ctx, "SELECT pg_advisory_unlock(1)") //nolint:errcheck // best-effort cleanup/response write

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS notes (
			id SERIAL PRIMARY KEY,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("failed to create notes table: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Record initial schema so v1.1.0 migrations know where to start.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO schema_migrations (id) VALUES (1) ON CONFLICT DO NOTHING
	`); err != nil {
		return fmt.Errorf("failed to record initial schema version: %w", err)
	}

	log.Println("Initial schema (v1.0.0) applied successfully")
	return nil
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK")) //nolint:errcheck // best-effort cleanup/response write
}

func readinessHandler(w http.ResponseWriter, r *http.Request) {
	if err := db.PingContext(r.Context()); err != nil {
		http.Error(w, "Database not ready: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready")) //nolint:errcheck // best-effort cleanup/response write
}

func versionHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(VersionInfo{Version: version}); err != nil {
		log.Printf("failed to encode version response: %v", err)
	}
}

func listNotesHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, content, created_at FROM notes ORDER BY created_at DESC")
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
	if err := rows.Err(); err != nil {
		http.Error(w, "Failed to iterate notes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(notes); err != nil {
		log.Printf("failed to encode notes response: %v", err)
	}
}

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

	err := db.QueryRowContext(r.Context(),
		"INSERT INTO notes (content) VALUES ($1) RETURNING id, created_at",
		note.Content,
	).Scan(&note.ID, &note.CreatedAt)
	if err != nil {
		http.Error(w, "Failed to create note: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(note); err != nil {
		log.Printf("failed to encode note response: %v", err)
	}
}

func getNoteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	var note Note
	err = db.QueryRowContext(r.Context(),
		"SELECT id, content, created_at FROM notes WHERE id = $1", id,
	).Scan(&note.ID, &note.Content, &note.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Note not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Failed to query note: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(note); err != nil {
		log.Printf("failed to encode note response: %v", err)
	}
}

func deleteNoteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	result, err := db.ExecContext(r.Context(), "DELETE FROM notes WHERE id = $1", id)
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

func uiHandler(w http.ResponseWriter, _ *http.Request) {
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
        function escapeHtml(str) {
            const div = document.createElement('div');
            div.textContent = str;
            return div.innerHTML;
        }

        async function loadNotes() {
            const response = await fetch('/notes');
            const notes = await response.json();
            const notesDiv = document.getElementById('notes');
            notesDiv.innerHTML = notes.map(note =>
                '<div class="note">' +
                '<strong>Note #' + note.id + '</strong><br>' +
                escapeHtml(note.content) + '<br>' +
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
	t.Execute(w, VersionInfo{Version: version}) //nolint:errcheck // best-effort cleanup/response write
}

func ordConfigHandler(w http.ResponseWriter, _ *http.Request) {
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
	if err := json.NewEncoder(w).Encode(config); err != nil {
		log.Printf("failed to encode ORD config response: %v", err)
	}
}

func ordDocumentHandler(w http.ResponseWriter, _ *http.Request) {
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
	if err := json.NewEncoder(w).Encode(document); err != nil {
		log.Printf("failed to encode ORD document response: %v", err)
	}
}
