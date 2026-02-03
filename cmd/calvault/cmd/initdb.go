package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/salman1993/calvault/internal/store"
	"github.com/spf13/cobra"
)

var initDBCmd = &cobra.Command{
	Use:   "init-db",
	Short: "Initialize the database schema",
	Long: `Initialize the calvault database with the required schema.

This command creates all necessary tables for storing calendars, events,
attendees, and sync state. It is safe to run multiple times - tables are 
only created if they don't already exist.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := cfg.DatabasePath()
		logger.Info("initializing database", "path", dbPath)

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}

		s, err := store.Open(dbPath)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = s.Close() }()

		if err := s.InitSchema(); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}

		logger.Info("database initialized successfully")

		// Print stats
		stats, err := s.GetStats()
		if err != nil {
			return fmt.Errorf("get stats: %w", err)
		}

		fmt.Printf("Database: %s\n", dbPath)
		fmt.Printf("  Accounts:   %d\n", stats.AccountCount)
		fmt.Printf("  Calendars:  %d\n", stats.CalendarCount)
		fmt.Printf("  Events:     %d\n", stats.EventCount)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initDBCmd)
}
