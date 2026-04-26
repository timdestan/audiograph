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

// ArtistCount is a query result pairing an artist with their play count.
type ArtistCount struct {
	Artist string
	Plays  int
}

// PlayCount is a generic name/play-count pair used for tracks and albums.
type PlayCount struct {
	Name  string
	Plays int
}

// LatestScrobbleTime returns the played_at of the most recent scrobble,
// or a zero time if the database is empty.
func (s *DB) LatestScrobbleTime() (time.Time, error) {
	var uts sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(played_at) FROM scrobbles`).Scan(&uts)
	if err != nil || !uts.Valid {
		return time.Time{}, err
	}
	return time.Unix(uts.Int64, 0).UTC(), nil
}

// Count returns the total number of scrobbles in the database.
func (s *DB) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM scrobbles`).Scan(&n)
	return n, err
}

// TimeCount pairs a time-bucket label with a scrobble count.
type TimeCount struct {
	Label string
	Count int
}

// ScrobbleCountsByTime returns scrobble counts for an artist grouped by the
// given strftime format string (e.g. "%Y", "%Y-%m", "%Y-%m-%d").
// A zero since means all time.
func (s *DB) ScrobbleCountsByTime(artist string, since time.Time, bucketFmt string) ([]TimeCount, error) {
	// strftime(?) is the first positional argument, followed by WHERE filters.
	args := []any{bucketFmt, artist}
	where := "WHERE artist = ?"
	if !since.IsZero() {
		where += " AND played_at >= ?"
		args = append(args, since.Unix())
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT strftime(?, datetime(played_at, 'unixepoch')), COUNT(*)
		FROM scrobbles %s
		GROUP BY 1 ORDER BY 1
	`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TimeCount
	for rows.Next() {
		var tc TimeCount
		if err := rows.Scan(&tc.Label, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// TopArtists returns artists ranked by play count within the given window.
// A zero since means all time.
func (s *DB) TopArtists(since time.Time, limit int) ([]ArtistCount, error) {
	var rows *sql.Rows
	var err error
	if since.IsZero() {
		rows, err = s.db.Query(`
			SELECT artist, COUNT(*) AS plays
			FROM scrobbles
			GROUP BY artist
			ORDER BY plays DESC
			LIMIT ?
		`, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT artist, COUNT(*) AS plays
			FROM scrobbles
			WHERE played_at >= ?
			GROUP BY artist
			ORDER BY plays DESC
			LIMIT ?
		`, since.Unix(), limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ArtistCount
	for rows.Next() {
		var ac ArtistCount
		if err := rows.Scan(&ac.Artist, &ac.Plays); err != nil {
			return nil, err
		}
		out = append(out, ac)
	}
	return out, rows.Err()
}

// topByField queries play counts grouped by a column (track or album) for one artist.
// field must be a trusted internal constant, never user input.
func (s *DB) topByField(field, artist string, since time.Time, limit int) ([]PlayCount, error) {
	q := fmt.Sprintf(`SELECT %s, COUNT(*) AS plays FROM scrobbles WHERE artist = ?`, field)
	args := []any{artist}
	if !since.IsZero() {
		q += ` AND played_at >= ?`
		args = append(args, since.Unix())
	}
	q += fmt.Sprintf(` GROUP BY %s ORDER BY plays DESC LIMIT ?`, field)
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PlayCount
	for rows.Next() {
		var pc PlayCount
		if err := rows.Scan(&pc.Name, &pc.Plays); err != nil {
			return nil, err
		}
		out = append(out, pc)
	}
	return out, rows.Err()
}

// TopTracksByArtist returns the most-played tracks for an artist within the given window.
func (s *DB) TopTracksByArtist(artist string, since time.Time, limit int) ([]PlayCount, error) {
	return s.topByField("track", artist, since, limit)
}

// TopAlbumsByArtist returns the most-played albums for an artist within the given window.
func (s *DB) TopAlbumsByArtist(artist string, since time.Time, limit int) ([]PlayCount, error) {
	return s.topByField("album", artist, since, limit)
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
