package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/salman1993/calvault/internal/calendar"
	"github.com/salman1993/calvault/internal/oauth"
	"github.com/salman1993/calvault/internal/store"
	"github.com/salman1993/calvault/internal/sync"
	"github.com/spf13/cobra"
)

var incremental bool

var syncCmd = &cobra.Command{
	Use:   "sync [email]",
	Short: "Sync calendar events from Google",
	Long: `Synchronize calendar events from a Google account.

By default, performs a full sync of all calendars and events.
Use --incremental to only fetch changes since the last sync (faster).

If no email is specified, syncs all configured accounts.

Examples:
  calvault sync you@gmail.com              # Full sync
  calvault sync you@gmail.com --incremental # Incremental sync
  calvault sync                             # Sync all accounts`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate config
		if cfg.OAuth.ClientSecrets == "" {
			return errOAuthNotConfigured()
		}

		// Open database
		dbPath := cfg.DatabasePath()
		s, err := store.Open(dbPath)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = s.Close() }()

		if err := s.InitSchema(); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}

		// Create OAuth manager
		oauthMgr, err := oauth.NewManager(cfg.OAuth.ClientSecrets, cfg.TokensDir(), logger)
		if err != nil {
			return wrapOAuthError(fmt.Errorf("create oauth manager: %w", err))
		}

		// Determine which accounts to sync
		var emails []string
		if len(args) == 1 {
			emails = []string{args[0]}
		} else {
			sources, err := s.ListSources()
			if err != nil {
				return fmt.Errorf("list sources: %w", err)
			}
			if len(sources) == 0 {
				return fmt.Errorf("no accounts configured - run 'add-account' first")
			}
			for _, src := range sources {
				if !oauthMgr.HasToken(src.Identifier) {
					fmt.Printf("Skipping %s (no OAuth token - run 'add-account' first)\n", src.Identifier)
					continue
				}
				emails = append(emails, src.Identifier)
			}
			if len(emails) == 0 {
				return fmt.Errorf("no accounts have valid tokens - run 'add-account' first")
			}
		}

		// Set up context with cancellation
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		// Handle Ctrl+C gracefully
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigChan
			fmt.Println("\nInterrupted. Stopping sync...")
			cancel()
		}()

		// Sync each account
		var syncErrors []string
		for _, email := range emails {
			if ctx.Err() != nil {
				break
			}

			if err := runSync(ctx, s, oauthMgr, email); err != nil {
				syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", email, err))
				continue
			}
		}

		if len(syncErrors) > 0 {
			fmt.Println()
			fmt.Println("Errors:")
			for _, e := range syncErrors {
				fmt.Printf("  %s\n", e)
			}
			return fmt.Errorf("%d account(s) failed to sync", len(syncErrors))
		}

		return nil
	},
}

func runSync(ctx context.Context, s *store.Store, oauthMgr *oauth.Manager, email string) error {
	tokenSource, err := oauthMgr.TokenSource(ctx, email)
	if err != nil {
		return fmt.Errorf("get token source: %w (run 'add-account' first)", err)
	}

	// Create Calendar client
	rateLimiter := calendar.NewRateLimiter(float64(cfg.Sync.RateLimitQPS))
	client, err := calendar.NewClient(ctx, tokenSource,
		calendar.WithLogger(logger),
		calendar.WithRateLimiter(rateLimiter),
	)
	if err != nil {
		return fmt.Errorf("create calendar client: %w", err)
	}

	// Create syncer with progress reporter
	syncer := sync.New(client, s).
		WithLogger(logger).
		WithProgress(&CLIProgress{})

	// Run sync
	startTime := time.Now()
	syncType := "full"
	if incremental {
		syncType = "incremental"
	}
	fmt.Printf("Starting %s sync for %s\n\n", syncType, email)

	summary, err := syncer.SyncAccount(ctx, email, sync.Options{
		Incremental: incremental,
	})
	if err != nil {
		if ctx.Err() != nil {
			fmt.Println("\nSync interrupted. Run again to continue.")
			return nil
		}
		return fmt.Errorf("sync failed: %w", err)
	}

	// Print summary
	fmt.Println()
	fmt.Println("Sync complete!")
	fmt.Printf("  Duration:   %s\n", summary.Duration.Round(time.Second))
	fmt.Printf("  Calendars:  %d synced\n", summary.CalendarsSynced)
	fmt.Printf("  Events:     +%d added, ~%d updated, -%d deleted\n",
		summary.EventsAdded, summary.EventsUpdated, summary.EventsDeleted)

	elapsed := time.Since(startTime)
	logger.Info("sync completed",
		"email", email,
		"calendars", summary.CalendarsSynced,
		"events_added", summary.EventsAdded,
		"elapsed", elapsed,
	)

	return nil
}

// CLIProgress implements sync.Progress for terminal output.
type CLIProgress struct{}

func (p *CLIProgress) OnCalendarStart(calendarName string) {
	fmt.Printf("Syncing: %s\n", calendarName)
}

func (p *CLIProgress) OnCalendarDone(calendarName string, added, updated, deleted int) {
	fmt.Printf("  → +%d /%d -%d\n", added, updated, deleted)
}

func (p *CLIProgress) OnEvent(eventSummary string) {
	// Could show progress dots or event names if verbose
}

func init() {
	syncCmd.Flags().BoolVar(&incremental, "incremental", false, "Only sync changes since last sync")
	rootCmd.AddCommand(syncCmd)
}
