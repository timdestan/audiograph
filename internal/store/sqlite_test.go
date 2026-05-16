package store_test

import (
	"testing"
	"time"

	"github.com/timdestan/audiograph/internal/models"
	"github.com/timdestan/audiograph/internal/store"
)

// epoch is a fixed reference point so day offsets produce deterministic Unix timestamps.
var epoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func at(day int) time.Time { return epoch.AddDate(0, 0, day) }

func sc(artist, album, track string, day int) models.Scrobble {
	return models.Scrobble{Artist: artist, Album: album, Track: track, PlayedAt: at(day)}
}

func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustInsert(t *testing.T, db *store.DB, ss []models.Scrobble) int {
	t.Helper()
	n, err := db.Insert(ss)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return n
}

func mustAddTag(t *testing.T, db *store.DB, artist, tag string) {
	t.Helper()
	if err := db.AddArtistTag(artist, tag); err != nil {
		t.Fatalf("AddArtistTag(%q, %q): %v", artist, tag, err)
	}
}

// --- Insert / Count ---

func TestOpen_EmptyDB(t *testing.T) {
	db := openTestDB(t)
	n, err := db.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("Count = %d, want 0", n)
	}
}

func TestInsert_CountRoundTrip(t *testing.T) {
	db := openTestDB(t)
	n := mustInsert(t, db, []models.Scrobble{
		sc("A", "Ax", "T1", 0),
		sc("A", "Ax", "T2", 1),
		sc("B", "Bx", "T1", 2),
	})
	if n != 3 {
		t.Errorf("Insert returned %d, want 3", n)
	}
	count, err := db.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 3 {
		t.Errorf("Count = %d, want 3", count)
	}
}

func TestInsert_SkipsDuplicates(t *testing.T) {
	db := openTestDB(t)
	row := sc("A", "Ax", "T1", 0)
	n1 := mustInsert(t, db, []models.Scrobble{row})
	n2 := mustInsert(t, db, []models.Scrobble{row})
	if n1 != 1 || n2 != 0 {
		t.Errorf("Insert counts = %d, %d; want 1, 0", n1, n2)
	}
	count, _ := db.Count()
	if count != 1 {
		t.Errorf("Count after duplicate = %d, want 1", count)
	}
}

// --- LatestScrobbleTime ---

func TestLatestScrobbleTime(t *testing.T) {
	db := openTestDB(t)

	got, err := db.LatestScrobbleTime()
	if err != nil {
		t.Fatalf("LatestScrobbleTime (empty): %v", err)
	}
	if !got.IsZero() {
		t.Errorf("LatestScrobbleTime (empty) = %v, want zero", got)
	}

	mustInsert(t, db, []models.Scrobble{
		sc("A", "x", "T1", 0),
		sc("A", "x", "T2", 5),
		sc("A", "x", "T3", 3),
	})
	got, err = db.LatestScrobbleTime()
	if err != nil {
		t.Fatalf("LatestScrobbleTime: %v", err)
	}
	if got.Unix() != at(5).Unix() {
		t.Errorf("LatestScrobbleTime = %v, want %v", got, at(5))
	}
}

// --- TopArtists ---

func TestTopArtists(t *testing.T) {
	db := openTestDB(t)
	mustInsert(t, db, []models.Scrobble{
		sc("A", "x", "t1", 0),
		sc("A", "x", "t2", 1),
		sc("A", "x", "t3", 2),
		sc("B", "y", "t1", 3),
		sc("B", "y", "t2", 4),
		sc("C", "z", "t1", 5),
	})

	t.Run("all time ranking", func(t *testing.T) {
		artists, err := db.TopArtists(time.Time{}, 10)
		if err != nil {
			t.Fatalf("TopArtists: %v", err)
		}
		if len(artists) != 3 {
			t.Fatalf("got %d artists, want 3", len(artists))
		}
		if artists[0].Artist != "A" || artists[0].Plays != 3 {
			t.Errorf("rank 1 = %+v, want {A 3}", artists[0])
		}
		if artists[1].Artist != "B" || artists[1].Plays != 2 {
			t.Errorf("rank 2 = %+v, want {B 2}", artists[1])
		}
		if artists[2].Artist != "C" || artists[2].Plays != 1 {
			t.Errorf("rank 3 = %+v, want {C 1}", artists[2])
		}
	})

	t.Run("period filter excludes old scrobbles", func(t *testing.T) {
		// since=day3 → only B(2) and C(1) qualify; A's plays are all before day3
		artists, err := db.TopArtists(at(3), 10)
		if err != nil {
			t.Fatalf("TopArtists: %v", err)
		}
		if len(artists) != 2 {
			t.Fatalf("got %d artists, want 2", len(artists))
		}
		if artists[0].Artist != "B" {
			t.Errorf("rank 1 = %q, want B", artists[0].Artist)
		}
	})

	t.Run("limit", func(t *testing.T) {
		artists, err := db.TopArtists(time.Time{}, 2)
		if err != nil {
			t.Fatalf("TopArtists: %v", err)
		}
		if len(artists) != 2 {
			t.Errorf("got %d artists, want 2", len(artists))
		}
	})
}

// --- Artist tags ---

func TestArtistTags_AddRemove(t *testing.T) {
	db := openTestDB(t)

	tags, err := db.ArtistTags("Artist A")
	if err != nil {
		t.Fatalf("ArtistTags (empty): %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("ArtistTags (empty) = %v, want []", tags)
	}

	mustAddTag(t, db, "Artist A", "ambient")
	mustAddTag(t, db, "Artist A", "electronic")

	tags, err = db.ArtistTags("Artist A")
	if err != nil {
		t.Fatalf("ArtistTags: %v", err)
	}
	if len(tags) != 2 || tags[0] != "ambient" || tags[1] != "electronic" {
		t.Errorf("ArtistTags = %v, want [ambient electronic]", tags)
	}

	// Duplicate add is idempotent.
	mustAddTag(t, db, "Artist A", "ambient")
	tags, _ = db.ArtistTags("Artist A")
	if len(tags) != 2 {
		t.Errorf("after duplicate add: %d tags, want 2", len(tags))
	}

	// Remove a tag.
	if err := db.RemoveArtistTag("Artist A", "ambient"); err != nil {
		t.Fatalf("RemoveArtistTag: %v", err)
	}
	tags, _ = db.ArtistTags("Artist A")
	if len(tags) != 1 || tags[0] != "electronic" {
		t.Errorf("after remove: ArtistTags = %v, want [electronic]", tags)
	}

	// Remove a tag that doesn't exist is a no-op.
	if err := db.RemoveArtistTag("Artist A", "nonexistent"); err != nil {
		t.Fatalf("RemoveArtistTag (nonexistent): %v", err)
	}
}

func TestAllTags_Distinct(t *testing.T) {
	db := openTestDB(t)

	tags, err := db.AllTags()
	if err != nil {
		t.Fatalf("AllTags (empty): %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("AllTags (empty) = %v, want []", tags)
	}

	mustAddTag(t, db, "Artist A", "ambient")
	mustAddTag(t, db, "Artist B", "ambient") // same tag, different artist — should appear once
	mustAddTag(t, db, "Artist B", "jazz")

	tags, err = db.AllTags()
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}
	if len(tags) != 2 || tags[0] != "ambient" || tags[1] != "jazz" {
		t.Errorf("AllTags = %v, want [ambient jazz]", tags)
	}
}

func TestAllArtistTags(t *testing.T) {
	db := openTestDB(t)

	mustAddTag(t, db, "Artist A", "ambient")
	mustAddTag(t, db, "Artist A", "electronic")
	mustAddTag(t, db, "Artist B", "jazz")

	m, err := db.AllArtistTags()
	if err != nil {
		t.Fatalf("AllArtistTags: %v", err)
	}
	if len(m["Artist A"]) != 2 || m["Artist A"][0] != "ambient" || m["Artist A"][1] != "electronic" {
		t.Errorf("Artist A = %v, want [ambient electronic]", m["Artist A"])
	}
	if len(m["Artist B"]) != 1 || m["Artist B"][0] != "jazz" {
		t.Errorf("Artist B = %v, want [jazz]", m["Artist B"])
	}
	if _, ok := m["Artist C"]; ok {
		t.Error("Artist C should not appear in the map")
	}
}

func TestTopArtistsByTag(t *testing.T) {
	db := openTestDB(t)
	mustInsert(t, db, []models.Scrobble{
		sc("A", "x", "t1", 0),
		sc("A", "x", "t2", 1),
		sc("A", "x", "t3", 2),
		sc("B", "y", "t1", 3),
		sc("B", "y", "t2", 4),
		sc("C", "z", "t1", 5),
	})
	mustAddTag(t, db, "A", "ambient")
	mustAddTag(t, db, "B", "ambient")
	mustAddTag(t, db, "C", "jazz")

	t.Run("tag filter", func(t *testing.T) {
		artists, err := db.TopArtistsByTag("ambient", time.Time{}, 10)
		if err != nil {
			t.Fatalf("TopArtistsByTag: %v", err)
		}
		if len(artists) != 2 {
			t.Fatalf("got %d artists, want 2", len(artists))
		}
		if artists[0].Artist != "A" || artists[0].Plays != 3 {
			t.Errorf("rank 1 = %+v, want {A 3}", artists[0])
		}
		if artists[1].Artist != "B" || artists[1].Plays != 2 {
			t.Errorf("rank 2 = %+v, want {B 2}", artists[1])
		}
	})

	t.Run("tag + period filter", func(t *testing.T) {
		// since=day3: A's scrobbles (days 0-2) are excluded, only B qualifies for "ambient"
		artists, err := db.TopArtistsByTag("ambient", at(3), 10)
		if err != nil {
			t.Fatalf("TopArtistsByTag: %v", err)
		}
		if len(artists) != 1 || artists[0].Artist != "B" || artists[0].Plays != 2 {
			t.Errorf("got %v, want [{B 2}]", artists)
		}
	})

	t.Run("tag with no artists", func(t *testing.T) {
		artists, err := db.TopArtistsByTag("classical", time.Time{}, 10)
		if err != nil {
			t.Fatalf("TopArtistsByTag: %v", err)
		}
		if len(artists) != 0 {
			t.Errorf("got %v, want []", artists)
		}
	})
}

// --- AlbumArt ---

func TestAlbumArt_GetSet(t *testing.T) {
	db := openTestDB(t)

	// No entry yet — not cached.
	_, cached, err := db.AlbumArt("Artist", "Album")
	if err != nil || cached {
		t.Errorf("AlbumArt (empty): cached=%v err=%v, want cached=false err=nil", cached, err)
	}

	// Set a URL and retrieve it.
	const wantURL = "https://example.com/art.jpg"
	if err := db.SetAlbumArt("Artist", "Album", wantURL); err != nil {
		t.Fatalf("SetAlbumArt: %v", err)
	}
	gotURL, cached, err := db.AlbumArt("Artist", "Album")
	if err != nil || !cached || gotURL != wantURL {
		t.Errorf("AlbumArt = (%q, %v, %v), want (%q, true, nil)", gotURL, cached, err, wantURL)
	}

	// An empty URL means "tried but found nothing" — still cached.
	if err := db.SetAlbumArt("Artist", "NoArt", ""); err != nil {
		t.Fatalf("SetAlbumArt (empty url): %v", err)
	}
	gotURL, cached, err = db.AlbumArt("Artist", "NoArt")
	if err != nil || !cached || gotURL != "" {
		t.Errorf("AlbumArt (no art) = (%q, %v, %v), want ('', true, nil)", gotURL, cached, err)
	}
}

func TestClearUnresolvedAlbumArt(t *testing.T) {
	db := openTestDB(t)

	if err := db.SetAlbumArt("Artist", "Miss", ""); err != nil {
		t.Fatalf("SetAlbumArt: %v", err)
	}

	// Cutoff in the past — entry is newer, should not be cleared.
	n, err := db.ClearUnresolvedAlbumArt(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ClearUnresolvedAlbumArt: %v", err)
	}
	if n != 0 {
		t.Errorf("cleared %d rows with past cutoff, want 0", n)
	}
	if _, cached, _ := db.AlbumArt("Artist", "Miss"); !cached {
		t.Error("Miss should still be cached after past-cutoff clear")
	}

	// Cutoff in the future — entry is older, should be cleared.
	n, err = db.ClearUnresolvedAlbumArt(time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("ClearUnresolvedAlbumArt: %v", err)
	}
	if n != 1 {
		t.Errorf("cleared %d rows with future cutoff, want 1", n)
	}
	if _, cached, _ := db.AlbumArt("Artist", "Miss"); cached {
		t.Error("Miss should have been cleared by future-cutoff clear")
	}
}

// --- CountInRange / DeleteRange ---

func TestCountInRange_DeleteRange(t *testing.T) {
	db := openTestDB(t)
	mustInsert(t, db, []models.Scrobble{
		sc("A", "x", "t1", 0),
		sc("A", "x", "t2", 5),
		sc("A", "x", "t3", 10),
	})

	n, err := db.CountInRange(at(0), at(5))
	if err != nil {
		t.Fatalf("CountInRange: %v", err)
	}
	if n != 2 {
		t.Errorf("CountInRange(0..5) = %d, want 2", n)
	}

	deleted, err := db.DeleteRange(at(0), at(5))
	if err != nil {
		t.Fatalf("DeleteRange: %v", err)
	}
	if deleted != 2 {
		t.Errorf("DeleteRange returned %d, want 2", deleted)
	}

	total, _ := db.Count()
	if total != 1 {
		t.Errorf("Count after delete = %d, want 1", total)
	}
}
