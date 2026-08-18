#!/bin/bash
# Re-shoot every picture on the README pages.
#
# Why this is a script and not a list of steps in a document: it has been done by hand twice and
# it went wrong both times, in the same way — a picture came out carrying this machine's real
# projects, real branches, a real deploy failure and a real domain, because one shot inherited
# what the previous one had left on screen. Those files were a `git push` away from a public page.
# What protects against that is not remembering, it is the order below.
#
#   ./tools/shoot-assets.sh            # everything
#   ./tools/shoot-assets.sh sessions   # one of them, by name
#
# Everything is drawn by the app itself, offscreen, so **no Screen Recording permission is
# needed** and nothing depends on what is on your screen at the time. Needs: the app built
# (./build.sh) and ffmpeg (brew install ffmpeg).
set -euo pipefail
cd "$(dirname "$0")/.."

CFG="$HOME/.config/clawdline/config.json"
OUT="$PWD/docs/assets"
TMP="$PWD/.shoot"                    # git-ignored; frames are large and worth keeping while tuning
T="$PWD/docs/assets/demo-transcript.jsonl"
WANT="${1:-all}"

command -v ffmpeg >/dev/null || { echo "ffmpeg not found — brew install ffmpeg"; exit 1; }
mkdir -p "$TMP"

# The config is changed and put back. The pictures are English because README.md is, and the
# default text size because that is what a new install has — shooting at whatever this machine
# happens to be set to is how a page ends up documenting one person's preferences.
BACKUP="$TMP/config-before-shoot.json"
cp "$CFG" "$BACKUP"
restore() { cp "$BACKUP" "$CFG"; relaunch; echo "→ config restored"; }
trap restore EXIT

set_cfg() { python3 -c "
import json, io, sys
p = sys.argv[1]; c = json.load(io.open(p))
for kv in sys.argv[2:]:
    k, v = kv.split('=', 1)
    c[k] = json.loads(v)
json.dump(c, io.open(p, 'w'), indent=2, ensure_ascii=False)
" "$CFG" "$@"; }

relaunch() {
  osascript -e 'tell application "Clawdline" to quit' 2>/dev/null || true
  sleep 1
  open -a "$HOME/Applications/Clawdline.app" 2>/dev/null || open -a Clawdline
  sleep 4
}

# **Text goes in raw, never percent-escaped.** `open` escapes the URL on its way through, so
# anything escaped here arrives escaped twice and the bar types "%20" at you.
shot() {  # shot <name> <query> [settle]
  rm -f "$OUT/$1.png"
  open "clawdline://snapshot?path=$OUT/$1.png&routine=idle&t=0.3&$2"
  sleep "${3:-4}"
  [ -s "$OUT/$1.png" ] || { echo "!! $1 did not render"; exit 1; }
  echo "→ $1.png"
}

strip() {  # strip <name> <script> <seconds> <width> [extra query]
  rm -rf "$TMP/$1"
  open "clawdline://filmstrip?dir=$TMP/$1&script=$2&fps=24&seconds=$3${5:-}"
  # Wait for the frames rather than guessing: a short clip on a loaded machine still takes as
  # long as the machine takes, and a sleep that is usually enough is a sleep that fails on the
  # day somebody is watching.
  want_frames=$(python3 -c "print(int($3 * 24))")
  for _ in $(seq 1 60); do
    [ "$(ls "$TMP/$1" 2>/dev/null | grep -c '^f[0-9]*\.png$')" -ge "$want_frames" ] && break
    sleep 1
  done
  [ -f "$TMP/$1/f0000.png" ] || { echo "!! $1: no frames rendered"; exit 1; }
  ffmpeg -v error -y -framerate 24 -i "$TMP/$1/f%04d.png" \
    -vf "fps=24,scale=$4:-2:flags=lanczos,palettegen=stats_mode=diff" "$TMP/$1/pal.png"
  ffmpeg -v error -y -framerate 24 -i "$TMP/$1/f%04d.png" -i "$TMP/$1/pal.png" \
    -filter_complex "fps=24,scale=$4:-2:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=3" \
    -loop 0 "$OUT/$1.gif"
  echo "→ $1.gif"
}

want() { [ "$WANT" = all ] || [ "$WANT" = "$1" ]; }

# `cmd || true` was swallowing ffmpeg's exit code, so a clip that never rendered still printed a
# tick. A shooting tool that lies about what it produced is worse than one that stops.
run_if() { local n="$1"; shift; if want "$n"; then "$@"; fi; }

set_cfg 'language="en"' 'output_size=11.5' 'mascot="clawd"'
relaunch

run_if sessions   shot sessions   "list=demo"
run_if picker     shot picker     "list=mascots"
run_if transcript shot transcript "output=1&full=0&transcript=$T" 6
run_if fullscreen shot fullscreen "output=1&full=1&transcript=$T" 7

# The notch is three states and one strip, left-aligned so the stand-in cutout stays put while
# the ears grow around it.
if want island; then
  for m in working2 waiting finished; do
    rm -f "$TMP/island-$m.png"
    open "clawdline://snapshot?path=$TMP/island-$m.png&island=$m"; sleep 3
  done
  W=$(for f in "$TMP"/island-*.png; do sips -g pixelWidth "$f" | tail -1 | awk '{print $2}'; done | sort -n | tail -1)
  H=$(sips -g pixelHeight "$TMP/island-waiting.png" | tail -1 | awk '{print $2}')
  ffmpeg -v error -y \
    -loop 1 -t 1.8 -i "$TMP/island-working2.png" \
    -loop 1 -t 2.4 -i "$TMP/island-waiting.png" \
    -loop 1 -t 2.0 -i "$TMP/island-finished.png" \
    -filter_complex "[0]pad=$W:$H:0:0:0x6b6b6b[a];[1]pad=$W:$H:0:0:0x6b6b6b[b];[2]pad=$W:$H:0:0:0x6b6b6b[c];[a][b][c]concat=n=3:v=1:a=0,fps=12,scale=760:-2:flags=lanczos,split[x][y];[y]palettegen=stats_mode=diff[p];[x][p]paletteuse=dither=bayer:bayer_scale=3" \
    -loop 0 "$OUT/island.gif"
  echo "→ island.gif"
fi

run_if demo    strip demo    demo    4.4 760 "&text=add retry with backoff to the upload handler"
run_if dance   strip dance   dance   2   420
run_if stretch strip stretch stretch 2   420

if want voice; then
  strip voice voice 7.5 760 "&text=cambia el retry a exponential backoff|cambia el retry a exponential backoff, y después corre los tests."
fi

# The second pack, for the gallery pair. Language stays English; only the character changes.
if want mochi-dance; then
  set_cfg 'mascot="mochi"'; relaunch
  strip mochi-dance dance 2 420
  set_cfg 'mascot="clawd"'; relaunch
fi

# The Chinese page has its own voice clip, because the words being spoken are the point of it.
if want voice.zh; then
  set_cfg 'language="zh-Hant"'; relaunch
  # Raw UTF-8 rather than percent-escaped: `open` escapes the URL on its way through, so anything
  # escaped here arrives escaped twice and the bar types "%E6%8A%8A" at you.
  strip voice.zh voice 7.5 760 "&text=把那個 webhook 的 retry 改成 exponential backoff|把那個 webhook 的 retry 改成 exponential backoff，然後跑一次測試。"
fi

echo
echo "Now look at them — ./tools/check-assets.sh — and do not skip it."
