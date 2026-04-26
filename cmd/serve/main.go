package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/timdestan/audiograph/internal/store"
)

const pageLimit = 100

var tmpl = template.Must(template.New("scrobbles").Funcs(template.FuncMap{
	"formatTime": func(t time.Time) string {
		return t.Local().Format("2006-01-02 15:04")
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>audiograph</title>
<style>
  body { font-family: sans-serif; max-width: 860px; margin: 40px auto; padding: 0 16px; color: #222; }
  h1   { font-size: 1.4rem; margin-bottom: 1.5rem; }
  table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
  th   { text-align: left; border-bottom: 2px solid #ddd; padding: 6px 8px; }
  td   { padding: 6px 8px; border-bottom: 1px solid #eee; }
  tr:hover td { background: #f9f9f9; }
  .time { color: #888; white-space: nowrap; }
</style>
</head>
<body>
<h1>Recent scrobbles</h1>
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
</body>
</html>
`))

func main() {
	dbPath := flag.String("db", "data/audiograph.db", "path to SQLite database")
	addr := flag.String("addr", "localhost:8080", "listen address")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer db.Close()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		scrobbles, err := db.RecentScrobbles(pageLimit)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			log.Printf("query error: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, scrobbles); err != nil {
			log.Printf("template error: %v", err)
		}
	})

	fmt.Printf("Listening on http://%s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
