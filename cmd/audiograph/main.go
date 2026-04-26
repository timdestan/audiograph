package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/timdestan/audiograph/internal/lastfm"
	"github.com/timdestan/audiograph/internal/models"
	"github.com/timdestan/audiograph/internal/store"
)

func main() {
	var (
		username = flag.String("user", "", "last.fm username (required)")
		apiKey   = flag.String("api-key", "", "last.fm API key (required, or set LASTFM_API_KEY)")
		format   = flag.String("format", "json", "output format: json or csv")
		out      = flag.String("out", "", "output file path (default: stdout)")
		limit    = flag.Int("limit", 0, "max scrobbles to fetch (0 = all)")
		dbPath   = flag.String("db", "data/audiograph.db", "write scrobbles to a SQLite database at this path")
	)
	flag.Parse()

	if *username == "" {
		fmt.Fprintln(os.Stderr, "error: -user is required")
		flag.Usage()
		os.Exit(1)
	}

	key := *apiKey
	if key == "" {
		key = os.Getenv("LASTFM_API_KEY")
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "error: provide -api-key or set LASTFM_API_KEY")
		os.Exit(1)
	}

	client := lastfm.New(key)

	var from time.Time
	var db *store.DB
	if *dbPath != "" {
		var err error
		db, err = store.Open(*dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
			os.Exit(1)
		}
		defer db.Close()
		from, err = db.LatestScrobbleTime()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading latest scrobble time: %v\n", err)
			os.Exit(1)
		}
		if !from.IsZero() {
			// last.fm's `from` parameter is inclusive, so without this we'd
			// always re-fetch the most recent scrobble we already have.
			// Advancing by one second is safe because last.fm timestamps have
			// one-second granularity and a track takes longer than a second to
			// play, making same-second collisions between distinct scrobbles
			// impossible in practice. INSERT OR IGNORE is a safety net regardless.
			from = from.Add(time.Second)
		}
		if from.IsZero() {
			fmt.Fprintf(os.Stderr, "Fetching all scrobbles for %q...\n", *username)
		} else {
			fmt.Fprintf(os.Stderr, "Fetching scrobbles for %q since %s...\n", *username, from.Local().Format("2006-01-02 15:04"))
		}
	} else {
		fmt.Fprintf(os.Stderr, "Fetching scrobbles for %q...\n", *username)
	}

	scrobbles, err := client.GetAllScrobbles(*username, from, *limit, func(fetched, total int) {
		fmt.Fprintf(os.Stderr, "\r  %d / %d", fetched, total)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\nDone. %d scrobbles fetched.\n", len(scrobbles))

	if db != nil {
		inserted, err := db.Insert(scrobbles)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error writing to database: %v\n", err)
			os.Exit(1)
		}
		total, _ := db.Count()
		fmt.Fprintf(os.Stderr, "Database: %d new, %d total.\n", inserted, total)
	}

	if *dbPath == "" || *out != "" {
		w := os.Stdout
		if *out != "" {
			f, err := os.Create(*out)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error creating output file: %v\n", err)
				os.Exit(1)
			}
			defer f.Close()
			w = f
		}

		switch *format {
		case "json":
			if err := writeJSON(w, scrobbles); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		case "csv":
			if err := writeCSV(w, scrobbles); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown format %q; use json or csv\n", *format)
			os.Exit(1)
		}
	}
}

func writeJSON(w *os.File, scrobbles []models.Scrobble) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(scrobbles)
}

func writeCSV(w *os.File, scrobbles []models.Scrobble) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"played_at", "artist", "album", "track", "mbid_artist", "mbid_album", "mbid_track"}); err != nil {
		return err
	}
	for _, s := range scrobbles {
		row := []string{
			strconv.FormatInt(s.PlayedAt.Unix(), 10),
			s.Artist,
			s.Album,
			s.Track,
			s.MBIDArtist,
			s.MBIDAlbum,
			s.MBIDTrack,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
