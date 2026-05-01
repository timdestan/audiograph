package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/timdestan/audiograph/internal/models"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS album_art (
	artist TEXT NOT NULL,
	album  TEXT NOT NULL,
	url    TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (artist, album)
);
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

// AlbumCount pairs an album+artist with a play count.
type AlbumCount struct {
	Album  string
	Artist string
	Plays  int
	MBID   string
}

// TrackCount pairs a track+artist with a play count.
type TrackCount struct {
	Track  string
	Artist string
	Plays  int
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

// TopAlbums returns albums ranked by play count within the given window.
// Albums are keyed by (album, artist) to avoid collisions across artists.
// A zero since means all time.
func (s *DB) TopAlbums(since time.Time, limit int) ([]AlbumCount, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if since.IsZero() {
		rows, err = s.db.Query(`
			SELECT album, artist, COUNT(*) AS plays, MAX(mbid_album) AS mbid
			FROM scrobbles
			GROUP BY album, artist
			ORDER BY plays DESC
			LIMIT ?
		`, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT album, artist, COUNT(*) AS plays, MAX(mbid_album) AS mbid
			FROM scrobbles
			WHERE played_at >= ?
			GROUP BY album, artist
			ORDER BY plays DESC
			LIMIT ?
		`, since.Unix(), limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AlbumCount
	for rows.Next() {
		var ac AlbumCount
		if err := rows.Scan(&ac.Album, &ac.Artist, &ac.Plays, &ac.MBID); err != nil {
			return nil, err
		}
		out = append(out, ac)
	}
	return out, rows.Err()
}

// TopTracks returns tracks ranked by play count within the given window.
// Tracks are keyed by (track, artist) to avoid collisions across artists.
// A zero since means all time.
func (s *DB) TopTracks(since time.Time, limit int) ([]TrackCount, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if since.IsZero() {
		rows, err = s.db.Query(`
			SELECT track, artist, COUNT(*) AS plays
			FROM scrobbles
			GROUP BY track, artist
			ORDER BY plays DESC
			LIMIT ?
		`, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT track, artist, COUNT(*) AS plays
			FROM scrobbles
			WHERE played_at >= ?
			GROUP BY track, artist
			ORDER BY plays DESC
			LIMIT ?
		`, since.Unix(), limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TrackCount
	for rows.Next() {
		var tc TrackCount
		if err := rows.Scan(&tc.Track, &tc.Artist, &tc.Plays); err != nil {
			return nil, err
		}
		out = append(out, tc)
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

// AlbumArt returns the cached image URL for an artist+album pair.
// cached=false means no lookup has been attempted yet.
// A cached record with an empty url means a lookup was tried but found nothing.
func (s *DB) AlbumArt(artist, album string) (url string, cached bool, err error) {
	var u string
	err = s.db.QueryRow(
		`SELECT url FROM album_art WHERE artist = ? AND album = ?`,
		artist, album,
	).Scan(&u)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return u, true, nil
}

// SetAlbumArt stores a resolved image URL for an artist+album.
// Pass an empty url to record that no art was found, preventing future lookups.
func (s *DB) SetAlbumArt(artist, album, url string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO album_art (artist, album, url) VALUES (?, ?, ?)`,
		artist, album, url,
	)
	return err
}

// CountInRange returns the number of scrobbles with played_at in [from, to].
func (s *DB) CountInRange(from, to time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM scrobbles WHERE played_at >= ? AND played_at <= ?`,
		from.Unix(), to.Unix(),
	).Scan(&n)
	return n, err
}

// DeleteRange removes all scrobbles with played_at in [from, to] and returns
// the number of rows deleted.
func (s *DB) DeleteRange(from, to time.Time) (int64, error) {
	res, err := s.db.Exec(
		`DELETE FROM scrobbles WHERE played_at >= ? AND played_at <= ?`,
		from.Unix(), to.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AlbumRef is a minimal album identity used for art prefetching.
type AlbumRef struct {
	Artist string
	Album  string
	MBID   string
}

// AlbumArtEntry is a resolved album_art row with a non-empty URL.
type AlbumArtEntry struct {
	Artist string
	Album  string
	URL    string
}

// ClearUnresolvedAlbumArt deletes all album_art rows where no URL was found,
// allowing them to be re-resolved on the next prefetch. Returns the number
// of rows removed.
func (s *DB) ClearUnresolvedAlbumArt() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM album_art WHERE url = ''`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AlbumArtEntries returns all album_art rows that have a resolved URL.
// Used by the prefetch worker to download any that aren't yet on disk.
func (s *DB) AlbumArtEntries() ([]AlbumArtEntry, error) {
	rows, err := s.db.Query(
		`SELECT artist, album, url FROM album_art WHERE url != ''`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AlbumArtEntry
	for rows.Next() {
		var e AlbumArtEntry
		if err := rows.Scan(&e.Artist, &e.Album, &e.URL); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UnresolvedAlbums returns distinct albums from scrobbles that have no
// entry in album_art yet. Used by the prefetch worker to discover new art.
func (s *DB) UnresolvedAlbums() ([]AlbumRef, error) {
	rows, err := s.db.Query(`
		SELECT s.artist, s.album, MAX(s.mbid_album) AS mbid
		FROM scrobbles s
		LEFT JOIN album_art aa ON aa.artist = s.artist AND aa.album = s.album
		WHERE aa.artist IS NULL
		GROUP BY s.artist, s.album
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AlbumRef
	for rows.Next() {
		var a AlbumRef
		if err := rows.Scan(&a.Artist, &a.Album, &a.MBID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecentScrobbles returns scrobbles ordered newest-first with limit/offset for pagination.
func (s *DB) RecentScrobbles(limit, offset int) ([]models.Scrobble, error) {
	rows, err := s.db.Query(`
		SELECT played_at, artist, album, track, mbid_artist, mbid_album, mbid_track
		FROM scrobbles
		ORDER BY played_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
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
