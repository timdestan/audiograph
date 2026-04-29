package lastfm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/timdestan/audiograph/internal/models"
)

const (
	apiBase  = "https://ws.audioscrobbler.com/2.0/"
	pageSize = 200
)

// Client talks to the last.fm API.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

func New(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// recentTracksResponse mirrors the last.fm JSON response for user.getRecentTracks.
type recentTracksResponse struct {
	RecentTracks struct {
		Track []struct {
			Artist struct {
				Text string `json:"#text"`
				Mbid string `json:"mbid"`
			} `json:"artist"`
			Album struct {
				Text string `json:"#text"`
				Mbid string `json:"mbid"`
			} `json:"album"`
			Name string `json:"name"`
			Mbid string `json:"mbid"`
			Date struct {
				Uts  string `json:"uts"`
				Text string `json:"#text"`
			} `json:"date"`
			Attr struct {
				NowPlaying string `json:"nowplaying"`
			} `json:"@attr"`
		} `json:"track"`
		Attr struct {
			Page       string `json:"page"`
			PerPage    string `json:"perPage"`
			TotalPages string `json:"totalPages"`
			Total      string `json:"total"`
		} `json:"@attr"`
	} `json:"recenttracks"`
}

type albumInfoResponse struct {
	Album struct {
		Image []struct {
			Text string `json:"#text"`
			Size string `json:"size"`
		} `json:"image"`
	} `json:"album"`
}

// AlbumImageURL returns the URL of the best available image for the album,
// or "" if none is found.
func (c *Client) AlbumImageURL(artist, album string) (string, error) {
	params := url.Values{
		"method":  {"album.getInfo"},
		"artist":  {artist},
		"album":   {album},
		"api_key": {c.apiKey},
		"format":  {"json"},
	}
	req, err := http.NewRequest(http.MethodGet, apiBase+"?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "audiograph/0.1 (github.com/timdestan/audiograph)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	var result albumInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	bySize := make(map[string]string, len(result.Album.Image))
	for _, img := range result.Album.Image {
		bySize[img.Size] = img.Text
	}
	for _, size := range []string{"mega", "extralarge", "large"} {
		if u := bySize[size]; u != "" {
			return u, nil
		}
	}
	return "", nil
}

// GetAllScrobbles fetches listening history for a user.
// from restricts results to scrobbles after that time; zero means all time.
// limit caps the number of scrobbles returned; 0 means no limit.
// progress is called after each page with (fetched, total).
func (c *Client) GetAllScrobbles(username string, from time.Time, limit int, progress func(fetched, total int)) ([]models.Scrobble, error) {
	var all []models.Scrobble
	page := 1

	for {
		resp, err := c.fetchPage(username, from, page)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", page, err)
		}

		totalPages, err := strconv.Atoi(resp.RecentTracks.Attr.TotalPages)
		if err != nil {
			return nil, fmt.Errorf("parsing totalPages: %w", err)
		}
		total, _ := strconv.Atoi(resp.RecentTracks.Attr.Total)

		for _, t := range resp.RecentTracks.Track {
			if limit > 0 && len(all) >= limit {
				break
			}
			// Skip the currently-playing track (has no date).
			if t.Attr.NowPlaying == "true" || t.Date.Uts == "" {
				continue
			}
			uts, err := strconv.ParseInt(t.Date.Uts, 10, 64)
			if err != nil {
				continue
			}
			all = append(all, models.Scrobble{
				Artist:     t.Artist.Text,
				Album:      t.Album.Text,
				Track:      t.Name,
				PlayedAt:   time.Unix(uts, 0).UTC(),
				MBIDArtist: t.Artist.Mbid,
				MBIDTrack:  t.Mbid,
				MBIDAlbum:  t.Album.Mbid,
			})
		}

		if progress != nil {
			progress(len(all), total)
		}

		if page >= totalPages || (limit > 0 && len(all) >= limit) {
			break
		}
		page++
	}

	return all, nil
}

func (c *Client) fetchPage(username string, from time.Time, page int) (*recentTracksResponse, error) {
	params := url.Values{
		"method":  {"user.getRecentTracks"},
		"user":    {username},
		"api_key": {c.apiKey},
		"format":  {"json"},
		"limit":   {strconv.Itoa(pageSize)},
		"page":    {strconv.Itoa(page)},
	}
	if !from.IsZero() {
		params.Set("from", strconv.FormatInt(from.Unix(), 10))
	}

	req, err := http.NewRequest(http.MethodGet, apiBase+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "audiograph/0.1 (github.com/timdestan/audiograph)")

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", httpResp.StatusCode)
	}

	var result recentTracksResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
