// Package calendar provides a Google Calendar API client with rate limiting.
package calendar

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/oauth2"
	gcalendar "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Client wraps the Google Calendar API with rate limiting.
type Client struct {
	service     *gcalendar.Service
	rateLimiter *RateLimiter
	logger      *slog.Logger
}

// RateLimiter implements a simple token bucket rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	qps      float64
	tokens   float64
	lastTime time.Time
}

// NewRateLimiter creates a rate limiter with the specified QPS.
func NewRateLimiter(qps float64) *RateLimiter {
	return &RateLimiter{
		qps:      qps,
		tokens:   qps, // Start with a full bucket
		lastTime: time.Now(),
	}
}

// Wait blocks until a token is available.
func (r *RateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(r.lastTime).Seconds()
	r.tokens += elapsed * r.qps
	if r.tokens > r.qps {
		r.tokens = r.qps
	}
	r.lastTime = now

	// If we have a token, use it
	if r.tokens >= 1.0 {
		r.tokens -= 1.0
		return nil
	}

	// Wait for a token
	waitTime := time.Duration((1.0-r.tokens)/r.qps*1000) * time.Millisecond
	r.mu.Unlock()

	select {
	case <-time.After(waitTime):
	case <-ctx.Done():
		r.mu.Lock()
		return ctx.Err()
	}

	r.mu.Lock()
	r.tokens = 0
	r.lastTime = time.Now()
	return nil
}

// ClientOption configures the client.
type ClientOption func(*Client)

// WithLogger sets the logger.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger
	}
}

// WithRateLimiter sets a custom rate limiter.
func WithRateLimiter(rl *RateLimiter) ClientOption {
	return func(c *Client) {
		c.rateLimiter = rl
	}
}

// NewClient creates a new Calendar API client.
func NewClient(ctx context.Context, tokenSource oauth2.TokenSource, opts ...ClientOption) (*Client, error) {
	httpClient := oauth2.NewClient(ctx, tokenSource)
	service, err := gcalendar.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("create calendar service: %w", err)
	}

	c := &Client{
		service:     service,
		rateLimiter: NewRateLimiter(10), // Default 10 QPS
		logger:      slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// CalendarEntry represents a calendar from the calendar list.
type CalendarEntry struct {
	ID          string
	Summary     string
	Description string
	TimeZone    string
	IsPrimary   bool
}

// ListCalendars returns all calendars for the authenticated user.
func (c *Client) ListCalendars(ctx context.Context) ([]*CalendarEntry, error) {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	var calendars []*CalendarEntry
	pageToken := ""

	for {
		call := c.service.CalendarList.List().MaxResults(250)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		list, err := call.Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("list calendars: %w", err)
		}

		for _, entry := range list.Items {
			calendars = append(calendars, &CalendarEntry{
				ID:          entry.Id,
				Summary:     entry.Summary,
				Description: entry.Description,
				TimeZone:    entry.TimeZone,
				IsPrimary:   entry.Primary,
			})
		}

		pageToken = list.NextPageToken
		if pageToken == "" {
			break
		}

		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, err
		}
	}

	return calendars, nil
}

// EventsPage represents a page of events.
type EventsPage struct {
	Events        []*gcalendar.Event
	NextPageToken string
	NextSyncToken string
}

// ListEventsOptions configures event listing.
type ListEventsOptions struct {
	PageToken     string
	SyncToken     string
	ShowDeleted   bool
	SingleEvents  bool
	MaxResults    int64
	TimeMin       time.Time
	TimeMax       time.Time
}

// ListEvents lists events from a calendar.
func (c *Client) ListEvents(ctx context.Context, calendarID string, opts ListEventsOptions) (*EventsPage, error) {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	call := c.service.Events.List(calendarID).
		ShowDeleted(opts.ShowDeleted).
		SingleEvents(opts.SingleEvents)

	if opts.MaxResults > 0 {
		call = call.MaxResults(opts.MaxResults)
	} else {
		call = call.MaxResults(2500)
	}

	if opts.PageToken != "" {
		call = call.PageToken(opts.PageToken)
	}

	if opts.SyncToken != "" {
		call = call.SyncToken(opts.SyncToken)
	}

	if !opts.TimeMin.IsZero() {
		call = call.TimeMin(opts.TimeMin.Format(time.RFC3339))
	}

	if !opts.TimeMax.IsZero() {
		call = call.TimeMax(opts.TimeMax.Format(time.RFC3339))
	}

	events, err := call.Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	return &EventsPage{
		Events:        events.Items,
		NextPageToken: events.NextPageToken,
		NextSyncToken: events.NextSyncToken,
	}, nil
}

// ListEventsIncremental fetches only changed events using a sync token.
func (c *Client) ListEventsIncremental(ctx context.Context, calendarID, syncToken string) (*EventsPage, error) {
	return c.ListEvents(ctx, calendarID, ListEventsOptions{
		SyncToken:   syncToken,
		ShowDeleted: true, // Important: need to see deleted events
	})
}
