# navidrome-spotify-top-songs

A [Navidrome](https://www.navidrome.org/) plugin that serves an artist's **"Top songs"**
from **Spotify** (or Last.fm / Deezer) instead of ListenBrainz.

Navidrome matches the results against your local library, so you only see tracks you
actually have on disk, in the order the external source ranks them.

Tested against Navidrome **0.64.2**.

## How it works

Navidrome already has a metadata *agent* system. This plugin implements the
`MetadataAgent` capability (`nd_get_artist_top_songs`) and returns song references
(title, artist, album, duration). Navidrome then matches them to your library by
MBID → ISRC → title/artist similarity.

```
Music client  ->  getTopSongs.view  ->  Navidrome  ->  agent (this plugin)
                                                          |
                                                 Spotify / Last.fm / Deezer
                                                          |
                                                core/matcher (local library)
```

### Why it does not use the official Spotify API

`GET /artists/{id}/top-tracks` was **removed** for Development Mode apps in February
2026, and Extended Quota requires an organization with 250k monthly active users.
Client Credentials needs a `client_secret`, which cannot be shipped inside a plugin.

Instead it uses the **public, unauthenticated** page
`open.spotify.com/embed/artist/{id}`, which includes the real top tracks in its
`__NEXT_DATA__` blob. The artist ID is resolved from Navidrome's `mbzArtistId` through
MusicBrainz.

## Requirements

- Navidrome **0.64.0+** with plugins enabled (Extism).
- Nothing else. The `.ndp` is prebuilt; no TinyGo or Go needed to install it.

## Installation

### 1. Copy the plugin

```bash
cp navidrome-spotify-top-songs.ndp /path/to/your/plugins/
```

The default folder is `<DataFolder>/plugins` if you do not set `Folder`.

### 2. Enable plugins in `navidrome.toml`

```toml
Agents = "navidrome-spotify-top-songs,lastfm,deezer,listenbrainz"

[Plugins]
Enabled = true
Folder = "/path/to/your/plugins"
```

> `Agents` goes at the **root** of the file, not inside `[Plugins]`. If you put it
> inside, Navidrome silently ignores it with a warning
> (`Option 'Plugins.Agents' is not recognized`).
>
> Order matters: the first agent that returns results wins. This plugin falls through
> to the next source on its own, so leaving it first is the usual choice.

### 3. Enable it in the UI

Open Navidrome → **Plugins** → `navidrome-spotify-top-songs`:

- **Enabled**: on
- **Users** and **Libraries**: grant access, or the plugin never runs
- **Config**: leave `{}` to use Spotify + Deezer, or set it as described below

The **plugin ID is the file name**. If you rename the `.ndp`, update `Agents` and
reconfigure it.

### Docker

If Navidrome runs in a container, the `.ndp` must be inside the container:

```yaml
volumes:
  - ./plugins:/data/plugins
```

## Configuration

Edited from Navidrome's plugin UI (key/value JSON).

| Key | Type | Default | Description |
|---|---|---|---|
| `sources` | string | `spotify,lastfm,deezer` | Comma separated source order. The first one with results wins. |
| `lastfm_api_key` | string | empty | Free key from [Last.fm](https://www.last.fm/api/account/create). Without it, Last.fm is skipped. |

Examples:

```
{"sources":"spotify"}
{"sources":"lastfm,deezer","lastfm_api_key":"abc123..."}
```

### Sources

| Source | Credentials | Max tracks | Notes |
|---|---|---|---|
| `spotify` | None | **10** | Real Spotify ranking. Needs MusicBrainz to resolve the artist. |
| `lastfm` | Free API key | per `count` | Requires `lastfm_api_key`. |
| `deezer` | None | per `count` | Generous limits, good fallback. |
| `listenbrainz` | — | — | Not part of this plugin: Navidrome's built-in agent. |

If a source fails (network, quota, changed format), the error is logged and the next
source is tried. A failure never breaks the artist page.

## Behaviour and limits

- **Only tracks you own appear.** If Spotify returns 10 and you have 3, you see 3.
- **Spotify is capped at 10 tracks** per artist: the embed page does not paginate.
- **Order is preserved** from the external source, not alphabetical.
- **24 h cache** per artist and source. Transient failures (e.g. a MusicBrainz 503) are
  **not** cached, so they cannot poison the result.
- MusicBrainz allows ~1 req/s; the plugin retries once on 503/429.
- Name-based resolution requires **score ≥ 90 and an exact name match**, to avoid
  attributing the wrong artist.

### Clients and the Community / Personal toggle

In the artist view, clients such as Feishin request `type=community` by default, which
is where this plugin takes effect. Switching to **Personal** makes Navidrome sort your
own tracks by local `playCount`, and the external source is not involved.

## Building from source

Requires Go 1.25+. **TinyGo is not needed.**

```bash
./build.sh
```

Produces `navidrome-spotify-top-songs.ndp`. You can pass another name:

```bash
./build.sh my-plugin.ndp
```

Manual equivalent:

```bash
GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags="-s -w" \
    -buildmode=c-shared -o plugin.wasm .
zip -j navidrome-spotify-top-songs.ndp manifest.json plugin.wasm
```

Tests:

```bash
go test ./...
```

## Permissions

Declared in `manifest.json`:

- `http` — `open.spotify.com`, `musicbrainz.org`, `ws.audioscrobbler.com`, `api.deezer.com`
- `cache` — cache per-artist lookups

Navidrome shows these permissions in the UI before the plugin can be enabled.

## Known issues

- **Fewer tracks than expected** for artists with many alternate versions (live
  recordings, radio sessions): Navidrome's matcher can drop ambiguous matches. It is
  deliberately conservative.
- **Spotify returns nothing and falls through to another source**: can happen when the
  artist has no Spotify relation in MusicBrainz and the name search is inconclusive.
- **Changes to Spotify's HTML** would break the embed parsing. In that case the plugin
  returns an error and the next source takes over; check the log with
  `LogLevel = "debug"`.

## Logs

```toml
LogLevel = "debug"

[Plugins]
LogLevel = "debug"
```

Look for:

```
Serving 10 top songs for Radiohead from spotify
spotify failed for X: ...
```

## Disclaimer

Spotify support relies on a public, undocumented page (the embed), not on the official
API. It may change or stop working without notice. Intended for personal use.

## License

GPL-3.0, same as Navidrome.
