package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/timdestan/audiograph/internal/lastfm"
	"github.com/timdestan/audiograph/internal/models"
	"github.com/timdestan/audiograph/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "import":
		runImport(os.Args[2:])
	case "purge":
		runPurge(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: audiograph <command> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  import   fetch scrobbles from last.fm and write to database or file")
	fmt.Fprintln(os.Stderr, "  purge    delete scrobbles in a time range from the database")
}

func runImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	var (
		username = fs.String("user", "", "last.fm username (required)")
		apiKey   = fs.String("api-key", "", "last.fm API key (required, or set LASTFM_API_KEY)")
		format   = fs.String("format", "json", "output format: json or csv")
		out      = fs.String("out", "", "output file path (default: stdout)")
		limit    = fs.Int("limit", 0, "max scrobbles to fetch (0 = all)")
		dbPath   = fs.String("db", "data/audiograph.db", "write scrobbles to a SQLite database at this path")
	)
	fs.Parse(args)

	if *username == "" {
		fmt.Fprintln(os.Stderr, "error: -user is required")
		fs.Usage()
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

var timeFormats = []string{
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

func parseTime(s string) (time.Time, error) {
	for _, layout := range timeFormats {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q — use YYYY-MM-DD or YYYY-MM-DD HH:MM:SS", s)
}

func runPurge(args []string) {
	fs := flag.NewFlagSet("purge", flag.ExitOnError)
	var (
		dbPath  = fs.String("db", "data/audiograph.db", "path to SQLite database")
		fromStr = fs.String("from", "", "start of range to delete, inclusive (YYYY-MM-DD or YYYY-MM-DD HH:MM:SS)")
		toStr   = fs.String("to", "", "end of range to delete, inclusive (YYYY-MM-DD or YYYY-MM-DD HH:MM:SS)")
		dryRun  = fs.Bool("dry-run", false, "show what would be deleted without deleting")
		yes     = fs.Bool("yes", false, "skip confirmation prompt")
	)
	fs.Parse(args)

	if *fromStr == "" || *toStr == "" {
		fmt.Fprintln(os.Stderr, "error: -from and -to are required")
		fs.Usage()
		os.Exit(1)
	}

	from, err := parseTime(*fromStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: -from: %v\n", err)
		os.Exit(1)
	}
	to, err := parseTime(*toStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: -to: %v\n", err)
		os.Exit(1)
	}
	// A bare date like "2026-01-15" means "through the end of that day".
	if strings.Count(*toStr, ":") == 0 {
		to = to.Add(24*time.Hour - time.Second)
	}

	if !to.After(from) {
		fmt.Fprintln(os.Stderr, "error: -to must be after -from")
		os.Exit(1)
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	count, err := db.CountInRange(from, to)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error counting scrobbles: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Range:  %s  →  %s\n", from.Format("2006-01-02 15:04:05"), to.Format("2006-01-02 15:04:05"))
	fmt.Printf("Plays:  %d scrobbles would be deleted\n", count)

	if count == 0 {
		fmt.Println("Nothing to delete.")
		return
	}

	if *dryRun {
		fmt.Println("(dry run — no changes made)")
		return
	}

	if !*yes {
		fmt.Print("Delete these scrobbles? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			fmt.Println("Aborted.")
			return
		}
	}

	deleted, err := db.DeleteRange(from, to)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error deleting scrobbles: %v\n", err)
		os.Exit(1)
	}

	total, _ := db.Count()
	fmt.Printf("Deleted %d scrobbles. %d remaining in database.\n", deleted, total)
}
