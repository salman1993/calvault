-- Sources (Google accounts)
CREATE TABLE IF NOT EXISTS sources (
    id INTEGER PRIMARY KEY,
    source_type TEXT NOT NULL DEFAULT 'google',
    identifier TEXT NOT NULL UNIQUE,  -- email address
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Calendars
CREATE TABLE IF NOT EXISTS calendars (
    id INTEGER PRIMARY KEY,
    source_id INTEGER NOT NULL REFERENCES sources(id),
    google_calendar_id TEXT NOT NULL,
    summary TEXT,
    description TEXT,
    timezone TEXT,
    is_primary BOOLEAN DEFAULT FALSE,
    sync_token TEXT,  -- For incremental sync
    last_synced_at DATETIME,
    UNIQUE(source_id, google_calendar_id)
);

CREATE INDEX IF NOT EXISTS idx_calendars_source ON calendars(source_id);

-- Events
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY,
    source_id INTEGER NOT NULL REFERENCES sources(id),
    calendar_id INTEGER NOT NULL REFERENCES calendars(id),
    google_event_id TEXT NOT NULL,
    
    -- Core fields
    summary TEXT,
    description TEXT,
    location TEXT,
    
    -- Timing (stored as UTC)
    start_time DATETIME,
    end_time DATETIME,
    all_day BOOLEAN DEFAULT FALSE,
    original_timezone TEXT,
    
    -- Recurrence
    recurring_event_id TEXT,
    recurrence_rule TEXT,  -- RRULE string
    
    -- Status
    status TEXT DEFAULT 'confirmed',  -- confirmed, tentative, cancelled
    visibility TEXT,  -- default, public, private
    
    -- People
    organizer_email TEXT,
    organizer_name TEXT,
    creator_email TEXT,
    
    -- Metadata
    created_at DATETIME,
    updated_at DATETIME,
    synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    
    UNIQUE(source_id, google_event_id)
);

CREATE INDEX IF NOT EXISTS idx_events_start ON events(start_time);
CREATE INDEX IF NOT EXISTS idx_events_calendar ON events(calendar_id);
CREATE INDEX IF NOT EXISTS idx_events_recurring ON events(recurring_event_id);
CREATE INDEX IF NOT EXISTS idx_events_summary ON events(summary);

-- Attendees
CREATE TABLE IF NOT EXISTS attendees (
    id INTEGER PRIMARY KEY,
    event_id INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    display_name TEXT,
    response_status TEXT,  -- needsAction, declined, tentative, accepted
    is_organizer BOOLEAN DEFAULT FALSE,
    is_self BOOLEAN DEFAULT FALSE,
    UNIQUE(event_id, email)
);

CREATE INDEX IF NOT EXISTS idx_attendees_email ON attendees(email);
CREATE INDEX IF NOT EXISTS idx_attendees_event ON attendees(event_id);

-- Sync tracking
CREATE TABLE IF NOT EXISTS sync_runs (
    id INTEGER PRIMARY KEY,
    source_id INTEGER NOT NULL REFERENCES sources(id),
    calendar_id INTEGER REFERENCES calendars(id),
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    status TEXT DEFAULT 'running',  -- running, completed, failed
    events_added INTEGER DEFAULT 0,
    events_updated INTEGER DEFAULT 0,
    events_deleted INTEGER DEFAULT 0,
    error_message TEXT
);

CREATE INDEX IF NOT EXISTS idx_sync_runs_source ON sync_runs(source_id);
