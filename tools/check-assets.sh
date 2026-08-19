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
# The session list is no longer among them: what that section illustrates is states changing, so
# it is a clip now and is checked with the clips below.
for f in picker transcript fullscreen; do
  [ -f "$OUT/$f.png" ] || continue
  W=$(sips -g pixelWidth "$OUT/$f.png" | tail -1 | awk '{print $2}')
  H=$(sips -g pixelHeight "$OUT/$f.png" | tail -1 | awk '{print $2}')
  # The footer, and the last line of content above it — a transcript leaks through there.
  ffmpeg -v error -y -i "$OUT/$f.png" -vf "crop=$W:220:0:$((H-225)),scale=1100:-1" -frames:v 1 "$TMP/$f.png"
done

# The browser pictures carry their identity somewhere else: not in a footer, but in the session
# list — the project names, the paths under them and the task titles.
#
# What these must show is the page's **own fixtures** — atrium, clawdline, notebook, and paths under
# `/Users/x/`. A path under your home directory, or a project you recognise, means the picture was
# taken against a live server rather than against the fixtures, and must not be committed.
#
# They are phone-shaped, so they go into the sheet scaled to the same height as everything else
# and padded out to its width rather than scaled to it — a 390-wide picture stretched to 1100 is
# two thousand pixels of column nobody can take in.
band() {  # band <in> <out> <filters before the pad>
  ffmpeg -v error -y -i "$1" -vf "$3,pad=1100:520:(ow-iw)/2:0:0x101010" -frames:v 1 "$2"
}

if [ -f "$OUT/web-wide.png" ]; then
  band "$OUT/web-wide.png" "$TMP/web-wide.png" "scale=1100:-1,crop=420:520:0:56"
fi
for g in web web-push; do
  [ -f "$OUT/$g.gif" ] || continue
  ffmpeg -v error -y -i "$OUT/$g.gif" -vf "select='eq(n\,12)',scale=-2:520,pad=1100:520:(ow-iw)/2:0:0x101010" \
    -frames:v 1 "$TMP/web-$g.png"
done

# Two frames from every clip, side by side: an early one and a late one.
#
# **One frame cannot answer the question this sheet asks of a clip.** The picture that prompted
# this rule was `picker-live.gif`, whose first frame was correct and whose second half was not in
# the file at all — the encode had stopped part way through, so the GIF was two seconds of the
# character that does not change. Anything read from the opening of a clip is a claim about the
# opening of a clip. The late frame is where a state that was supposed to change has to have
# changed, and it is also the half a leak is most likely to be hiding in, because it is the half
# nobody looks at.
for g in sessions sessions-live picker-live demo island dance mochi-dance stretch voice voice.zh; do
  [ -f "$OUT/$g.gif" ] || continue
  n=$(ffprobe -v error -select_streams v -count_frames -show_entries stream=nb_read_frames \
        -of csv=p=0 "$OUT/$g.gif")
  late=$(( n * 4 / 5 ))
  ffmpeg -v error -y -i "$OUT/$g.gif" \
    -vf "select='eq(n\,8)+eq(n\,$late)',scale=548:-2,tile=2x1:padding=4:color=0x101010,pad=1100:ih:(ow-iw)/2:0:0x101010" \
    -frames:v 1 "$TMP/gif-$g.png"
done

FILES=("$TMP"/*.png)
ffmpeg -v error -y $(printf -- '-i %s ' "${FILES[@]}") \
  -filter_complex "$(for i in $(seq 0 $(( ${#FILES[@]} - 1 ))); do printf '[%d]' "$i"; done)vstack=inputs=${#FILES[@]}" \
  -frames:v 1 "$TMP/sheet.png"

echo "→ $TMP/sheet.png"
echo
echo "Look at it. Every footer must read 'my-project', and the only conversation in there"
echo "must be the made-up one. If you see a repository you recognise, do not commit."
echo
echo "The browser pictures are held to the same rule: the only projects in the session list may"
echo "be the page's own fixtures, and the only notification may be one the app actually sent."
