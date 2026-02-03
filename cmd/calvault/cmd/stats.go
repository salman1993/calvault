package cmd

import (
	"fmt"

	"github.com/salman1993/calvault/internal/store"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show archive statistics",
	Long: `Display statistics about the calendar archive.

Shows counts of accounts, calendars, events, date range, 
unique locations, and recurring events.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := store.Open(cfg.DatabasePath())
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = s.Close() }()

		stats, err := s.GetStats()
		if err != nil {
			return fmt.Errorf("get stats: %w", err)
		}

		fmt.Println("Calendar Archive Statistics")
		fmt.Println("===========================")
		fmt.Printf("  Accounts:         %d\n", stats.AccountCount)
		fmt.Printf("  Calendars:        %d\n", stats.CalendarCount)
		fmt.Printf("  Total events:     %d\n", stats.EventCount)

		if stats.EventCount > 0 {
			fmt.Printf("  Date range:       %s to %s\n",
				stats.EarliestEvent.Format("2006-01-02"),
				stats.LatestEvent.Format("2006-01-02"))
			fmt.Printf("  Unique locations: %d\n", stats.UniqueLocations)
			fmt.Printf("  Recurring events: %d\n", stats.RecurringCount)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
