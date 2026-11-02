package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/types"
)

// Spotify's Web API removed GET /artists/{id}/top-tracks for development-mode apps
// (Feb 2026), and client-credentials needs a secret that cannot ship in a plugin.
// The public artist embed page, however, is unauthenticated and ships the artist's
// top tracks as JSON in its __NEXT_DATA__ blob. It is capped at 10 tracks.
const (
	spotifyEmbedURL      = "https://open.spotify.com/embed/artist/%s"
	musicbrainzArtistURL = "https://musicbrainz.org/ws/2/artist/%s?inc=url-rels&fmt=json"
	musicbrainzSearchURL = "https://musicbrainz.org/ws/2/artist?query=%s&fmt=json&limit=3"
	// MusicBrainz asks for ~1 request/second and a descriptive agent.
	musicbrainzUA = "NavidromeTopSongsPlugin/1.0 ( https://github.com/navidrome/navidrome )"
	minMatchScore = 90
)

var (
	nextDataRe      = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
	spotifyArtistRe = regexp.MustCompile(`open\.spotify\.com/artist/([0-9A-Za-z]{22})`)
)

type spotifyEmbed struct {
	Props struct {
		PageProps struct {
			State *struct {
				Data struct {
					Entity *struct {
						Name      string `json:"name"`
						TrackList []struct {
							Title    string `json:"title"`
							Subtitle string `json:"subtitle"`
							Duration int    `json:"duration"` // milliseconds
						} `json:"trackList"`
					} `json:"entity"`
				} `json:"data"`
			} `json:"state"`
		} `json:"pageProps"`
	} `json:"props"`
}

func spotifyTopSongs(artistName, mbid string, count int) ([]types.SongRef, error) {
	artistID, err := resolveSpotifyArtistID(artistName, mbid)
	if err != nil {
		return nil, err
	}
	if artistID == "" {
		return nil, nil
	}

	body, err := httpGet(fmt.Sprintf(spotifyEmbedURL, artistID))
	if err != nil {
		return nil, err
	}

	songs, err := parseSpotifyEmbed(body, artistName)
	if err != nil {
		return nil, err
	}
	if len(songs) > count {
		songs = songs[:count]
	}
	return songs, nil
}

func parseSpotifyEmbed(body []byte, fallbackArtist string) ([]types.SongRef, error) {
	m := nextDataRe.FindSubmatch(body)
	if m == nil {
		return nil, errors.New("spotify: no __NEXT_DATA__ in embed page")
	}

	var embed spotifyEmbed
	if err := json.Unmarshal(m[1], &embed); err != nil {
		return nil, fmt.Errorf("spotify: invalid embed payload: %w", err)
	}
	// An unknown artist id still returns 200, just without state.
	if embed.Props.PageProps.State == nil || embed.Props.PageProps.State.Data.Entity == nil {
		return nil, nil
	}

	entity := embed.Props.PageProps.State.Data.Entity
	artist := strings.TrimSpace(entity.Name)
	if artist == "" {
		artist = fallbackArtist
	}

	songs := make([]types.SongRef, 0, len(entity.TrackList))
	for _, t := range entity.TrackList {
		if strings.TrimSpace(t.Title) == "" {
			continue
		}
		// subtitle carries the credited artist, which may differ from the queried one.
		credit := strings.TrimSpace(t.Subtitle)
		if credit == "" {
			credit = artist
		}
		songs = append(songs, types.SongRef{
			Artists:    []types.ArtistRef{{Name: credit}},
			DurationMs: uint32(max(t.Duration, 0)),
			Name:       strings.TrimSpace(t.Title),
		})
	}
	return songs, nil
}

// resolveSpotifyArtistID maps a MusicBrainz artist id (or, failing that, a name) to
// a Spotify artist id. MusicBrainz stores Spotify as an official artist URL relation.
func resolveSpotifyArtistID(artistName, mbid string) (string, error) {
	key := "spotify:artist:" + strings.ToLower(strings.TrimSpace(mbid+"|"+artistName))
	if cached, found, err := host.CacheGetString(key); err == nil && found {
		return cached, nil
	}

	id, err := lookupSpotifyArtistID(artistName, mbid)
	if err != nil {
		// Do not cache transient failures: a 503 from MusicBrainz must not pin a
		// miss for a day.
		return "", err
	}

	_ = host.CacheSetString(key, id, cacheTTL)
	return id, nil
}

func lookupSpotifyArtistID(artistName, mbid string) (string, error) {
	if mbid != "" {
		if id, err := spotifyIDFromMBID(mbid); err != nil {
			return "", err
		} else if id != "" {
			return id, nil
		}
	}
	if artistName == "" {
		return "", nil
	}

	mbid, err := searchMusicBrainzArtist(artistName)
	if err != nil || mbid == "" {
		return "", err
	}
	return spotifyIDFromMBID(mbid)
}

func spotifyIDFromMBID(mbid string) (string, error) {
	body, err := httpGetWithUA(fmt.Sprintf(musicbrainzArtistURL, url.PathEscape(mbid)), musicbrainzUA)
	if err != nil {
		return "", err
	}

	var resp struct {
		Relations []struct {
			Type string `json:"type"`
			URL  struct {
				Resource string `json:"resource"`
			} `json:"url"`
		} `json:"relations"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("musicbrainz: invalid artist payload: %w", err)
	}

	for _, rel := range resp.Relations {
		if m := spotifyArtistRe.FindStringSubmatch(rel.URL.Resource); m != nil {
			return m[1], nil
		}
	}
	return "", nil
}

func searchMusicBrainzArtist(artistName string) (string, error) {
	query := url.QueryEscape(fmt.Sprintf(`artist:"%s"`, artistName))
	body, err := httpGetWithUA(fmt.Sprintf(musicbrainzSearchURL, query), musicbrainzUA)
	if err != nil {
		return "", err
	}

	var resp struct {
		Artists []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Score int    `json:"score"`
		} `json:"artists"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("musicbrainz: invalid search payload: %w", err)
	}

	for _, a := range resp.Artists {
		// A loose match would map the wrong artist to Spotify.
		if a.Score >= minMatchScore && strings.EqualFold(a.Name, artistName) {
			return a.ID, nil
		}
	}
	return "", nil
}

func httpGet(endpoint string) ([]byte, error) {
	return httpGetWithUA(endpoint, userAgent)
}

func httpGetWithUA(endpoint, ua string) ([]byte, error) {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       endpoint,
		Headers:   map[string]string{"Accept": "text/html,application/json", "User-Agent": ua},
		TimeoutMs: 10000,
	})
	if err != nil {
		return nil, err
	}
	// MusicBrainz throttles bursts with 503/429; one quick retry usually clears it.
	if resp.StatusCode == 429 || resp.StatusCode == 503 {
		retry, retryErr := host.HTTPSend(host.HTTPRequest{
			Method:    "GET",
			URL:       endpoint,
			Headers:   map[string]string{"Accept": "text/html,application/json", "User-Agent": ua},
			TimeoutMs: 10000,
		})
		if retryErr == nil && retry.StatusCode >= 200 && retry.StatusCode < 300 {
			return retry.Body, nil
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	return resp.Body, nil
}
