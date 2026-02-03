# calvault

Offline Google Calendar archive tool. Export and store calendar event data locally with full SQL queryability.

Inspired by [msgvault](https://www.msgvault.io/); read the [blog post](https://wesmckinney.com/blog/announcing-msgvault/).

## Installation

```bash
just build
just install  # installs to ~/.local/bin
```

## Setup

1. Create OAuth credentials at [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
2. Download `client_secret.json`
3. Configure calvault:

```bash
mkdir -p ~/.calvault
cat > ~/.calvault/config.toml << EOF
[oauth]
client_secrets = "/path/to/client_secret.json"
EOF
```

## Usage

```bash
# Add a Google account
calvault add-account you@gmail.com

# Sync all calendars
calvault sync you@gmail.com

# Incremental sync (faster, only changes)
calvault sync you@gmail.com --incremental

# View statistics
calvault stats

# Query with SQL
calvault query "SELECT summary, start_time FROM events ORDER BY start_time DESC LIMIT 10"
```

## Example Queries

See [examples/](examples/) for sample queries:

```bash
calvault query -f examples/dermatologist_visits.sql
calvault query -f examples/busiest_days.sql
calvault query -f examples/meetings_by_organizer.sql
```

## License

MIT
