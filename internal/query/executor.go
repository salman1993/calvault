// Package query provides safe SQL query execution for LLMs.
package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Executor executes read-only SQL queries.
type Executor struct {
	db *sql.DB
}

// QueryResult holds the result of a query.
type QueryResult struct {
	Columns  []string        `json:"columns"`
	Rows     [][]interface{} `json:"rows"`
	RowCount int             `json:"row_count"`
}

// NewExecutor creates a new query executor with read-only access.
func NewExecutor(dbPath string) (*Executor, error) {
	// Open in read-only mode
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Executor{db: db}, nil
}

// Close closes the database connection.
func (e *Executor) Close() error {
	return e.db.Close()
}

// Execute runs a read-only SQL query with a timeout.
func (e *Executor) Execute(ctx context.Context, query string) (*QueryResult, error) {
	// Strip SQL comments and whitespace for validation
	normalized := stripSQLComments(query)
	normalizedUpper := strings.ToUpper(normalized)
	if !strings.HasPrefix(normalizedUpper, "SELECT") {
		return nil, fmt.Errorf("only SELECT queries allowed")
	}

	// Reject dangerous patterns even in SELECT
	lower := strings.ToLower(query)
	dangerousPatterns := []string{
		"into ",     // SELECT INTO
		"attach ",   // ATTACH DATABASE
		"detach ",   // DETACH DATABASE
		"pragma ",   // PRAGMA commands
		"load_extension", // Load extension
	}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lower, pattern) {
			return nil, fmt.Errorf("query contains forbidden pattern: %s", pattern)
		}
	}

	// Add timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := e.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get columns: %w", err)
	}

	// Scan all rows
	var results [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		ptrs := make([]interface{}, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		// Convert values to JSON-friendly types
		row := make([]interface{}, len(values))
		for i, v := range values {
			switch val := v.(type) {
			case []byte:
				row[i] = string(val)
			case time.Time:
				row[i] = val.Format(time.RFC3339)
			default:
				row[i] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return &QueryResult{
		Columns:  columns,
		Rows:     results,
		RowCount: len(results),
	}, nil
}

// stripSQLComments removes SQL comments and leading whitespace for validation.
func stripSQLComments(query string) string {
	lines := strings.Split(query, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip comment-only lines
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		// Remove inline comments
		if idx := strings.Index(trimmed, "--"); idx > 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return strings.TrimSpace(strings.Join(result, " "))
}
