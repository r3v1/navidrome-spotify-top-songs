package main

import (
	"testing"
)

// Trimmed from a real https://open.spotify.com/embed/artist/{id} render.
const spotifyEmbedFixture = `<!DOCTYPE html><html><head><title>Spotify Embed</title></head><body>
<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"state":{"data":{"entity":{"type":"artist","name":"Radiohead","uri":"spotify:artist:4Z8W4fKeB5YxbusRsdQVPb","title":"Radiohead","trackList":[{"uri":"spotify:track:70LcF31zb1H0PyJoS1Sx1r","title":"Creep","subtitle":"Radiohead","duration":238640,"isPlayable":true},{"uri":"spotify:track:2fuYa3Lx06QQJAm0MjztKr","title":"Let Down","subtitle":"Radiohead","duration":299000},{"uri":"spotify:track:x","title":"  ","subtitle":"Radiohead","duration":1000}]}}}}}}</script>
</body></html>`

const spotifyEmbedMissingFixture = `<html><body><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"error":{"status":404}}}}</script></body></html>`

const musicbrainzRelationsFixture = `{
  "id": "a74b1b7f-71a5-4011-9441-d0b5e4122711",
  "relations": [
    {"type": "official homepage", "url": {"resource": "https://www.radiohead.com/"}},
    {"type": "streaming", "url": {"resource": "https://open.spotify.com/artist/4Z8W4fKeB5YxbusRsdQVPb"}}
  ]
}`

const musicbrainzSearchFixture = `{
  "artists": [
    {"id": "wrong", "name": "Radiohead Tribute Band", "score": 100},
    {"id": "a74b1b7f-71a5-4011-9441-d0b5e4122711", "name": "Radiohead", "score": 100}
  ]
}`

func TestParseSpotifyEmbed(t *testing.T) {
	songs, err := parseSpotifyEmbed([]byte(spotifyEmbedFixture), "Radiohead")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The blank-title entry is dropped.
	if len(songs) != 2 {
		t.Fatalf("want 2 songs, got %d", len(songs))
	}
	if songs[0].Name != "Creep" {
		t.Errorf("want Creep, got %q", songs[0].Name)
	}
	// Spotify reports milliseconds already.
	if songs[0].DurationMs != 238640 {
		t.Errorf("want 238640ms, got %d", songs[0].DurationMs)
	}
	if len(songs[0].Artists) != 1 || songs[0].Artists[0].Name != "Radiohead" {
		t.Errorf("unexpected artists: %+v", songs[0].Artists)
	}
}

func TestParseSpotifyEmbedUnknownArtist(t *testing.T) {
	songs, err := parseSpotifyEmbed([]byte(spotifyEmbedMissingFixture), "Nobody")
	if err != nil {
		t.Fatalf("unknown artist must not be an error, got: %v", err)
	}
	if len(songs) != 0 {
		t.Fatalf("want no songs, got %d", len(songs))
	}
}

func TestParseSpotifyEmbedNoNextData(t *testing.T) {
	if _, err := parseSpotifyEmbed([]byte("<html></html>"), "X"); err == nil {
		t.Fatal("want error when the page shape changes")
	}
}

func TestSpotifyArtistRegex(t *testing.T) {
	m := spotifyArtistRe.FindStringSubmatch("https://open.spotify.com/artist/4Z8W4fKeB5YxbusRsdQVPb?si=abc")
	if m == nil || m[1] != "4Z8W4fKeB5YxbusRsdQVPb" {
		t.Fatalf("failed to extract artist id: %v", m)
	}
	// Non-22-char ids must not match.
	if spotifyArtistRe.MatchString("https://open.spotify.com/artist/bogus") {
		t.Fatal("matched an invalid artist id")
	}
}
