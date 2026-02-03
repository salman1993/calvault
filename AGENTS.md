# AGENTS.md

## Project Overview

calvault is an offline Google Calendar archive tool that exports and stores calendar event data locally with full queryability. The goal is to archive years of calendar data, make it queryable via SQL (including by LLMs), and answer questions like "how often did I visit my dermatologist in the last 12 months?"

## Architecture (Go)

Single-binary Go application:

```
calvault/
├── cmd/calvault/            # CLI entrypoint
│   └── cmd/                 # Cobra commands
├── internal/                # Core packages
│   ├── calendar/            # Google Calendar API client
│   ├── oauth/               # OAuth2 flows (browser + device)
│   ├── store/               # SQLite database access
│   ├── sync/                # Sync orchestration
│   └── query/               # SQL query execution for LLMs
│
├── go.mod                   # Go module
└── Makefile                 # Build targets
```

## Quick Commands

```bash
# Build
make build                    # Debug build
make build-release            # Release build
make install                  # Install to ~/.local/bin
make test                     # Run tests
make lint                     # Run linter

# CLI usage
./calvault init-db                                    # Initialize database
./calvault add-account you@gmail.com                  # Browser OAuth
./calvault add-account you@gmail.com --headless       # Device flow
./calvault sync you@gmail.com                         # Full sync
./calvault sync you@gmail.com --incremental           # Incremental sync
./calvault query "SELECT * FROM events WHERE ..."     # Run SQL query
./calvault stats                                      # Show archive stats
```

## Key Files

### CLI (`cmd/calvault/cmd/`)
- `root.go` - Cobra root command, config loading
- `sync.go` - Sync command (full + incremental)
- `query.go` - SQL query command for LLM interaction
- `stats.go` - Archive statistics

### Core (`internal/`)
- `calendar/client.go` - Google Calendar API client with rate limiting
- `oauth/oauth.go` - OAuth2 flows (browser + device)
- `store/store.go` - SQLite database operations
- `store/schema.sql` - Database schema
- `sync/sync.go` - Sync orchestration
- `query/executor.go` - Safe SQL query execution

## Database Schema

Core tables:
- `sources` - Google accounts with sync_token for incremental sync
- `calendars` - Calendar metadata (id, summary, timezone)
- `events` - Event data (see schema below)
- `attendees` - Event attendees (many-to-many)
- `sync_runs` - Sync history for debugging

### Events Table
```sql
CREATE TABLE events (
    id INTEGER PRIMARY KEY,
    source_id INTEGER NOT NULL REFERENCES sources(id),
    calendar_id INTEGER NOT NULL REFERENCES calendars(id),
    google_event_id TEXT NOT NULL,
    
    -- Core fields
    summary TEXT,
    description TEXT,
    location TEXT,
    
    -- Timing
    start_time DATETIME,
    end_time DATETIME,
    all_day BOOLEAN DEFAULT FALSE,
    timezone TEXT,
    
    -- Recurrence
    recurring_event_id TEXT,
    recurrence_rule TEXT,
    
    -- Metadata
    status TEXT,  -- confirmed, tentative, cancelled
    created_at DATETIME,
    updated_at DATETIME,
    
    -- Organizer
    organizer_email TEXT,
    organizer_name TEXT,
    
    UNIQUE(source_id, google_event_id)
);
CREATE INDEX idx_events_start ON events(start_time);
CREATE INDEX idx_events_summary ON events(summary);
```

## OAuth Scopes

```go
var Scopes = []string{
    "https://www.googleapis.com/auth/calendar.readonly",
}
```

## Sync Strategy

### Full Sync
1. List all calendars for the account
2. For each calendar, paginate through `Events.list()` 
3. Store events, attendees, handle recurring events
4. Save `syncToken` from final response

### Incremental Sync
1. Load stored `syncToken` for each calendar
2. Call `Events.list(syncToken=...)` to get only changes
3. Handle `410 Gone` → fall back to full sync
4. Update/delete events as indicated by API

## Query Interface for LLMs

The `query` command executes read-only SQL and returns results as JSON:

```bash
./calvault query "
  SELECT summary, start_time, location 
  FROM events 
  WHERE summary LIKE '%dermatologist%' 
    AND start_time > date('now', '-12 months')
  ORDER BY start_time DESC
"
```

Output:
```json
{
  "columns": ["summary", "start_time", "location"],
  "rows": [
    ["Dermatologist Appointment", "2025-09-15 10:00:00", "123 Medical Center"],
    ["Dermatologist Follow-up", "2025-03-22 14:30:00", "123 Medical Center"]
  ],
  "row_count": 2
}
```

### Safety
- Read-only: Only SELECT statements allowed
- Timeout: 30-second query timeout
- No writes: SQLite opened in read-only mode for queries

## Code Style & Linting

```bash
make fmt                      # Format code (go fmt)
make lint                     # Run linter (golangci-lint)
make test                     # Run tests
```

**Standards:**
- Default gofmt configuration
- Use `error` return values, wrap with context using `fmt.Errorf`
- Table-driven tests

## Code Conventions

- Cobra for CLI
- SQLite via mattn/go-sqlite3
- golang.org/x/oauth2 for OAuth
- Context-based cancellation for long operations
- Route all DB operations through `Store` struct

## Configuration

All data defaults to `~/.calvault/`:
- `~/.calvault/config.toml` - Configuration file
- `~/.calvault/calvault.db` - SQLite database
- `~/.calvault/tokens/` - OAuth tokens per account

Override with `CALVAULT_HOME` environment variable.

```toml
[oauth]
client_secrets = "/path/to/client_secret.json"

[sync]
rate_limit_qps = 10
```

## Example LLM Queries

```sql
-- How often did I visit my dermatologist?
SELECT COUNT(*) as visits, 
       strftime('%Y-%m', start_time) as month
FROM events 
WHERE summary LIKE '%dermatologist%' 
  AND start_time > date('now', '-12 months')
GROUP BY month;

-- Busiest days of the week
SELECT 
  CASE strftime('%w', start_time)
    WHEN '0' THEN 'Sunday'
    WHEN '1' THEN 'Monday'
    WHEN '2' THEN 'Tuesday'
    WHEN '3' THEN 'Wednesday'
    WHEN '4' THEN 'Thursday'
    WHEN '5' THEN 'Friday'
    WHEN '6' THEN 'Saturday'
  END as day_of_week,
  COUNT(*) as event_count
FROM events
WHERE start_time > date('now', '-6 months')
GROUP BY strftime('%w', start_time)
ORDER BY event_count DESC;

-- Time spent in meetings by organizer
SELECT organizer_email,
       COUNT(*) as meetings,
       SUM((julianday(end_time) - julianday(start_time)) * 24) as total_hours
FROM events
WHERE start_time > date('now', '-3 months')
  AND organizer_email IS NOT NULL
GROUP BY organizer_email
ORDER BY total_hours DESC
LIMIT 10;

-- Recurring events I attend
SELECT summary, recurrence_rule, COUNT(*) as occurrences
FROM events
WHERE recurring_event_id IS NOT NULL
  AND start_time > date('now', '-6 months')
GROUP BY recurring_event_id
ORDER BY occurrences DESC;
```
