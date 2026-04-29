package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/timdestan/audiograph/internal/store"
)

// timeFormats accepted for -from and -to flags, tried in order.
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

func main() {
	var (
		dbPath  = flag.String("db", "data/audiograph.db", "path to SQLite database")
		fromStr = flag.String("from", "", "start of range to delete, inclusive (YYYY-MM-DD or YYYY-MM-DD HH:MM:SS)")
		toStr   = flag.String("to", "", "end of range to delete, inclusive (YYYY-MM-DD or YYYY-MM-DD HH:MM:SS)")
		dryRun  = flag.Bool("dry-run", false, "show what would be deleted without deleting")
		yes     = flag.Bool("yes", false, "skip confirmation prompt")
	)
	flag.Parse()

	if *fromStr == "" || *toStr == "" {
		fmt.Fprintln(os.Stderr, "error: -from and -to are required")
		flag.Usage()
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
