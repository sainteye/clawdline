#!/bin/bash
# Look for this machine's real life inside the README pictures.
#
# Twice now a picture came out carrying a real project name, a real branch, a real deploy failure
# and a real domain, because a shot inherited state from the one before it. Both times the file
# looked fine at a glance and both times it was caught by cropping the footer and looking. So:
# this crops the parts that carry identity out of every asset and stacks them into one sheet.
#
# **It cannot pass or fail on its own — you have to look at the sheet.** What it is looking for
# is a real repository name where "my-project" should be, and any conversation that is not the
# made-up one in docs/assets/demo-transcript.jsonl.
set -euo pipefail
cd "$(dirname "$0")/.."

OUT="$PWD/docs/assets"
TMP="$PWD/.shoot/check"
mkdir -p "$TMP"
rm -f "$TMP"/*.png

command -v ffmpeg >/dev/null || { echo "ffmpeg not found — brew install ffmpeg"; exit 1; }

# The footer is the line that names the project, so it is the band worth cropping out of a still.
for f in sessions picker transcript fullscreen; do
  [ -f "$OUT/$f.png" ] || continue
  W=$(sips -g pixelWidth "$OUT/$f.png" | tail -1 | awk '{print $2}')
  H=$(sips -g pixelHeight "$OUT/$f.png" | tail -1 | awk '{print $2}')
  # The footer, and the last line of content above it — a transcript leaks through there.
  ffmpeg -v error -y -i "$OUT/$f.png" -vf "crop=$W:220:0:$((H-225)),scale=1100:-1" -frames:v 1 "$TMP/$f.png"
done

# One frame from the middle of every clip.
for g in demo island dance mochi-dance stretch voice voice.zh; do
  [ -f "$OUT/$g.gif" ] || continue
  ffmpeg -v error -y -i "$OUT/$g.gif" -vf "select='eq(n\,12)',scale=1100:-1" -frames:v 1 "$TMP/gif-$g.png"
done

FILES=("$TMP"/*.png)
ffmpeg -v error -y $(printf -- '-i %s ' "${FILES[@]}") \
  -filter_complex "$(for i in $(seq 0 $(( ${#FILES[@]} - 1 ))); do printf '[%d]' "$i"; done)vstack=inputs=${#FILES[@]}" \
  -frames:v 1 "$TMP/sheet.png"

echo "→ $TMP/sheet.png"
echo
echo "Look at it. Every footer must read 'my-project', and the only conversation in there"
echo "must be the made-up one. If you see a repository you recognise, do not commit."
