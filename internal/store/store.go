// Package store provides SQLite database access for calendar data.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schema string

// Store provides database operations for calendar data.
type Store struct {
	db *sql.DB
}

// Source represents a Google account.
type Source struct {
	ID         int64
	SourceType string
	Identifier string // email address
	CreatedAt  time.Time
}

// Calendar represents a Google Calendar.
type Calendar struct {
	ID               int64
	SourceID         int64
	GoogleCalendarID string
	Summary          string
	Description      string
	Timezone         string
	IsPrimary        bool
	SyncToken        sql.NullString
	LastSyncedAt     sql.NullTime
}

// Event represents a calendar event.
type Event struct {
	ID                int64
	SourceID          int64
	CalendarID        int64
	GoogleEventID     string
	Summary           string
	Description       string
	Location          string
	StartTime         sql.NullTime
	EndTime           sql.NullTime
	AllDay            bool
	OriginalTimezone  string
	RecurringEventID  string
	RecurrenceRule    string
	Status            string
	Visibility        string
	OrganizerEmail    string
	OrganizerName     string
	CreatorEmail      string
	CreatedAt         sql.NullTime
	UpdatedAt         sql.NullTime
	SyncedAt          time.Time
}

// Attendee represents an event attendee.
type Attendee struct {
	ID             int64
	EventID        int64
	Email          string
	DisplayName    string
	ResponseStatus string
	IsOrganizer    bool
	IsSelf         bool
}

// SyncStats holds statistics from a sync run.
type SyncStats struct {
	EventsAdded   int
	EventsUpdated int
	EventsDeleted int
}

// Stats holds overall database statistics.
type Stats struct {
	AccountCount    int
	CalendarCount   int
	EventCount      int
	EarliestEvent   time.Time
	LatestEvent     time.Time
	UniqueLocations int
	RecurringCount  int
}

// Open opens or creates the SQLite database at the given path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection.
func (s *Store) DB() *sql.DB {
	return s.db
}

// InitSchema creates the database tables if they don't exist.
func (s *Store) InitSchema() error {
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

// GetOrCreateSource returns an existing source or creates a new one.
func (s *Store) GetOrCreateSource(email string) (*Source, error) {
	// Try to get existing source
	source, err := s.GetSourceByIdentifier(email)
	if err != nil {
		return nil, err
	}
	if source != nil {
		return source, nil
	}

	// Create new source
	result, err := s.db.Exec(
		`INSERT INTO sources (source_type, identifier) VALUES ('google', ?)`,
		email,
	)
	if err != nil {
		return nil, fmt.Errorf("insert source: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get last insert id: %w", err)
	}

	return &Source{
		ID:         id,
		SourceType: "google",
		Identifier: email,
		CreatedAt:  time.Now(),
	}, nil
}

// GetSourceByIdentifier returns a source by email address.
func (s *Store) GetSourceByIdentifier(email string) (*Source, error) {
	row := s.db.QueryRow(
		`SELECT id, source_type, identifier, created_at FROM sources WHERE identifier = ?`,
		email,
	)

	var source Source
	err := row.Scan(&source.ID, &source.SourceType, &source.Identifier, &source.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan source: %w", err)
	}

	return &source, nil
}

// ListSources returns all sources.
func (s *Store) ListSources() ([]*Source, error) {
	rows, err := s.db.Query(
		`SELECT id, source_type, identifier, created_at FROM sources ORDER BY identifier`,
	)
	if err != nil {
		return nil, fmt.Errorf("query sources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sources []*Source
	for rows.Next() {
		var source Source
		if err := rows.Scan(&source.ID, &source.SourceType, &source.Identifier, &source.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		sources = append(sources, &source)
	}

	return sources, rows.Err()
}

// UpsertCalendar inserts or updates a calendar.
func (s *Store) UpsertCalendar(sourceID int64, cal *Calendar) (int64, error) {
	result, err := s.db.Exec(`
		INSERT INTO calendars (source_id, google_calendar_id, summary, description, timezone, is_primary)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, google_calendar_id) DO UPDATE SET
			summary = excluded.summary,
			description = excluded.description,
			timezone = excluded.timezone,
			is_primary = excluded.is_primary
	`, sourceID, cal.GoogleCalendarID, cal.Summary, cal.Description, cal.Timezone, cal.IsPrimary)
	if err != nil {
		return 0, fmt.Errorf("upsert calendar: %w", err)
	}

	// Get the ID (either new or existing)
	var id int64
	err = s.db.QueryRow(
		`SELECT id FROM calendars WHERE source_id = ? AND google_calendar_id = ?`,
		sourceID, cal.GoogleCalendarID,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get calendar id: %w", err)
	}

	_ = result // Suppress unused variable warning
	return id, nil
}

// GetCalendars returns all calendars for a source.
func (s *Store) GetCalendars(sourceID int64) ([]*Calendar, error) {
	rows, err := s.db.Query(`
		SELECT id, source_id, google_calendar_id, summary, description, timezone, 
		       is_primary, sync_token, last_synced_at
		FROM calendars WHERE source_id = ?
		ORDER BY is_primary DESC, summary
	`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query calendars: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var calendars []*Calendar
	for rows.Next() {
		var cal Calendar
		if err := rows.Scan(
			&cal.ID, &cal.SourceID, &cal.GoogleCalendarID, &cal.Summary,
			&cal.Description, &cal.Timezone, &cal.IsPrimary, &cal.SyncToken, &cal.LastSyncedAt,
		); err != nil {
			return nil, fmt.Errorf("scan calendar: %w", err)
		}
		calendars = append(calendars, &cal)
	}

	return calendars, rows.Err()
}

// UpdateCalendarSyncToken updates the sync token for a calendar.
func (s *Store) UpdateCalendarSyncToken(calID int64, token string) error {
	_, err := s.db.Exec(
		`UPDATE calendars SET sync_token = ?, last_synced_at = ? WHERE id = ?`,
		token, time.Now(), calID,
	)
	if err != nil {
		return fmt.Errorf("update sync token: %w", err)
	}
	return nil
}

// ClearCalendarSyncToken clears the sync token for a calendar (used when 410 is received).
func (s *Store) ClearCalendarSyncToken(calID int64) error {
	_, err := s.db.Exec(
		`UPDATE calendars SET sync_token = NULL WHERE id = ?`,
		calID,
	)
	if err != nil {
		return fmt.Errorf("clear sync token: %w", err)
	}
	return nil
}

// UpsertEvent inserts or updates an event.
func (s *Store) UpsertEvent(event *Event) (int64, error) {
	result, err := s.db.Exec(`
		INSERT INTO events (
			source_id, calendar_id, google_event_id, summary, description, location,
			start_time, end_time, all_day, original_timezone,
			recurring_event_id, recurrence_rule, status, visibility,
			organizer_email, organizer_name, creator_email,
			created_at, updated_at, synced_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, google_event_id) DO UPDATE SET
			calendar_id = excluded.calendar_id,
			summary = excluded.summary,
			description = excluded.description,
			location = excluded.location,
			start_time = excluded.start_time,
			end_time = excluded.end_time,
			all_day = excluded.all_day,
			original_timezone = excluded.original_timezone,
			recurring_event_id = excluded.recurring_event_id,
			recurrence_rule = excluded.recurrence_rule,
			status = excluded.status,
			visibility = excluded.visibility,
			organizer_email = excluded.organizer_email,
			organizer_name = excluded.organizer_name,
			creator_email = excluded.creator_email,
			updated_at = excluded.updated_at,
			synced_at = excluded.synced_at
	`,
		event.SourceID, event.CalendarID, event.GoogleEventID,
		event.Summary, event.Description, event.Location,
		event.StartTime, event.EndTime, event.AllDay, event.OriginalTimezone,
		event.RecurringEventID, event.RecurrenceRule, event.Status, event.Visibility,
		event.OrganizerEmail, event.OrganizerName, event.CreatorEmail,
		event.CreatedAt, event.UpdatedAt, time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("upsert event: %w", err)
	}

	// Get the ID
	var id int64
	err = s.db.QueryRow(
		`SELECT id FROM events WHERE source_id = ? AND google_event_id = ?`,
		event.SourceID, event.GoogleEventID,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get event id: %w", err)
	}

	_ = result
	return id, nil
}

// DeleteEvent deletes an event by google_event_id.
func (s *Store) DeleteEvent(sourceID int64, googleEventID string) error {
	_, err := s.db.Exec(
		`DELETE FROM events WHERE source_id = ? AND google_event_id = ?`,
		sourceID, googleEventID,
	)
	if err != nil {
		return fmt.Errorf("delete event: %w", err)
	}
	return nil
}

// GetEventCount returns the total number of events for a source.
func (s *Store) GetEventCount(sourceID int64) (int64, error) {
	var count int64
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM events WHERE source_id = ?`,
		sourceID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count events: %w", err)
	}
	return count, nil
}

// ReplaceAttendees replaces all attendees for an event.
func (s *Store) ReplaceAttendees(eventID int64, attendees []*Attendee) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete existing attendees
	if _, err := tx.Exec(`DELETE FROM attendees WHERE event_id = ?`, eventID); err != nil {
		return fmt.Errorf("delete attendees: %w", err)
	}

	// Insert new attendees
	for _, a := range attendees {
		_, err := tx.Exec(`
			INSERT INTO attendees (event_id, email, display_name, response_status, is_organizer, is_self)
			VALUES (?, ?, ?, ?, ?, ?)
		`, eventID, a.Email, a.DisplayName, a.ResponseStatus, a.IsOrganizer, a.IsSelf)
		if err != nil {
			return fmt.Errorf("insert attendee: %w", err)
		}
	}

	return tx.Commit()
}

// StartSyncRun creates a new sync run record.
func (s *Store) StartSyncRun(sourceID, calendarID int64) (int64, error) {
	var calID interface{}
	if calendarID > 0 {
		calID = calendarID
	}

	result, err := s.db.Exec(
		`INSERT INTO sync_runs (source_id, calendar_id, status) VALUES (?, ?, 'running')`,
		sourceID, calID,
	)
	if err != nil {
		return 0, fmt.Errorf("start sync run: %w", err)
	}

	return result.LastInsertId()
}

// CompleteSyncRun marks a sync run as completed.
func (s *Store) CompleteSyncRun(runID int64, stats SyncStats) error {
	_, err := s.db.Exec(`
		UPDATE sync_runs SET
			completed_at = ?,
			status = 'completed',
			events_added = ?,
			events_updated = ?,
			events_deleted = ?
		WHERE id = ?
	`, time.Now(), stats.EventsAdded, stats.EventsUpdated, stats.EventsDeleted, runID)
	if err != nil {
		return fmt.Errorf("complete sync run: %w", err)
	}
	return nil
}

// FailSyncRun marks a sync run as failed.
func (s *Store) FailSyncRun(runID int64, errMsg string) error {
	_, err := s.db.Exec(`
		UPDATE sync_runs SET
			completed_at = ?,
			status = 'failed',
			error_message = ?
		WHERE id = ?
	`, time.Now(), errMsg, runID)
	if err != nil {
		return fmt.Errorf("fail sync run: %w", err)
	}
	return nil
}

// GetStats returns overall database statistics.
func (s *Store) GetStats() (*Stats, error) {
	stats := &Stats{}

	// Account count
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sources`).Scan(&stats.AccountCount)

	// Calendar count
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM calendars`).Scan(&stats.CalendarCount)

	// Event count
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&stats.EventCount)

	// Date range
	_ = s.db.QueryRow(`SELECT MIN(start_time) FROM events WHERE start_time IS NOT NULL`).Scan(&stats.EarliestEvent)
	_ = s.db.QueryRow(`SELECT MAX(start_time) FROM events WHERE start_time IS NOT NULL`).Scan(&stats.LatestEvent)

	// Unique locations
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT location) FROM events WHERE location IS NOT NULL AND location != ''`).Scan(&stats.UniqueLocations)

	// Recurring events
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE recurring_event_id IS NOT NULL AND recurring_event_id != ''`).Scan(&stats.RecurringCount)

	return stats, nil
}
