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
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		album := r.URL.Query().Get("album")
		w.Header().Set("Content-Type", "application/json")
		w.Write(albumResponse(artByAlbum[album]))
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
