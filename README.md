# audiograph

Personal music listening history — export, store, and explore your scrobbles.

Data is fetched from [last.fm](https://www.last.fm) and stored locally in a SQLite database. Once imported, everything runs offline.

## Prerequisites

- [Go 1.21+](https://go.dev/dl/)
- A last.fm API key — create one at https://www.last.fm/api/account/create

Set your API key as an environment variable so you don't have to pass it every time:

```bash
export LASTFM_API_KEY=your_api_key_here
```

## Commands

### `audiograph` — import scrobbles

Fetches your listening history from last.fm and writes it to a local SQLite database.

```bash
# Full import into the default database (data/audiograph.db)
go run ./cmd/audiograph -user YOUR_USERNAME

# Incremental update — only fetches scrobbles since the last import
go run ./cmd/audiograph -user YOUR_USERNAME

# Limit the number of scrobbles (useful for testing)
go run ./cmd/audiograph -user YOUR_USERNAME -limit 50

# Export to JSON in addition to the database
go run ./cmd/audiograph -user YOUR_USERNAME -out data/scrobbles.json

# Export to CSV in addition to the database
go run ./cmd/audiograph -user YOUR_USERNAME -out data/scrobbles.csv -format csv
```

Running the import a second time is safe — duplicate scrobbles are skipped automatically.

**Flags**

| Flag | Default | Description |
|------|---------|-------------|
| `-user` | *(required)* | last.fm username |
| `-api-key` | `$LASTFM_API_KEY` | last.fm API key |
| `-db` | `data/audiograph.db` | SQLite database path |
| `-out` | stdout | Output file path (JSON/CSV) |
| `-format` | `json` | Output format: `json` or `csv` |
| `-limit` | `0` (all) | Max scrobbles to fetch |

### `serve` — browse locally

Starts a local web server for exploring your listening history.

```bash
go run ./cmd/serve
```

Then open http://localhost:8080 in your browser.

**Pages**

- `/` — 100 most recent scrobbles
- `/artists` — top artists, filterable by time period (7 days / 30 days / 1 year / all time)
- `/artist?name=Artist+Name` — top tracks and albums for a single artist, with the same period filter

**Flags**

| Flag | Default | Description |
|------|---------|-------------|
| `-db` | `data/audiograph.db` | SQLite database path |
| `-addr` | `localhost:8080` | Listen address |

## Building binaries

```bash
go build -o audiograph ./cmd/audiograph
go build -o serve     ./cmd/serve
```
