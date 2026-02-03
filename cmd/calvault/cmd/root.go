package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/salman1993/calvault/internal/config"
	"github.com/spf13/cobra"
)

var (
	// Build info (set via ldflags)
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"

	cfgFile string
	verbose bool
	cfg     *config.Config
	logger  *slog.Logger
)

var rootCmd = &cobra.Command{
	Use:   "calvault",
	Short: "Offline Google Calendar archive tool",
	Long: `calvault is an offline Google Calendar archive tool that exports and stores
calendar event data locally with full SQL queryability.

Archive years of calendar data and answer questions like:
"How often did I visit my dermatologist in the last 12 months?"`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading for commands that don't need it
		if cmd.Name() == "version" {
			return nil
		}

		// Set up logging
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: level,
		}))

		// Load config
		var err error
		cfg, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// oauthSetupHint is the common help text for OAuth configuration issues.
const oauthSetupHint = `
To use calvault, you need a Google Cloud OAuth credential:
  1. Go to https://console.cloud.google.com/apis/credentials
  2. Create an OAuth 2.0 Client ID (Desktop application)
  3. Download the client_secret.json file
  4. Add to your config.toml:
       [oauth]
       client_secrets = "/path/to/client_secret.json"`

// errOAuthNotConfigured returns a helpful error when OAuth client secrets are missing.
func errOAuthNotConfigured() error {
	return fmt.Errorf("OAuth client secrets not configured." + oauthSetupHint)
}

// wrapOAuthError wraps an oauth/client-secrets error with setup instructions.
func wrapOAuthError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("OAuth client secrets file not found." + oauthSetupHint)
	}
	return err
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.calvault/config.toml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
}
