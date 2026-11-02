// Navidrome metadata agent plugin: artist top songs from Spotify (or Last.fm / Deezer).
//
// Navidrome resolves agent output against the local library itself (core/matcher),
// so this plugin only has to return song references: title, artist and album.
// Returning nil with a nil error means "no data", which makes Navidrome move on to
// the next configured agent instead of retrying.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/metadata"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/navidrome/navidrome/plugins/pdk/go/types"
)

const (
	defaultSources = "spotify,lastfm,deezer"
	defaultCount   = 50
	cacheTTL       = 60 * 60 * 24 // 1 day
	userAgent      = "NavidromeTopSongsPlugin/1.0"
)

type plugin struct{}

func init() {
	metadata.Register(&plugin{})
}

func main() {}

var (
	_ metadata.ArtistTopSongsProvider = (*plugin)(nil)
)

// GetArtistTopSongs returns the artist's most popular tracks according to the
// configured sources, in order. Errors are returned only for real failures:
// "no data" is a nil response with a nil error so the next agent gets a turn.
func (p *plugin) GetArtistTopSongs(req metadata.TopSongsRequest) (*metadata.TopSongsResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, nil
	}

	count := int(req.Count)
	if count <= 0 {
		count = defaultCount
	}

	for _, source := range configuredSources() {
		var (
			songs []types.SongRef
			err   error
		)

		switch source {
		case "lastfm":
			apiKey, _ := pdk.GetConfig("lastfm_api_key")
			if apiKey == "" {
				continue
			}
			songs, err = topSongsFromCache(source, name, count, func() ([]types.SongRef, error) {
				return lastfmTopSongs(apiKey, name, count)
			})
		case "deezer":
			songs, err = topSongsFromCache(source, name, count, func() ([]types.SongRef, error) {
				return deezerTopSongs(name, count)
			})
		case "spotify":
			songs, err = topSongsFromCache(source, name, count, func() ([]types.SongRef, error) {
				return spotifyTopSongs(name, req.MBID, count)
			})
		default:
			pdk.Log(pdk.LogDebug, "Unknown source in sources config: "+source)
			continue
		}

		if err != nil {
			// A failing source must not block the next one.
			pdk.Log(pdk.LogInfo, fmt.Sprintf("%s failed for %s: %v", source, name, err))
			continue
		}
		if len(songs) > 0 {
			pdk.Log(pdk.LogDebug, fmt.Sprintf("Serving %d top songs for %s from %s", len(songs), name, source))
			return &metadata.TopSongsResponse{Songs: songs}, nil
		}
	}

	return nil, nil
}

func configuredSources() []string {
	raw, ok := pdk.GetConfig("sources")
	if !ok || strings.TrimSpace(raw) == "" {
		raw = defaultSources
	}

	var sources []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			sources = append(sources, s)
		}
	}
	if len(sources) == 0 {
		return []string{defaultSources}
	}
	return sources
}

// topSongsFromCache memoises a source lookup. Last.fm and Deezer are rate limited
// and artist pages are revisited often, so a miss is cached too (as an empty list).
func topSongsFromCache(source, artist string, count int, fetch func() ([]types.SongRef, error)) ([]types.SongRef, error) {
	key := fmt.Sprintf("%s:%d:%s", source, count, strings.ToLower(artist))

	if raw, found, err := host.CacheGetString(key); err == nil && found {
		var cached []types.SongRef
		if json.Unmarshal([]byte(raw), &cached) == nil {
			return cached, nil
		}
	}

	songs, err := fetch()
	if err != nil {
		return nil, err
	}

	if encoded, err := json.Marshal(songs); err == nil {
		_ = host.CacheSetString(key, string(encoded), cacheTTL)
	}
	return songs, nil
}

type lastfmTopTracks struct {
	Tracks struct {
		Track []struct {
			Name      string `json:"name"`
			Duration  string `json:"duration"`
			MBID      string `json:"mbid"`
			Artist    lastfmName
			Listeners string `json:"listeners"`
		} `json:"track"`
	} `json:"toptracks"`
	Message string `json:"message"`
	Error   int    `json:"error"`
}

type lastfmName struct {
	Name string `json:"name"`
	MBID string `json:"mbid"`
}

func lastfmTopSongs(apiKey, artist string, count int) ([]types.SongRef, error) {
	endpoint := fmt.Sprintf(
		"https://ws.audioscrobbler.com/2.0/?method=artist.gettoptracks&artist=%s&limit=%d&format=json&api_key=%s",
		url.QueryEscape(artist), count, url.QueryEscape(apiKey),
	)

	body, err := httpGetJSON(endpoint)
	if err != nil {
		return nil, err
	}

	return parseLastfmTopTracks(body, artist)
}

func parseLastfmTopTracks(body []byte, artist string) ([]types.SongRef, error) {
	var resp lastfmTopTracks
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("invalid Last.fm response: %w", err)
	}
	if resp.Error != 0 {
		return nil, fmt.Errorf("last.fm error %d: %s", resp.Error, resp.Message)
	}

	songs := make([]types.SongRef, 0, len(resp.Tracks.Track))
	for _, t := range resp.Tracks.Track {
		if t.Name == "" {
			continue
		}

		artistName := t.Artist.Name
		if artistName == "" {
			artistName = artist
		}

		song := types.SongRef{
			Name:    t.Name,
			Artists: []types.ArtistRef{{Name: artistName, MBID: t.Artist.MBID}},
			MBID:    t.MBID,
		}
		// Last.fm reports duration in seconds, as a string; "" when unknown.
		if seconds, err := parseSeconds(t.Duration); err == nil {
			song.DurationMs = seconds * 1000
		}
		songs = append(songs, song)
	}
	return songs, nil
}

type deezerTopTracks struct {
	Error *struct{ Message string } `json:"error"`
	Data  []deezerTrack             `json:"data"`
}

type deezerTrack struct {
	Title    string `json:"title"`
	Duration int    `json:"duration"`
	Artist   struct {
		Name string `json:"name"`
	} `json:"artist"`
	Album struct {
		Title string `json:"title"`
	} `json:"album"`
}

func deezerTopSongs(artist string, count int) ([]types.SongRef, error) {
	// Deezer has no MBID lookup here, so resolve the name to an artist id first.
	searchURL := fmt.Sprintf("https://api.deezer.com/search/artist?q=%s&limit=1", url.QueryEscape(artist))
	body, err := httpGetJSON(searchURL)
	if err != nil {
		return nil, err
	}

	var search struct {
		Error *struct{ Message string } `json:"error"`
		Data  []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &search); err != nil {
		return nil, fmt.Errorf("invalid Deezer search response: %w", err)
	}
	if search.Error != nil {
		return nil, errors.New("deezer: " + search.Error.Message)
	}
	if len(search.Data) == 0 {
		return nil, nil
	}

	endpoint := fmt.Sprintf("https://api.deezer.com/artist/%d/top?limit=%d", search.Data[0].ID, count)
	body, err = httpGetJSON(endpoint)
	if err != nil {
		return nil, err
	}

	return parseDeezerTopTracks(body, artist)
}

func parseDeezerTopTracks(body []byte, artist string) ([]types.SongRef, error) {
	var top deezerTopTracks
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("invalid Deezer top tracks response: %w", err)
	}
	if top.Error != nil {
		return nil, errors.New("deezer: " + top.Error.Message)
	}

	songs := make([]types.SongRef, 0, len(top.Data))
	for _, t := range top.Data {
		if t.Title == "" {
			continue
		}
		artistName := t.Artist.Name
		if artistName == "" {
			artistName = artist
		}
		songs = append(songs, types.SongRef{
			Album:      t.Album.Title,
			Artists:    []types.ArtistRef{{Name: artistName}},
			DurationMs: uint32(t.Duration) * 1000,
			Name:       t.Title,
		})
	}
	return songs, nil
}

func httpGetJSON(endpoint string) ([]byte, error) {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       endpoint,
		Headers:   map[string]string{"Accept": "application/json", "User-Agent": userAgent},
		TimeoutMs: 10000,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
	}
	return resp.Body, nil
}

func parseSeconds(raw string) (uint32, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0, errors.New("no duration")
	}
	var seconds uint32
	if _, err := fmt.Sscanf(raw, "%d", &seconds); err != nil {
		return 0, err
	}
	return seconds, nil
}
