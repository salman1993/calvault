package query

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/salman1993/calvault/internal/store"
)

// setupTestDB creates a temporary database for testing.
func setupTestDB(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "calvault-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	dbPath := filepath.Join(dir, "test.db")

	// Initialize schema
	s, err := store.Open(dbPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("open store: %v", err)
	}
	if err := s.InitSchema(); err != nil {
		_ = s.Close()
		_ = os.RemoveAll(dir)
		t.Fatalf("init schema: %v", err)
	}
	_ = s.Close()

	return dbPath, func() { _ = os.RemoveAll(dir) }
}

func TestExecutor_OnlySelectAllowed(t *testing.T) {
	dbPath, cleanup := setupTestDB(t)
	defer cleanup()

	exec, err := NewExecutor(dbPath)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	defer func() { _ = exec.Close() }()

	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"simple select", "SELECT 1", false},
		{"select from table", "SELECT * FROM events", false},
		{"select with where", "SELECT summary FROM events WHERE id = 1", false},
		{"insert blocked", "INSERT INTO events (summary) VALUES ('test')", true},
		{"update blocked", "UPDATE events SET summary = 'x'", true},
		{"delete blocked", "DELETE FROM events", true},
		{"drop blocked", "DROP TABLE events", true},
		{"create blocked", "CREATE TABLE foo (id INT)", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := exec.Execute(context.Background(), tt.query)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for query: %s", tt.query)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for query %s: %v", tt.query, err)
			}
		})
	}
}

func TestExecutor_DangerousPatternsBlocked(t *testing.T) {
	dbPath, cleanup := setupTestDB(t)
	defer cleanup()

	exec, err := NewExecutor(dbPath)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	defer func() { _ = exec.Close() }()

	tests := []struct {
		name  string
		query string
	}{
		{"select into", "SELECT * INTO OUTFILE '/tmp/x' FROM events"},
		{"attach database", "SELECT 1; ATTACH DATABASE ':memory:' AS foo"},
		{"pragma", "SELECT 1; PRAGMA table_info(events)"},
		{"load extension", "SELECT load_extension('/tmp/evil.so')"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := exec.Execute(context.Background(), tt.query)
			if err == nil {
				t.Errorf("expected dangerous pattern to be blocked: %s", tt.query)
			}
		})
	}
}

func TestExecutor_SQLCommentsStripped(t *testing.T) {
	dbPath, cleanup := setupTestDB(t)
	defer cleanup()

	exec, err := NewExecutor(dbPath)
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	defer func() { _ = exec.Close() }()

	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "comment then select",
			query:   "-- this is a comment\nSELECT 1",
			wantErr: false,
		},
		{
			name:    "multiple comments then select",
			query:   "-- comment 1\n-- comment 2\nSELECT 1",
			wantErr: false,
		},
		{
			name:    "inline comment",
			query:   "SELECT 1 -- inline comment",
			wantErr: false,
		},
		{
			name:    "comment hiding insert fails",
			query:   "-- SELECT 1\nINSERT INTO events (summary) VALUES ('x')",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := exec.Execute(context.Background(), tt.query)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for query: %s", tt.query)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestStripSQLComments(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SELECT 1", "SELECT 1"},
		{"-- comment\nSELECT 1", "SELECT 1"},
		{"SELECT 1 -- inline", "SELECT 1"},
		{"-- a\n-- b\nSELECT * FROM t", "SELECT * FROM t"},
		{"  \n  SELECT 1", "SELECT 1"},
	}

	for _, tt := range tests {
		got := stripSQLComments(tt.input)
		if got != tt.expected {
			t.Errorf("stripSQLComments(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
