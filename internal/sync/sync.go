// Package sync provides calendar synchronization logic.
package sync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/salman1993/calvault/internal/calendar"
	"github.com/salman1993/calvault/internal/store"
	gcalendar "google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
)

// ErrSyncTokenExpired indicates the sync token is no longer valid.
var ErrSyncTokenExpired = errors.New("sync token expired (410 Gone)")

// Progress reports sync progress.
type Progress interface {
	OnCalendarStart(calendarName string)
	OnCalendarDone(calendarName string, added, updated, deleted int)
	OnEvent(eventSummary string)
}

// Summary contains sync run statistics.
type Summary struct {
	CalendarsSynced int
	EventsAdded     int
	EventsUpdated   int
	EventsDeleted   int
	Duration        time.Duration
}

// Options configures sync behavior.
type Options struct {
	Incremental bool
}

// Syncer orchestrates calendar synchronization.
type Syncer struct {
	client   *calendar.Client
	store    *store.Store
	logger   *slog.Logger
	progress Progress
}

// New creates a new syncer.
func New(client *calendar.Client, store *store.Store) *Syncer {
	return &Syncer{
		client: client,
		store:  store,
		logger: slog.Default(),
	}
}

// WithLogger sets the logger.
func (s *Syncer) WithLogger(logger *slog.Logger) *Syncer {
	s.logger = logger
	return s
}

// WithProgress sets the progress reporter.
func (s *Syncer) WithProgress(p Progress) *Syncer {
	s.progress = p
	return s
}

// SyncAccount syncs all calendars for an account.
func (s *Syncer) SyncAccount(ctx context.Context, email string, opts Options) (*Summary, error) {
	startTime := time.Now()
	summary := &Summary{}

	// Get or create source
	source, err := s.store.GetOrCreateSource(email)
	if err != nil {
		return nil, fmt.Errorf("get source: %w", err)
	}

	// List calendars from API
	calendars, err := s.client.ListCalendars(ctx)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}

	s.logger.Info("found calendars", "count", len(calendars), "email", email)

	// Sync each calendar
	for _, cal := range calendars {
		if ctx.Err() != nil {
			break
		}

		// Store/update calendar metadata
		storeCal := &store.Calendar{
			GoogleCalendarID: cal.ID,
			Summary:          cal.Summary,
			Description:      cal.Description,
			Timezone:         cal.TimeZone,
			IsPrimary:        cal.IsPrimary,
		}

		calID, err := s.store.UpsertCalendar(source.ID, storeCal)
		if err != nil {
			s.logger.Error("failed to upsert calendar", "calendar", cal.Summary, "error", err)
			continue
		}

		// Get stored calendar with sync token
		storedCals, err := s.store.GetCalendars(source.ID)
		if err != nil {
			s.logger.Error("failed to get calendars", "error", err)
			continue
		}

		var storedCal *store.Calendar
		for _, c := range storedCals {
			if c.ID == calID {
				storedCal = c
				break
			}
		}

		if s.progress != nil {
			s.progress.OnCalendarStart(cal.Summary)
		}

		// Sync events
		var calSummary *Summary
		if opts.Incremental && storedCal.SyncToken.Valid && storedCal.SyncToken.String != "" {
			calSummary, err = s.syncCalendarIncremental(ctx, source.ID, calID, cal.ID, storedCal.SyncToken.String)
			if errors.Is(err, ErrSyncTokenExpired) {
				// Clear token and fall back to full sync
				s.logger.Info("sync token expired, falling back to full sync", "calendar", cal.Summary)
				if clearErr := s.store.ClearCalendarSyncToken(calID); clearErr != nil {
					s.logger.Error("failed to clear sync token", "error", clearErr)
				}
				calSummary, err = s.syncCalendarFull(ctx, source.ID, calID, cal.ID)
			}
		} else {
			calSummary, err = s.syncCalendarFull(ctx, source.ID, calID, cal.ID)
		}

		if err != nil {
			s.logger.Error("failed to sync calendar", "calendar", cal.Summary, "error", err)
			continue
		}

		summary.CalendarsSynced++
		summary.EventsAdded += calSummary.EventsAdded
		summary.EventsUpdated += calSummary.EventsUpdated
		summary.EventsDeleted += calSummary.EventsDeleted

		if s.progress != nil {
			s.progress.OnCalendarDone(cal.Summary, calSummary.EventsAdded, calSummary.EventsUpdated, calSummary.EventsDeleted)
		}
	}

	summary.Duration = time.Since(startTime)
	return summary, nil
}

// syncCalendarFull performs a full sync of a calendar.
func (s *Syncer) syncCalendarFull(ctx context.Context, sourceID, calID int64, googleCalID string) (*Summary, error) {
	summary := &Summary{}
	pageToken := ""

	for {
		page, err := s.client.ListEvents(ctx, googleCalID, calendar.ListEventsOptions{
			PageToken:    pageToken,
			ShowDeleted:  false,
			SingleEvents: false, // Keep recurring event structure
		})
		if err != nil {
			return summary, fmt.Errorf("list events: %w", err)
		}

		for _, event := range page.Events {
			isNew, err := s.processEvent(ctx, sourceID, calID, event)
			if err != nil {
				s.logger.Error("failed to process event", "event", event.Id, "error", err)
				continue
			}

			if isNew {
				summary.EventsAdded++
			} else {
				summary.EventsUpdated++
			}

			if s.progress != nil && event.Summary != "" {
				s.progress.OnEvent(event.Summary)
			}
		}

		pageToken = page.NextPageToken
		if pageToken == "" {
			// Save sync token for future incremental syncs
			if page.NextSyncToken != "" {
				if err := s.store.UpdateCalendarSyncToken(calID, page.NextSyncToken); err != nil {
					s.logger.Error("failed to save sync token", "error", err)
				}
			}
			break
		}
	}

	return summary, nil
}

// syncCalendarIncremental performs an incremental sync using sync token.
func (s *Syncer) syncCalendarIncremental(ctx context.Context, sourceID, calID int64, googleCalID, syncToken string) (*Summary, error) {
	summary := &Summary{}
	pageToken := ""
	currentSyncToken := syncToken

	for {
		opts := calendar.ListEventsOptions{
			PageToken:   pageToken,
			ShowDeleted: true, // Need to see deleted events
		}
		if pageToken == "" {
			opts.SyncToken = currentSyncToken
		}

		page, err := s.client.ListEvents(ctx, googleCalID, opts)
		if err != nil {
			// Check for 410 Gone (sync token expired)
			var apiErr *googleapi.Error
			if errors.As(err, &apiErr) && apiErr.Code == 410 {
				return nil, ErrSyncTokenExpired
			}
			return summary, fmt.Errorf("list events: %w", err)
		}

		for _, event := range page.Events {
			// Handle deleted events
			if event.Status == "cancelled" {
				if err := s.store.DeleteEvent(sourceID, event.Id); err != nil {
					s.logger.Error("failed to delete event", "event", event.Id, "error", err)
				} else {
					summary.EventsDeleted++
				}
				continue
			}

			isNew, err := s.processEvent(ctx, sourceID, calID, event)
			if err != nil {
				s.logger.Error("failed to process event", "event", event.Id, "error", err)
				continue
			}

			if isNew {
				summary.EventsAdded++
			} else {
				summary.EventsUpdated++
			}

			if s.progress != nil && event.Summary != "" {
				s.progress.OnEvent(event.Summary)
			}
		}

		pageToken = page.NextPageToken
		if pageToken == "" {
			// Save new sync token
			if page.NextSyncToken != "" {
				if err := s.store.UpdateCalendarSyncToken(calID, page.NextSyncToken); err != nil {
					s.logger.Error("failed to save sync token", "error", err)
				}
			}
			break
		}
	}

	return summary, nil
}

// processEvent converts and stores a Google Calendar event.
func (s *Syncer) processEvent(_ context.Context, sourceID, calID int64, ge *gcalendar.Event) (bool, error) {
	event := &store.Event{
		SourceID:      sourceID,
		CalendarID:    calID,
		GoogleEventID: ge.Id,
		Summary:       ge.Summary,
		Description:   ge.Description,
		Location:      ge.Location,
		Status:        ge.Status,
		Visibility:    ge.Visibility,
	}

	// Parse start time
	if ge.Start != nil {
		if ge.Start.DateTime != "" {
			t, err := time.Parse(time.RFC3339, ge.Start.DateTime)
			if err == nil {
				event.StartTime = sql.NullTime{Time: t, Valid: true}
			}
		} else if ge.Start.Date != "" {
			t, err := time.Parse("2006-01-02", ge.Start.Date)
			if err == nil {
				event.StartTime = sql.NullTime{Time: t, Valid: true}
				event.AllDay = true
			}
		}
		event.OriginalTimezone = ge.Start.TimeZone
	}

	// Parse end time
	if ge.End != nil {
		if ge.End.DateTime != "" {
			t, err := time.Parse(time.RFC3339, ge.End.DateTime)
			if err == nil {
				event.EndTime = sql.NullTime{Time: t, Valid: true}
			}
		} else if ge.End.Date != "" {
			t, err := time.Parse("2006-01-02", ge.End.Date)
			if err == nil {
				event.EndTime = sql.NullTime{Time: t, Valid: true}
			}
		}
	}

	// Organizer
	if ge.Organizer != nil {
		event.OrganizerEmail = ge.Organizer.Email
		event.OrganizerName = ge.Organizer.DisplayName
	}

	// Creator
	if ge.Creator != nil {
		event.CreatorEmail = ge.Creator.Email
	}

	// Recurrence
	event.RecurringEventID = ge.RecurringEventId
	if len(ge.Recurrence) > 0 {
		event.RecurrenceRule = strings.Join(ge.Recurrence, "\n")
	}

	// Timestamps
	if ge.Created != "" {
		t, err := time.Parse(time.RFC3339, ge.Created)
		if err == nil {
			event.CreatedAt = sql.NullTime{Time: t, Valid: true}
		}
	}
	if ge.Updated != "" {
		t, err := time.Parse(time.RFC3339, ge.Updated)
		if err == nil {
			event.UpdatedAt = sql.NullTime{Time: t, Valid: true}
		}
	}

	// Check if event exists (to determine if it's new)
	var existingID int64
	err := s.store.DB().QueryRow(
		`SELECT id FROM events WHERE source_id = ? AND google_event_id = ?`,
		sourceID, ge.Id,
	).Scan(&existingID)
	isNew := err == sql.ErrNoRows

	// Upsert event
	eventID, err := s.store.UpsertEvent(event)
	if err != nil {
		return false, fmt.Errorf("upsert event: %w", err)
	}

	// Store attendees
	var attendees []*store.Attendee
	for _, a := range ge.Attendees {
		attendees = append(attendees, &store.Attendee{
			Email:          a.Email,
			DisplayName:    a.DisplayName,
			ResponseStatus: a.ResponseStatus,
			IsOrganizer:    a.Organizer,
			IsSelf:         a.Self,
		})
	}
	if len(attendees) > 0 {
		if err := s.store.ReplaceAttendees(eventID, attendees); err != nil {
			s.logger.Warn("failed to store attendees", "event", ge.Id, "error", err)
		}
	}

	return isNew, nil
}
