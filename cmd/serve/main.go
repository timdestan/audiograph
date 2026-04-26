package main

import (
	"encoding/json"
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

// baseHeadScripts runs before <body> to apply the saved theme before first paint.
const baseHeadScripts = `<script>
(function(){
  var s = localStorage.getItem('theme');
  var d = window.matchMedia('(prefers-color-scheme: dark)').matches;
  if (s === 'dark' || (!s && d)) document.documentElement.classList.add('dark');
})();
</script>`

const baseStyle = `
<style>
  :root {
    --bg:           #fff;
    --fg:           #222;
    --muted:        #888;
    --link:         #555;
    --active-fg:    #000;
    --border:       #ddd;
    --border-light: #eee;
    --hover-bg:     #f9f9f9;
  }
  :root.dark {
    --bg:           #1a1a1a;
    --fg:           #e0e0e0;
    --muted:        #999;
    --link:         #aaa;
    --active-fg:    #fff;
    --border:       #444;
    --border-light: #2e2e2e;
    --hover-bg:     #242424;
  }
  body { font-family: sans-serif; max-width: 860px; margin: 40px auto; padding: 0 16px;
         color: var(--fg); background: var(--bg); }
  h1   { font-size: 1.4rem; margin-bottom: 0.5rem; }
  h2   { font-size: 1.1rem; margin: 1.5rem 0 0.5rem; }
  nav  { margin-bottom: 1.5rem; font-size: 0.9rem; display: flex; align-items: baseline; gap: 1rem; }
  nav a { color: var(--link); text-decoration: none; }
  nav a:hover, nav a.active { color: var(--active-fg); text-decoration: underline; }
  nav .spacer { flex: 1; }
  .theme-btn { background: none; border: none; cursor: pointer; font: inherit;
               font-size: 0.9rem; color: var(--link); padding: 0; }
  .theme-btn:hover { color: var(--active-fg); text-decoration: underline; }
  .theme-btn::after { content: 'light'; }
  :root.dark .theme-btn::after { content: 'dark'; }
  table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
  th   { text-align: left; border-bottom: 2px solid var(--border); padding: 6px 8px; }
  td   { padding: 6px 8px; border-bottom: 1px solid var(--border-light); }
  tr:hover td { background: var(--hover-bg); }
  a    { color: var(--link); }
  a:hover { color: var(--active-fg); }
  .time  { color: var(--muted); white-space: nowrap; }
  .num   { text-align: right; color: var(--muted); }
  .periods { margin-bottom: 1rem; font-size: 0.85rem; }
  .periods a { margin-right: 0.75rem; color: var(--link); text-decoration: none; }
  .periods a:hover, .periods a.active { color: var(--active-fg); text-decoration: underline; }
  .two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 2rem; }
  .chart-wrap { position: relative; height: 180px; margin-bottom: 1.5rem; }
</style>`

// baseBodyScripts is included once at the end of each page's <body>.
const baseBodyScripts = `<script>
function toggleTheme() {
  var isDark = document.documentElement.classList.toggle('dark');
  localStorage.setItem('theme', isDark ? 'dark' : 'light');
  location.reload();
}
</script>`

const recentHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>audiograph</title>` + baseHeadScripts + baseStyle + `</head>
<body>
<h1>audiograph</h1>
<nav>
  <a href="/" class="active">Recent</a>
  <a href="/artists">Top artists</a>
  <a href="/albums">Top albums</a>
  <a href="/tracks">Top tracks</a>
  <span class="spacer"></span>
  <button class="theme-btn" onclick="toggleTheme()"></button>
</nav>
<table>
  <thead><tr><th>Time</th><th>Artist</th><th>Track</th><th>Album</th></tr></thead>
  <tbody>
  {{range .}}
  <tr>
    <td class="time">{{formatTime .PlayedAt}}</td>
    <td><a href="/artist?name={{urlquery .Artist}}">{{.Artist}}</a></td>
    <td>{{.Track}}</td>
    <td>{{.Album}}</td>
  </tr>
  {{end}}
  </tbody>
</table>
` + baseBodyScripts + `</body></html>`

const artistsHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>audiograph – top artists</title>` + baseHeadScripts + baseStyle + `</head>
<body>
<h1>audiograph</h1>
<nav>
  <a href="/">Recent</a>
  <a href="/artists" class="active">Top artists</a>
  <a href="/albums">Top albums</a>
  <a href="/tracks">Top tracks</a>
  <span class="spacer"></span>
  <button class="theme-btn" onclick="toggleTheme()"></button>
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
` + baseBodyScripts + `</body></html>`

const tracksHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>audiograph – top tracks</title>` + baseHeadScripts + baseStyle + `</head>
<body>
<h1>audiograph</h1>
<nav>
  <a href="/">Recent</a>
  <a href="/artists">Top artists</a>
  <a href="/albums">Top albums</a>
  <a href="/tracks" class="active">Top tracks</a>
  <span class="spacer"></span>
  <button class="theme-btn" onclick="toggleTheme()"></button>
</nav>
<div class="periods">
  <a href="/tracks?period=7d"  {{if eq .Period "7d" }}class="active"{{end}}>7 days</a>
  <a href="/tracks?period=30d" {{if eq .Period "30d"}}class="active"{{end}}>30 days</a>
  <a href="/tracks?period=1y"  {{if eq .Period "1y" }}class="active"{{end}}>1 year</a>
  <a href="/tracks?period=all" {{if eq .Period "all"}}class="active"{{end}}>All time</a>
</div>
<table>
  <thead><tr><th>#</th><th>Track</th><th>Artist</th><th class="num">Plays</th></tr></thead>
  <tbody>
  {{range $i, $t := .Tracks}}
  <tr>
    <td class="num">{{inc $i}}</td>
    <td>{{$t.Track}}</td>
    <td><a href="/artist?name={{urlquery $t.Artist}}&period={{$.Period}}">{{$t.Artist}}</a></td>
    <td class="num">{{$t.Plays}}</td>
  </tr>
  {{end}}
  </tbody>
</table>
` + baseBodyScripts + `</body></html>`

const albumsHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>audiograph – top albums</title>` + baseHeadScripts + baseStyle + `</head>
<body>
<h1>audiograph</h1>
<nav>
  <a href="/">Recent</a>
  <a href="/artists">Top artists</a>
  <a href="/albums" class="active">Top albums</a>
  <a href="/tracks">Top tracks</a>
  <span class="spacer"></span>
  <button class="theme-btn" onclick="toggleTheme()"></button>
</nav>
<div class="periods">
  <a href="/albums?period=7d"  {{if eq .Period "7d" }}class="active"{{end}}>7 days</a>
  <a href="/albums?period=30d" {{if eq .Period "30d"}}class="active"{{end}}>30 days</a>
  <a href="/albums?period=1y"  {{if eq .Period "1y" }}class="active"{{end}}>1 year</a>
  <a href="/albums?period=all" {{if eq .Period "all"}}class="active"{{end}}>All time</a>
</div>
<table>
  <thead><tr><th>#</th><th>Album</th><th>Artist</th><th class="num">Plays</th></tr></thead>
  <tbody>
  {{range $i, $a := .Albums}}
  <tr>
    <td class="num">{{inc $i}}</td>
    <td>{{$a.Album}}</td>
    <td><a href="/artist?name={{urlquery $a.Artist}}&period={{$.Period}}">{{$a.Artist}}</a></td>
    <td class="num">{{$a.Plays}}</td>
  </tr>
  {{end}}
  </tbody>
</table>
` + baseBodyScripts + `</body></html>`

const artistDetailHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>audiograph – {{.Artist}}</title>` + baseHeadScripts + baseStyle + `</head>
<body>
<h1>audiograph</h1>
<nav>
  <a href="/">Recent</a>
  <a href="/artists?period={{.Period}}">Top artists</a>
  <a href="/albums?period={{.Period}}">Top albums</a>
  <a href="/tracks?period={{.Period}}">Top tracks</a>
  <span class="spacer"></span>
  <button class="theme-btn" onclick="toggleTheme()"></button>
</nav>
<h2>{{.Artist}}</h2>
<div class="periods">
  <a href="/artist?name={{urlquery .Artist}}&period=7d"  {{if eq .Period "7d" }}class="active"{{end}}>7 days</a>
  <a href="/artist?name={{urlquery .Artist}}&period=30d" {{if eq .Period "30d"}}class="active"{{end}}>30 days</a>
  <a href="/artist?name={{urlquery .Artist}}&period=1y"  {{if eq .Period "1y" }}class="active"{{end}}>1 year</a>
  <a href="/artist?name={{urlquery .Artist}}&period=all" {{if eq .Period "all"}}class="active"{{end}}>All time</a>
</div>
{{if .ChartLabels}}
<div class="chart-wrap">
  <canvas id="scrobble-chart"></canvas>
</div>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4"></script>
<script>
(function(){
  var dark = document.documentElement.classList.contains('dark');
  var textColor = dark ? '#999' : '#666';
  var gridColor = dark ? '#333' : '#e5e5e5';
  new Chart(document.getElementById('scrobble-chart'), {
    type: 'bar',
    data: {
      labels: {{.ChartLabels}},
      datasets: [{ data: {{.ChartData}}, backgroundColor: '#4a90d9', borderRadius: 2,
                   barPercentage: 0.5, categoryPercentage: 0.8 }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: { legend: { display: false } },
      scales: {
        x: { grid: { display: false }, ticks: { color: textColor } },
        y: { beginAtZero: true, grid: { color: gridColor },
             ticks: { color: textColor, precision: 0 },
             title: { display: true, text: 'plays', color: textColor } }
      }
    }
  });
})();
</script>
{{end}}
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
` + baseBodyScripts + `</body></html>`

type templates struct {
	recent       *template.Template
	artists      *template.Template
	albums       *template.Template
	tracks       *template.Template
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
	albums := template.Must(template.New("albums").Funcs(funcs).Parse(albumsHTML))
	tracks := template.Must(template.New("tracks").Funcs(funcs).Parse(tracksHTML))
	artistDetail := template.Must(template.New("artistDetail").Funcs(funcs).Parse(artistDetailHTML))
	return templates{recent: recent, artists: artists, albums: albums, tracks: tracks, artistDetail: artistDetail}
}

type artistsData struct {
	Period  string
	Artists []store.ArtistCount
}

type albumsData struct {
	Period string
	Albums []store.AlbumCount
}

type tracksData struct {
	Period string
	Tracks []store.TrackCount
}

type artistDetailData struct {
	Artist      string
	Period      string
	Tracks      []store.PlayCount
	Albums      []store.PlayCount
	ChartLabels template.JS
	ChartData   template.JS
}

// periodBucketFmt returns the strftime format appropriate for the given period.
func periodBucketFmt(period string) string {
	switch period {
	case "1y":
		return "%Y-%m"
	case "7d", "30d":
		return "%Y-%m-%d"
	default:
		return "%Y"
	}
}

func marshalChartData(counts []store.TimeCount) (labels, data template.JS) {
	ls := make([]string, len(counts))
	ds := make([]int, len(counts))
	for i, c := range counts {
		ls[i] = c.Label
		ds[i] = c.Count
	}
	lsJSON, _ := json.Marshal(ls)
	dsJSON, _ := json.Marshal(ds)
	return template.JS(lsJSON), template.JS(dsJSON)
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

	http.HandleFunc("/albums", func(w http.ResponseWriter, r *http.Request) {
		since, period := parsePeriod(r.URL.Query().Get("period"))
		albums, err := db.TopAlbums(since, pageLimit)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			log.Printf("query error: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.albums.Execute(w, albumsData{Period: period, Albums: albums}); err != nil {
			log.Printf("template error: %v", err)
		}
	})

	http.HandleFunc("/tracks", func(w http.ResponseWriter, r *http.Request) {
		since, period := parsePeriod(r.URL.Query().Get("period"))
		tracks, err := db.TopTracks(since, pageLimit)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			log.Printf("query error: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.tracks.Execute(w, tracksData{Period: period, Tracks: tracks}); err != nil {
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
		counts, err := db.ScrobbleCountsByTime(artist, since, periodBucketFmt(period))
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			log.Printf("query error: %v", err)
			return
		}
		chartLabels, chartData := marshalChartData(counts)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.artistDetail.Execute(w, artistDetailData{
			Artist:      artist,
			Period:      period,
			Tracks:      tracks,
			Albums:      albums,
			ChartLabels: chartLabels,
			ChartData:   chartData,
		}); err != nil {
			log.Printf("template error: %v", err)
		}
	})

	fmt.Printf("Listening on http://%s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, logMiddleware(http.DefaultServeMux)))
}
