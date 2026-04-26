package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/timdestan/audiograph/internal/store"
)

const pageLimit = 100

const baseStyle = `
<style>
  body { font-family: sans-serif; max-width: 860px; margin: 40px auto; padding: 0 16px; color: #222; }
  h1   { font-size: 1.4rem; margin-bottom: 0.5rem; }
  h2   { font-size: 1.1rem; margin: 1.5rem 0 0.5rem; }
  nav  { margin-bottom: 1.5rem; font-size: 0.9rem; }
  nav a { margin-right: 1rem; color: #555; text-decoration: none; }
  nav a:hover, nav a.active { color: #000; text-decoration: underline; }
  table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
  th   { text-align: left; border-bottom: 2px solid #ddd; padding: 6px 8px; }
  td   { padding: 6px 8px; border-bottom: 1px solid #eee; }
  tr:hover td { background: #f9f9f9; }
  .time  { color: #888; white-space: nowrap; }
  .num   { text-align: right; color: #555; }
  .periods { margin-bottom: 1rem; font-size: 0.85rem; }
  .periods a { margin-right: 0.75rem; color: #555; text-decoration: none; }
  .periods a:hover, .periods a.active { color: #000; text-decoration: underline; }
  .two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 2rem; }
</style>`

const recentHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>audiograph</title>` + baseStyle + `</head>
<body>
<h1>audiograph</h1>
<nav>
  <a href="/" class="active">Recent</a>
  <a href="/artists">Top artists</a>
</nav>
<table>
  <thead><tr><th>Time</th><th>Artist</th><th>Track</th><th>Album</th></tr></thead>
  <tbody>
  {{range .}}
  <tr>
    <td class="time">{{formatTime .PlayedAt}}</td>
    <td>{{.Artist}}</td>
    <td>{{.Track}}</td>
    <td>{{.Album}}</td>
  </tr>
  {{end}}
  </tbody>
</table>
</body></html>`

const artistsHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>audiograph – top artists</title>` + baseStyle + `</head>
<body>
<h1>audiograph</h1>
<nav>
  <a href="/">Recent</a>
  <a href="/artists" class="active">Top artists</a>
</nav>
<div class="periods">
  <a href="/artists?period=7d"  {{if eq .Period "7d" }}class="active"{{end}}>7 days</a>
  <a href="/artists?period=30d" {{if eq .Period "30d"}}class="active"{{end}}>30 days</a>
  <a href="/artists?period=1y"  {{if eq .Period "1y" }}class="active"{{end}}>1 year</a>
  <a href="/artists?period=all" {{if eq .Period "all"}}class="active"{{end}}>All time</a>
</div>
<table>
  <thead><tr><th>#</th><th>Artist</th><th class="num">Plays</th></tr></thead>
  <tbody>
  {{range $i, $a := .Artists}}
  <tr>
    <td class="num">{{inc $i}}</td>
    <td><a href="/artist?name={{urlquery $a.Artist}}&period={{$.Period}}">{{$a.Artist}}</a></td>
    <td class="num">{{$a.Plays}}</td>
  </tr>
  {{end}}
  </tbody>
</table>
</body></html>`

const artistDetailHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>audiograph – {{.Artist}}</title>` + baseStyle + `</head>
<body>
<h1>audiograph</h1>
<nav>
  <a href="/">Recent</a>
  <a href="/artists?period={{.Period}}">Top artists</a>
</nav>
<h2>{{.Artist}}</h2>
<div class="periods">
  <a href="/artist?name={{urlquery .Artist}}&period=7d"  {{if eq .Period "7d" }}class="active"{{end}}>7 days</a>
  <a href="/artist?name={{urlquery .Artist}}&period=30d" {{if eq .Period "30d"}}class="active"{{end}}>30 days</a>
  <a href="/artist?name={{urlquery .Artist}}&period=1y"  {{if eq .Period "1y" }}class="active"{{end}}>1 year</a>
  <a href="/artist?name={{urlquery .Artist}}&period=all" {{if eq .Period "all"}}class="active"{{end}}>All time</a>
</div>
<div class="two-col">
  <div>
    <table>
      <thead><tr><th>#</th><th>Track</th><th class="num">Plays</th></tr></thead>
      <tbody>
      {{range $i, $t := .Tracks}}
      <tr>
        <td class="num">{{inc $i}}</td>
        <td>{{$t.Name}}</td>
        <td class="num">{{$t.Plays}}</td>
      </tr>
      {{end}}
      </tbody>
    </table>
  </div>
  <div>
    <table>
      <thead><tr><th>#</th><th>Album</th><th class="num">Plays</th></tr></thead>
      <tbody>
      {{range $i, $a := .Albums}}
      <tr>
        <td class="num">{{inc $i}}</td>
        <td>{{$a.Name}}</td>
        <td class="num">{{$a.Plays}}</td>
      </tr>
      {{end}}
      </tbody>
    </table>
  </div>
</div>
</body></html>`

type templates struct {
	recent       *template.Template
	artists      *template.Template
	artistDetail *template.Template
}

func buildTemplates() templates {
	funcs := template.FuncMap{
		"inc":      func(i int) int { return i + 1 },
		"urlquery": url.QueryEscape,
		"formatTime": func(t time.Time) string {
			return t.Local().Format("2006-01-02 15:04")
		},
	}
	recent := template.Must(template.New("recent").Funcs(funcs).Parse(recentHTML))
	artists := template.Must(template.New("artists").Funcs(funcs).Parse(artistsHTML))
	artistDetail := template.Must(template.New("artistDetail").Funcs(funcs).Parse(artistDetailHTML))
	return templates{recent: recent, artists: artists, artistDetail: artistDetail}
}

type artistsData struct {
	Period  string
	Artists []store.ArtistCount
}

type artistDetailData struct {
	Artist string
	Period string
	Tracks []store.PlayCount
	Albums []store.PlayCount
}

// statusRecorder captures the HTTP status code written by a handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

const (
	ansiReset = "\033[0m"
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
	ansiGrey  = "\033[90m"
)

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		statusColor := ansiGreen
		if rec.status >= 400 {
			statusColor = ansiRed
		}
		log.Printf("%s%s %s %d%s %s",
			statusColor, r.Method, r.URL.RequestURI(), rec.status, ansiReset,
			formatDuration(time.Since(start)),
		)
	})
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%d%sns%s", d.Nanoseconds(), ansiGrey, ansiReset)
	case d < time.Millisecond:
		return fmt.Sprintf("%.1f%sµs%s", float64(d.Nanoseconds())/1e3, ansiGrey, ansiReset)
	case d < time.Second:
		return fmt.Sprintf("%.1f%sms%s", float64(d.Nanoseconds())/1e6, ansiGrey, ansiReset)
	default:
		return fmt.Sprintf("%.2f%ss%s", d.Seconds(), ansiGrey, ansiReset)
	}
}

func parsePeriod(s string) (time.Time, string) {
	now := time.Now()
	switch s {
	case "7d":
		return now.AddDate(0, 0, -7), "7d"
	case "30d":
		return now.AddDate(0, 0, -30), "30d"
	case "1y":
		return now.AddDate(-1, 0, 0), "1y"
	default:
		return time.Time{}, "all"
	}
}

func main() {
	dbPath := flag.String("db", "data/audiograph.db", "path to SQLite database")
	addr := flag.String("addr", "localhost:8080", "listen address")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer db.Close()

	tmpl := buildTemplates()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		scrobbles, err := db.RecentScrobbles(pageLimit)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			log.Printf("query error: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.recent.Execute(w, scrobbles); err != nil {
			log.Printf("template error: %v", err)
		}
	})

	http.HandleFunc("/artists", func(w http.ResponseWriter, r *http.Request) {
		since, period := parsePeriod(r.URL.Query().Get("period"))
		artists, err := db.TopArtists(since, pageLimit)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			log.Printf("query error: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.artists.Execute(w, artistsData{Period: period, Artists: artists}); err != nil {
			log.Printf("template error: %v", err)
		}
	})

	http.HandleFunc("/artist", func(w http.ResponseWriter, r *http.Request) {
		artist := r.URL.Query().Get("name")
		if artist == "" {
			http.Redirect(w, r, "/artists", http.StatusSeeOther)
			return
		}
		since, period := parsePeriod(r.URL.Query().Get("period"))
		tracks, err := db.TopTracksByArtist(artist, since, pageLimit)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			log.Printf("query error: %v", err)
			return
		}
		albums, err := db.TopAlbumsByArtist(artist, since, pageLimit)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			log.Printf("query error: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.artistDetail.Execute(w, artistDetailData{
			Artist: artist,
			Period: period,
			Tracks: tracks,
			Albums: albums,
		}); err != nil {
			log.Printf("template error: %v", err)
		}
	})

	fmt.Printf("Listening on http://%s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, logMiddleware(http.DefaultServeMux)))
}
