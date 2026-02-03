package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTestStore creates a temporary store for testing.
func setupTestStore(t *testing.T) (*Store, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "calvault-store-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	dbPath := filepath.Join(dir, "test.db")
	s, err := Open(dbPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("open store: %v", err)
	}

	if err := s.InitSchema(); err != nil {
		_ = s.Close()
		_ = os.RemoveAll(dir)
		t.Fatalf("init schema: %v", err)
	}

	return s, func() {
		_ = s.Close()
		_ = os.RemoveAll(dir)
	}
}

func TestStore_SourceRoundtrip(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	// Create source
	src, err := s.GetOrCreateSource("test@example.com")
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if src.Identifier != "test@example.com" {
		t.Errorf("identifier = %q, want test@example.com", src.Identifier)
	}

	// Get same source again (should not create duplicate)
	src2, err := s.GetOrCreateSource("test@example.com")
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if src2.ID != src.ID {
		t.Errorf("expected same ID, got %d and %d", src.ID, src2.ID)
	}

	// List sources
	sources, err := s.ListSources()
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(sources))
	}
}

func TestStore_CalendarUpsert(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	src, _ := s.GetOrCreateSource("test@example.com")

	cal := &Calendar{
		GoogleCalendarID: "primary",
		Summary:          "My Calendar",
		Timezone:         "America/New_York",
		IsPrimary:        true,
	}

	// Insert
	id1, err := s.UpsertCalendar(src.ID, cal)
	if err != nil {
		t.Fatalf("upsert calendar: %v", err)
	}

	// Update (upsert same calendar)
	cal.Summary = "Updated Name"
	id2, err := s.UpsertCalendar(src.ID, cal)
	if err != nil {
		t.Fatalf("upsert calendar again: %v", err)
	}
	if id1 != id2 {
		t.Errorf("upsert should return same ID, got %d and %d", id1, id2)
	}

	// Verify update
	cals, _ := s.GetCalendars(src.ID)
	if len(cals) != 1 {
		t.Fatalf("expected 1 calendar, got %d", len(cals))
	}
	if cals[0].Summary != "Updated Name" {
		t.Errorf("summary = %q, want Updated Name", cals[0].Summary)
	}
}

func TestStore_EventUpsertAndDelete(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	src, _ := s.GetOrCreateSource("test@example.com")
	calID, _ := s.UpsertCalendar(src.ID, &Calendar{
		GoogleCalendarID: "primary",
		Summary:          "Test Cal",
	})

	now := time.Now().Truncate(time.Second)
	event := &Event{
		SourceID:      src.ID,
		CalendarID:    calID,
		GoogleEventID: "evt123",
		Summary:       "Meeting",
		Location:      "Room A",
		StartTime:     sql.NullTime{Time: now, Valid: true},
		EndTime:       sql.NullTime{Time: now.Add(time.Hour), Valid: true},
		Status:        "confirmed",
	}

	// Insert
	eventID, err := s.UpsertEvent(event)
	if err != nil {
		t.Fatalf("upsert event: %v", err)
	}
	if eventID == 0 {
		t.Error("expected non-zero event ID")
	}

	// Verify count
	count, _ := s.GetEventCount(src.ID)
	if count != 1 {
		t.Errorf("event count = %d, want 1", count)
	}

	// Update via upsert
	event.Summary = "Updated Meeting"
	eventID2, err := s.UpsertEvent(event)
	if err != nil {
		t.Fatalf("upsert event again: %v", err)
	}
	if eventID != eventID2 {
		t.Errorf("upsert should return same ID, got %d and %d", eventID, eventID2)
	}

	// Count should still be 1
	count, _ = s.GetEventCount(src.ID)
	if count != 1 {
		t.Errorf("event count after update = %d, want 1", count)
	}

	// Delete
	if err := s.DeleteEvent(src.ID, "evt123"); err != nil {
		t.Fatalf("delete event: %v", err)
	}
	count, _ = s.GetEventCount(src.ID)
	if count != 0 {
		t.Errorf("event count after delete = %d, want 0", count)
	}
}

func TestStore_Attendees(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	src, _ := s.GetOrCreateSource("test@example.com")
	calID, _ := s.UpsertCalendar(src.ID, &Calendar{
		GoogleCalendarID: "primary",
		Summary:          "Test Cal",
	})

	eventID, _ := s.UpsertEvent(&Event{
		SourceID:      src.ID,
		CalendarID:    calID,
		GoogleEventID: "evt456",
		Summary:       "Team Sync",
	})

	attendees := []*Attendee{
		{Email: "alice@example.com", DisplayName: "Alice", ResponseStatus: "accepted"},
		{Email: "bob@example.com", DisplayName: "Bob", ResponseStatus: "tentative"},
	}

	// Replace attendees
	if err := s.ReplaceAttendees(eventID, attendees); err != nil {
		t.Fatalf("replace attendees: %v", err)
	}

	// Verify via raw query
	var count int
	_ = s.DB().QueryRow("SELECT COUNT(*) FROM attendees WHERE event_id = ?", eventID).Scan(&count)
	if count != 2 {
		t.Errorf("attendee count = %d, want 2", count)
	}

	// Replace with new list
	newAttendees := []*Attendee{
		{Email: "charlie@example.com", DisplayName: "Charlie", ResponseStatus: "accepted"},
	}
	if err := s.ReplaceAttendees(eventID, newAttendees); err != nil {
		t.Fatalf("replace attendees again: %v", err)
	}

	_ = s.DB().QueryRow("SELECT COUNT(*) FROM attendees WHERE event_id = ?", eventID).Scan(&count)
	if count != 1 {
		t.Errorf("attendee count after replace = %d, want 1", count)
	}
}

func TestStore_SyncToken(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	src, _ := s.GetOrCreateSource("test@example.com")
	calID, _ := s.UpsertCalendar(src.ID, &Calendar{
		GoogleCalendarID: "primary",
		Summary:          "Test Cal",
	})

	// Set sync token
	if err := s.UpdateCalendarSyncToken(calID, "token123"); err != nil {
		t.Fatalf("update sync token: %v", err)
	}

	// Verify
	cals, _ := s.GetCalendars(src.ID)
	if !cals[0].SyncToken.Valid || cals[0].SyncToken.String != "token123" {
		t.Errorf("sync token = %v, want token123", cals[0].SyncToken)
	}

	// Clear sync token
	if err := s.ClearCalendarSyncToken(calID); err != nil {
		t.Fatalf("clear sync token: %v", err)
	}

	cals, _ = s.GetCalendars(src.ID)
	if cals[0].SyncToken.Valid {
		t.Error("expected sync token to be cleared")
	}
}

func TestStore_Stats(t *testing.T) {
	s, cleanup := setupTestStore(t)
	defer cleanup()

	// Empty stats
	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.AccountCount != 0 || stats.EventCount != 0 {
		t.Errorf("expected empty stats, got accounts=%d events=%d", stats.AccountCount, stats.EventCount)
	}

	// Add some data
	src, _ := s.GetOrCreateSource("test@example.com")
	calID, _ := s.UpsertCalendar(src.ID, &Calendar{
		GoogleCalendarID: "primary",
		Summary:          "Test",
	})
	_, _ = s.UpsertEvent(&Event{
		SourceID:      src.ID,
		CalendarID:    calID,
		GoogleEventID: "evt1",
		Summary:       "Event 1",
		Location:      "Office",
		StartTime:     sql.NullTime{Time: time.Now(), Valid: true},
	})
	_, _ = s.UpsertEvent(&Event{
		SourceID:      src.ID,
		CalendarID:    calID,
		GoogleEventID: "evt2",
		Summary:       "Event 2",
		Location:      "Home",
		StartTime:     sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true},
	})

	stats, _ = s.GetStats()
	if stats.AccountCount != 1 {
		t.Errorf("account count = %d, want 1", stats.AccountCount)
	}
	if stats.CalendarCount != 1 {
		t.Errorf("calendar count = %d, want 1", stats.CalendarCount)
	}
	if stats.EventCount != 2 {
		t.Errorf("event count = %d, want 2", stats.EventCount)
	}
	if stats.UniqueLocations != 2 {
		t.Errorf("unique locations = %d, want 2", stats.UniqueLocations)
	}
}
