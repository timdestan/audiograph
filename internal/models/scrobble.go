package models

import "time"

// Scrobble represents a single play of a track.
type Scrobble struct {
	Artist    string
	Album     string
	Track     string
	PlayedAt  time.Time
	MBIDArtist string // MusicBrainz ID, may be empty
	MBIDTrack  string
	MBIDAlbum  string
}
