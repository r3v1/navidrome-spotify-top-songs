#!/bin/sh
# Build and package the plugin as an .ndp file.
#
#   ./build.sh            -> navidrome-spotify-top-songs.ndp
#   ./build.sh other.ndp  -> other.ndp
#
# Uses plain Go (wasip1) so TinyGo is not required.
set -eu

cd "$(dirname "$0")"

out="${1:-navidrome-spotify-top-songs.ndp}"

echo "==> go test"
GOFLAGS=-mod=mod go test ./...

echo "==> building plugin.wasm"
GOOS=wasip1 GOARCH=wasm GOFLAGS=-mod=mod \
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o plugin.wasm .

echo "==> packaging $out"
# Navidrome expects plugin.wasm at the zip root; the plugin id is the file name.
rm -f "$out" plugin.wasm.tmp
cp plugin.wasm plugin.wasm.tmp
mv plugin.wasm.tmp plugin.wasm
rm -f "$out"
zip -j -q "$out" manifest.json plugin.wasm

echo "==> done: $(pwd)/$out ($(du -h "$out" | cut -f1))"
echo
echo "Install:"
echo "  cp $out <navidrome-data>/plugins/"
echo "  navidrome.toml:"
echo "    [Plugins]"
echo "    Enabled = true"
echo "    Folder = \"<navidrome-data>/plugins\""
echo "    Agents = \"${out%.ndp},lastfm,deezer,listenbrainz\""
