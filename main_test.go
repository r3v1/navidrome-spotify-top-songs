package main

import (
	"testing"
)

const lastfmFixture = `{
  "toptracks": {
    "track": [
      {
        "name": "Creep",
        "duration": "238",
        "mbid": "2062f4b8-2b3b-4a5a-9d3f-2b0e0a9a1f11",
        "artist": {"name": "Radiohead", "mbid": "a74b1b7f-71a5-4011-9441-d0b5e4122711", "url": "https://www.last.fm/music/Radiohead"},
        "listeners": "1234567"
      },
      {
        "name": "Karma Police",
        "duration": "",
        "mbid": "",
        "artist": {"name": "Radiohead", "mbid": "a74b1b7f-71a5-4011-9441-d0b5e4122711"}
      },
      {
        "name": "",
        "duration": "100",
        "artist": {"name": "Radiohead"}
      }
    ]
  }
}`

const lastfmErrorFixture = `{"error":10,"message":"Invalid API key - You must be granted a valid key by last.fm"}`

const deezerFixture = `{
  "data": [
    {"title": "Creep", "duration": 238, "artist": {"name": "Radiohead"}, "album": {"title": "Pablo Honey"}},
    {"title": "Karma Police", "duration": 0, "artist": {"name": "Radiohead"}, "album": {"title": "OK Computer"}}
  ]
}`

func TestParseLastfmTopTracks(t *testing.T) {
	songs, err := parseLastfmTopTracks([]byte(lastfmFixture), "Radiohead")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The nameless track is dropped.
	if len(songs) != 2 {
		t.Fatalf("want 2 songs, got %d", len(songs))
	}

	first := songs[0]
	if first.Name != "Creep" {
		t.Errorf("want Creep, got %q", first.Name)
	}
	// 238 s -> 238000 ms (millisecond field takes precedence in Navidrome).
	if first.DurationMs != 238000 {
		t.Errorf("want 238000ms, got %d", first.DurationMs)
	}
	if first.MBID == "" {
		t.Error("want recording mbid propagated")
	}
	if len(first.Artists) != 1 || first.Artists[0].Name != "Radiohead" {
		t.Errorf("unexpected artists: %+v", first.Artists)
	}
	if first.Artists[0].MBID != "a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("want artist mbid propagated, got %q", first.Artists[0].MBID)
	}

	// Unknown duration must stay zero rather than becoming 0 ms of noise.
	if songs[1].DurationMs != 0 {
		t.Errorf("want 0ms for unknown duration, got %d", songs[1].DurationMs)
	}
}

func TestParseLastfmTopTracksError(t *testing.T) {
	if _, err := parseLastfmTopTracks([]byte(lastfmErrorFixture), "Radiohead"); err == nil {
		t.Fatal("want error for last.fm error payload")
	}
}

func TestParseLastfmFallbackArtistName(t *testing.T) {
	body := `{"toptracks":{"track":[{"name":"Song","artist":{"name":""}}]}}`
	songs, err := parseLastfmTopTracks([]byte(body), "Radiohead")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(songs) != 1 || songs[0].Artists[0].Name != "Radiohead" {
		t.Fatalf("want fallback to queried artist, got %+v", songs)
	}
}

func TestParseDeezerTopTracks(t *testing.T) {
	songs, err := parseDeezerTopTracks([]byte(deezerFixture), "Radiohead")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(songs) != 2 {
		t.Fatalf("want 2 songs, got %d", len(songs))
	}
	if songs[0].Album != "Pablo Honey" {
		t.Errorf("want album propagated, got %q", songs[0].Album)
	}
	if songs[0].DurationMs != 238000 {
		t.Errorf("want 238000ms, got %d", songs[0].DurationMs)
	}
}
