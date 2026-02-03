package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/salman1993/calvault/internal/query"
	"github.com/spf13/cobra"
)

var queryFile string

var queryCmd = &cobra.Command{
	Use:   "query [sql]",
	Short: "Execute a read-only SQL query",
	Long: `Execute a SQL query against the calendar database.

Only SELECT statements are allowed. Results are returned as JSON for
easy parsing by LLMs and scripts.

SQL can be provided as an argument, from a file, or via stdin:
  calvault query "SELECT COUNT(*) FROM events"
  calvault query --file query.sql
  echo "SELECT * FROM events" | calvault query
  calvault query < query.sql`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var sql string

		switch {
		case queryFile != "":
			// Read from file
			data, err := os.ReadFile(queryFile)
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}
			sql = string(data)
		case len(args) == 1:
			sql = args[0]
		default:
			// Read from stdin
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) != 0 {
				return fmt.Errorf("no query provided - pass as argument, --file, or pipe to stdin")
			}
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			sql = string(data)
		}

		sql = strings.TrimSpace(sql)
		if sql == "" {
			return fmt.Errorf("empty query")
		}

		executor, err := query.NewExecutor(cfg.DatabasePath())
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer func() { _ = executor.Close() }()

		result, err := executor.Execute(cmd.Context(), sql)
		if err != nil {
			return err
		}

		// Output as JSON for LLM consumption
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	},
}

func init() {
	queryCmd.Flags().StringVarP(&queryFile, "file", "f", "", "Read SQL from file")
	rootCmd.AddCommand(queryCmd)
}
