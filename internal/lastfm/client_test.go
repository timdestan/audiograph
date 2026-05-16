package lastfm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSimplifiedAlbumNames(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"Piano Man (Remastered)", []string{"Piano Man"}},
		{"Piano Man (Remastered) [2004 Edition]", []string{"Piano Man (Remastered)", "Piano Man"}},
		{"Hits (Deluxe Edition) [2023]", []string{"Hits (Deluxe Edition)", "Hits"}},
		{"Normal Album", nil},
		{"(Just Brackets)", nil},
		{"Abbey Road", nil},
		{"  Album Name (Live)  ", []string{"Album Name"}},
		{"A (B) (C) (D)", []string{"A (B) (C)", "A (B)", "A"}},
		{"Some Album EP", []string{"Some Album"}},
		{"Some Album EP (Deluxe)", []string{"Some Album EP", "Some Album"}},
		{"EP", nil},
	}

	for _, tc := range cases {
		got := SimplifiedAlbumNames(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("SimplifiedAlbumNames(%q) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("SimplifiedAlbumNames(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

// albumResponse builds a minimal last.fm album.getInfo JSON response.
// Pass imageURL="" to simulate an album with no art.
func albumResponse(imageURL string) []byte {
	type image struct {
		Text string `json:"#text"`
		Size string `json:"size"`
	}
	type album struct {
		Image []image `json:"image"`
	}
	type response struct {
		Album album `json:"album"`
	}
	r := response{Album: album{Image: []image{
		{Text: imageURL, Size: "extralarge"},
	}}}
	b, _ := json.Marshal(r)
	return b
}

// artServer returns a test server that responds to album.getInfo requests.
// artByAlbum maps album names to their image URLs; albums not in the map get
// an empty image response.
func artServer(t *testing.T, artByAlbum map[string]string) *httptest.Server {
	t.Helper()
	return artServerFull(t, artByAlbum, nil)
}

// artServerFull handles both album.getInfo and artist.getTopAlbums requests.
// topAlbumsByArtist maps artist names to their canonical album name list.
func artServerFull(t *testing.T, artByAlbum map[string]string, topAlbumsByArtist map[string][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("method") {
		case "artist.getTopAlbums":
			artist := r.URL.Query().Get("artist")
			type albumEntry struct {
				Name string `json:"name"`
			}
			type topAlbums struct {
				Album []albumEntry `json:"album"`
			}
			type response struct {
				TopAlbums topAlbums `json:"topalbums"`
			}
			var entries []albumEntry
			for _, name := range topAlbumsByArtist[artist] {
				entries = append(entries, albumEntry{Name: name})
			}
			b, _ := json.Marshal(response{TopAlbums: topAlbums{Album: entries}})
			w.Write(b)
		default: // album.getInfo
			album := r.URL.Query().Get("album")
			w.Write(albumResponse(artByAlbum[album]))
		}
	}))
}

func TestAlbumImageURL_ExactMatch(t *testing.T) {
	const want = "https://example.com/cover.jpg"
	srv := artServer(t, map[string]string{"Piano Man": want})
	defer srv.Close()

	c := newWithBase("key", srv.URL)
	got, err := c.AlbumImageURL("Billy Joel", "Piano Man")
	if err != nil {
		t.Fatalf("AlbumImageURL: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAlbumImageURL_FallsBackToSimplifiedName(t *testing.T) {
	const want = "https://example.com/cover.jpg"
	// Exact name "Piano Man (Remastered)" returns no art; simplified "Piano Man" does.
	srv := artServer(t, map[string]string{"Piano Man": want})
	defer srv.Close()

	c := newWithBase("key", srv.URL)
	got, err := c.AlbumImageURL("Billy Joel", "Piano Man (Remastered)")
	if err != nil {
		t.Fatalf("AlbumImageURL: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAlbumImageURL_FallsBackThroughMultipleLevels(t *testing.T) {
	const want = "https://example.com/cover.jpg"
	// "Album (Deluxe) [2023]" → try "Album (Deluxe)" (no art) → try "Album" (art found).
	srv := artServer(t, map[string]string{"Album": want})
	defer srv.Close()

	c := newWithBase("key", srv.URL)
	got, err := c.AlbumImageURL("Artist", "Album (Deluxe) [2023]")
	if err != nil {
		t.Fatalf("AlbumImageURL: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAlbumImageURL_FallsBackFromEPSuffix(t *testing.T) {
	const want = "https://example.com/cover.jpg"
	srv := artServer(t, map[string]string{"Some Album": want})
	defer srv.Close()

	c := newWithBase("key", srv.URL)
	got, err := c.AlbumImageURL("Artist", "Some Album EP")
	if err != nil {
		t.Fatalf("AlbumImageURL: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeForMatching(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Rock Soldier EP", "rock soldier"},
		{"The Rock Soldier CD", "rock soldier"},
		{"Señorita", "senorita"},
		{"Senorita EP", "senorita"},
		{"A Night to Remember", "night to remember"},
		{"An Album", "album"},
		{"Normal Album", "normal album"},
		{"  Spaced  ", "spaced"},
		{"Piano Man (Remastered)", "piano man"},
		{"Piano Man (2014 Remaster)", "piano man"},
		{"Some Album [EXPLICIT]", "some album"},
		{"The Rock Soldier CD (Deluxe)", "rock soldier"},
		{"(Just Brackets)", "(just brackets)"}, // leading bracket: don't strip whole string
	}
	for _, tc := range cases {
		if got := normalizeForMatching(tc.in); got != tc.want {
			t.Errorf("normalizeForMatching(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAlbumImageURL_FallsBackViaTopAlbums(t *testing.T) {
	const want = "https://example.com/cover.jpg"
	// Scrobble has "Rock Soldier EP"; last.fm canonical name is "The Rock Soldier CD".
	srv := artServerFull(t,
		map[string]string{"The Rock Soldier CD": want},
		map[string][]string{"Superdrag": {"The Rock Soldier CD"}},
	)
	defer srv.Close()

	c := newWithBase("key", srv.URL)
	got, err := c.AlbumImageURL("Superdrag", "Rock Soldier EP")
	if err != nil {
		t.Fatalf("AlbumImageURL: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAlbumImageURL_FallsBackViaTopAlbumsWithAccent(t *testing.T) {
	const want = "https://example.com/cover.jpg"
	// Scrobble has "Senorita EP"; last.fm canonical name is "Señorita".
	srv := artServerFull(t,
		map[string]string{"Señorita": want},
		map[string][]string{"Superdrag": {"Señorita"}},
	)
	defer srv.Close()

	c := newWithBase("key", srv.URL)
	got, err := c.AlbumImageURL("Superdrag", "Senorita EP")
	if err != nil {
		t.Fatalf("AlbumImageURL: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAlbumImageURL_NoImageFound(t *testing.T) {
	// No album in the map → every request returns an empty image URL.
	srv := artServer(t, map[string]string{})
	defer srv.Close()

	c := newWithBase("key", srv.URL)
	got, err := c.AlbumImageURL("Artist", "Album (Remastered)")
	if err != nil {
		t.Fatalf("AlbumImageURL: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}
