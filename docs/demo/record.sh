#!/usr/bin/env bash
# Regenerates docs/demo/tizen-cert.{gif,mp4} and docs-site/demo assets.
# Requires: asciinema, agg, ffmpeg, expect (macOS built-in).
set -euo pipefail
cd "$(dirname "$0")/../.."

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

# Build a demo binary. DEMO_PATH hides `open` from the recorded process so
# the sign-in step cannot launch a browser mid-recording (sdb stays visible
# if installed); the recording tools keep the full PATH.
go build -o "$workdir/tizen-cert" .
if command -v sdb >/dev/null; then ln -s "$(command -v sdb)" "$workdir/sdb"; fi
export DEMO_PATH="$workdir:/usr/bin:/bin"

asciinema rec --window-size 118x24 --overwrite \
  --command "expect docs/demo/demo.exp $workdir/tizen-cert" "$workdir/demo.cast"

agg --font-size 16 \
  --theme "0a1412,d7e6e2,122220,ef7466,2fd4b2,e8b649,5c9ce6,b48ead,2fd4b2,d7e6e2" \
  "$workdir/demo.cast" docs/demo/tizen-cert.gif

ffmpeg -y -i docs/demo/tizen-cert.gif -movflags faststart -pix_fmt yuv420p \
  -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" docs/demo/tizen-cert.mp4
cp docs/demo/tizen-cert.mp4 docs-site/demo/
ffmpeg -y -i docs/demo/tizen-cert.mp4 -ss 00:00:16 -frames:v 1 -q:v 3 docs-site/demo/tizen-cert.jpg
echo "demo assets regenerated"
