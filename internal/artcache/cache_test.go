package artcache_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/timdestan/audiograph/internal/artcache"
)

func newTestCache(t *testing.T) *artcache.Cache {
	t.Helper()
	c, err := artcache.New(t.TempDir())
	if err != nil {
		t.Fatalf("artcache.New: %v", err)
	}
	return c
}

func TestNew_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "art")
	c, err := artcache.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Dir != dir {
		t.Errorf("Dir = %q, want %q", c.Dir, dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory was not created: %v", err)
	}
}

func TestHas_MissBeforePut(t *testing.T) {
	c := newTestCache(t)
	if c.Has("Artist", "Album") {
		t.Error("Has returned true for an entry that was never Put")
	}
}

func TestGet_MissBeforePut(t *testing.T) {
	c := newTestCache(t)
	data, ok := c.Get("Artist", "Album")
	if ok || data != nil {
		t.Errorf("Get returned (%v, %v) for an entry that was never Put", data, ok)
	}
}

func TestPut_RoundTrip(t *testing.T) {
	c := newTestCache(t)
	want := []byte("image bytes")

	if err := c.Put("Artist", "Album", want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !c.Has("Artist", "Album") {
		t.Error("Has returned false after Put")
	}
	got, ok := c.Get("Artist", "Album")
	if !ok {
		t.Fatal("Get returned false after Put")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Get returned %q, want %q", got, want)
	}
}

func TestPut_NoTempFileRemains(t *testing.T) {
	c := newTestCache(t)

	if err := c.Put("Artist", "Album", []byte("data")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file after Put: %s", e.Name())
		}
	}
}

func TestPut_DifferentAlbumsHaveDistinctEntries(t *testing.T) {
	c := newTestCache(t)

	cases := [][2]string{
		{"Artist A", "Album 1"},
		{"Artist A", "Album 2"},
		{"Artist B", "Album 1"},
	}
	for i, tc := range cases {
		if err := c.Put(tc[0], tc[1], []byte{byte(i)}); err != nil {
			t.Fatalf("Put %v: %v", tc, err)
		}
	}
	for i, tc := range cases {
		got, ok := c.Get(tc[0], tc[1])
		if !ok {
			t.Errorf("Get(%v) returned false", tc)
			continue
		}
		if !bytes.Equal(got, []byte{byte(i)}) {
			t.Errorf("Get(%v) = %v, want %v", tc, got, []byte{byte(i)})
		}
	}
}

func TestFetch_DownloadsAndCaches(t *testing.T) {
	want := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic bytes
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(want)
	}))
	defer srv.Close()

	c := newTestCache(t)
	got, err := c.Fetch(srv.URL, "Artist", "Album")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Fetch returned %v, want %v", got, want)
	}
	if !c.Has("Artist", "Album") {
		t.Error("Has returned false after Fetch")
	}
	fromDisk, ok := c.Get("Artist", "Album")
	if !ok || !bytes.Equal(fromDisk, want) {
		t.Errorf("Get after Fetch returned (%v, %v), want (%v, true)", fromDisk, ok, want)
	}
}

func TestFetch_NonOKResponseReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestCache(t)
	_, err := c.Fetch(srv.URL, "Artist", "Album")
	if err == nil {
		t.Fatal("Fetch: expected error for 404 response, got nil")
	}
	if c.Has("Artist", "Album") {
		t.Error("Has returned true after a failed Fetch — corrupt entry written")
	}
}

func TestFetch_NetworkErrorReturnsError(t *testing.T) {
	c := newTestCache(t)
	_, err := c.Fetch("http://127.0.0.1:0/no-server", "Artist", "Album")
	if err == nil {
		t.Fatal("Fetch: expected error for unreachable server, got nil")
	}
	if c.Has("Artist", "Album") {
		t.Error("Has returned true after a failed Fetch")
	}
}
