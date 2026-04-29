package artcache

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Cache is a disk-backed store for album art image bytes.
type Cache struct {
	Dir        string
	httpClient *http.Client
}

// New creates a Cache rooted at dir, creating the directory if needed.
func New(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &Cache{
		Dir:        dir,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func key(artist, album string) string {
	h := sha256.Sum256([]byte(artist + "\x00" + album))
	return fmt.Sprintf("%x", h)
}

func (c *Cache) filePath(artist, album string) string {
	return filepath.Join(c.Dir, key(artist, album))
}

// Has reports whether the image is already cached on disk.
func (c *Cache) Has(artist, album string) bool {
	_, err := os.Stat(c.filePath(artist, album))
	return err == nil
}

// Get returns the cached image bytes, or (nil, false) if not present.
func (c *Cache) Get(artist, album string) ([]byte, bool) {
	data, err := os.ReadFile(c.filePath(artist, album))
	return data, err == nil
}

// Put saves image bytes to the disk cache using a write-then-rename so a
// partial write from an interrupted process never leaves a corrupt cache file.
func (c *Cache) Put(artist, album string, data []byte) error {
	final := c.filePath(artist, album)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// Fetch downloads the image at url, saves it to the cache, and returns the bytes.
func (c *Cache) Fetch(url, artist, album string) ([]byte, error) {
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, c.Put(artist, album, data)
}
