package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/timdestan/audiograph/internal/models"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS scrobbles (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	played_at   INTEGER NOT NULL,
	artist      TEXT    NOT NULL,
	album       TEXT    NOT NULL,
	track       TEXT    NOT NULL,
	mbid_artist TEXT    NOT NULL DEFAULT '',
	mbid_album  TEXT    NOT NULL DEFAULT '',
	mbid_track  TEXT    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_scrobbles_key ON scrobbles(played_at, artist, track);
CREATE INDEX IF NOT EXISTS idx_scrobbles_played_at ON scrobbles(played_at);
CREATE INDEX IF NOT EXISTS idx_scrobbles_artist    ON scrobbles(artist);
`

// DB wraps a SQLite connection for scrobble storage.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path and applies the schema.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	return &DB{db: db}, nil
}

func (s *DB) Close() error { return s.db.Close() }

// Insert writes scrobbles to the database, skipping any that already exist
// (matched on played_at + artist + track). Returns the number inserted.
func (s *DB) Insert(scrobbles []models.Scrobble) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO scrobbles (played_at, artist, album, track, mbid_artist, mbid_album, mbid_track)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for _, s := range scrobbles {
		res, err := stmt.Exec(
			s.PlayedAt.Unix(),
			s.Artist, s.Album, s.Track,
			s.MBIDArtist, s.MBIDAlbum, s.MBIDTrack,
		)
		if err != nil {
			return inserted, fmt.Errorf("inserting %q by %q: %w", s.Track, s.Artist, err)
		}
		n, _ := res.RowsAffected()
		inserted += int(n)
	}

	return inserted, tx.Commit()
}

// Count returns the total number of scrobbles in the database.
func (s *DB) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM scrobbles`).Scan(&n)
	return n, err
}

// RecentScrobbles returns the most recently played scrobbles, newest first.
func (s *DB) RecentScrobbles(limit int) ([]models.Scrobble, error) {
	rows, err := s.db.Query(`
		SELECT played_at, artist, album, track, mbid_artist, mbid_album, mbid_track
		FROM scrobbles
		ORDER BY played_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Scrobble
	for rows.Next() {
		var sc models.Scrobble
		var uts int64
		if err := rows.Scan(&uts, &sc.Artist, &sc.Album, &sc.Track,
			&sc.MBIDArtist, &sc.MBIDAlbum, &sc.MBIDTrack); err != nil {
			return nil, err
		}
		sc.PlayedAt = time.Unix(uts, 0).UTC()
		out = append(out, sc)
	}
	return out, rows.Err()
}
