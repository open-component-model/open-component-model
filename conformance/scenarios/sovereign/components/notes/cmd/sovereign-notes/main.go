// Package main implements the sovereign-notes v1.1.0 web service.
// This version uses incremental migrations to evolve the schema
// created by v1.0.0 (cmd/sovereign-notes-v1).
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

// Note represents a note in the system
type Note struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
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
	version = "1.1.0" // Set via build flags
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

	// Test database connection
	if err := db.PingContext(context.Background()); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	// Run database migrations
	if err := runMigrations(); err != nil {
		log.Fatal("Failed to run database migrations:", err)
	}

	defer db.Close()

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

// migration represents a database schema migration.
type migration struct {
	id  int
	sql string
}

// migrations is the ordered list of schema migrations.
// Each migration is idempotent and safe to re-run.
var migrations = []migration{
	{
		id: 1,
		sql: `CREATE TABLE IF NOT EXISTS notes (
			id SERIAL PRIMARY KEY,
			content TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)`,
	},
	{
		id:  2,
		sql: `ALTER TABLE notes ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT ''`,
	},
}

// runMigrations applies all pending migrations using an advisory lock
// to prevent race conditions when multiple replicas start simultaneously.
func runMigrations() error {
	ctx := context.Background()

	// Acquire advisory lock to prevent concurrent migrations
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock(1)"); err != nil {
		return fmt.Errorf("failed to acquire advisory lock: %w", err)
	}
	defer db.ExecContext(ctx, "SELECT pg_advisory_unlock(1)") //nolint:errcheck // best-effort cleanup/response write

	// Create migrations tracking table
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	for _, m := range migrations {
		// Check if migration has already been applied
		var exists bool
		if err := db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id = $1)", m.id,
		).Scan(&exists); err != nil {
			return fmt.Errorf("failed to check migration %d: %w", m.id, err)
		}

		if exists {
			log.Printf("Migration %d already applied, skipping", m.id)
			continue
		}

		log.Printf("Applying migration %d...", m.id)
		if _, err := db.ExecContext(ctx, m.sql); err != nil {
			return fmt.Errorf("failed to apply migration %d: %w", m.id, err)
		}

		if _, err := db.ExecContext(ctx,
			"INSERT INTO schema_migrations (id) VALUES ($1)", m.id,
		); err != nil {
			return fmt.Errorf("failed to record migration %d: %w", m.id, err)
		}

		log.Printf("Migration %d applied successfully", m.id)
	}

	return nil
}

// Health check endpoint
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK")) //nolint:errcheck // best-effort cleanup/response write
}

// Readiness check endpoint (includes database connectivity)
func readinessHandler(w http.ResponseWriter, r *http.Request) {
	if err := db.PingContext(r.Context()); err != nil {
		http.Error(w, "Database not ready: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready")) //nolint:errcheck // best-effort cleanup/response write
}

// Version endpoint
func versionHandler(w http.ResponseWriter, _ *http.Request) {
	versionInfo := VersionInfo{
		Version: version,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(versionInfo); err != nil {
		log.Printf("failed to encode version response: %v", err)
	}
}

// List all notes
func listNotesHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.QueryContext(r.Context(), "SELECT id, title, content, created_at FROM notes ORDER BY created_at DESC")
	if err != nil {
		http.Error(w, "Failed to query notes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt); err != nil {
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

	err := db.QueryRowContext(r.Context(),
		"INSERT INTO notes (title, content) VALUES ($1, $2) RETURNING id, created_at",
		note.Title, note.Content,
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

// Get a specific note
func getNoteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		http.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	var note Note
	err = db.QueryRowContext(r.Context(),
		"SELECT id, title, content, created_at FROM notes WHERE id = $1",
		id,
	).Scan(&note.ID, &note.Title, &note.Content, &note.CreatedAt)

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

// Delete a note
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

// Simple HTML UI
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
        <input type="text" id="titleInput" placeholder="Title (optional)" />
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
                '<strong>' + escapeHtml(note.title || 'Note #' + note.id) + '</strong><br>' +
                escapeHtml(note.content) + '<br>' +
                '<small>' + new Date(note.created_at).toLocaleString() + '</small>' +
                '</div>'
            ).join('');
        }

        async function addNote() {
            const title = document.getElementById('titleInput').value;
            const content = document.getElementById('noteInput').value;
            if (!content) return;

            await fetch('/notes', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ title, content })
            });

            document.getElementById('titleInput').value = '';
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

// ORD configuration endpoint
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

// ORD document endpoint
func ordDocumentHandler(w http.ResponseWriter, _ *http.Request) {
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
	if err := json.NewEncoder(w).Encode(document); err != nil {
		log.Printf("failed to encode ORD document response: %v", err)
	}
}
