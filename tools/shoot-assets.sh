#!/bin/bash
# Re-shoot every picture on the README pages.
#
# Why this is a script and not a list of steps in a document: it has been done by hand twice and
# it went wrong both times, in the same way — a picture came out carrying this machine's real
# projects, real branches, a real deploy failure and a real domain, because one shot inherited
# what the previous one had left on screen. Those files were a `git push` away from a public page.
# What protects against that is not remembering, it is the order below.
#
#   ./tools/shoot-assets.sh                     # everything
#   ./tools/shoot-assets.sh sessions            # one of them, by name
#   ./tools/shoot-assets.sh web web-wide        # or several, which costs one restart and not two
#
# The browser ones are `web`, `web-wide`, `fleet-phone`, `fleet-wide`, `tabs` and `web-push`; see
# the note further down. `tabs` is the odd one out and says so where it is defined: it is the only
# picture on the page that is a drawing rather than a shot of something running.
#
# Everything is drawn by the app itself, offscreen, so **no Screen Recording permission is
# needed** and nothing depends on what is on your screen at the time. Needs: the app built
# (./build.sh) and ffmpeg (brew install ffmpeg).
#
# Two of them are not the app. `web`, `web-wide` and `web-push` are pictures of the page you reach
# from a phone, so they are drawn by Chrome instead — driven over the DevTools protocol by
# tools/shoot-web.js, in a throwaway profile, with the frames coming from the renderer rather than
# from the screen. Same property: nothing on your display gets into them. Those three additionally
# need node (brew install node) and Google Chrome, and `web-push` needs the app running with
# Settings → Remote → Answer over HTTP on, because the notification in it is a real one.
set -euo pipefail
cd "$(dirname "$0")/.."

CFG="$HOME/.config/clawdline/config.json"
OUT="$PWD/docs/assets"
TMP="$PWD/.shoot"                    # git-ignored; frames are large and worth keeping while tuning
T="$PWD/docs/assets/demo-transcript.jsonl"
WANT=("$@")
[ ${#WANT[@]} -eq 0 ] && WANT=(all)

command -v ffmpeg >/dev/null || { echo "ffmpeg not found — brew install ffmpeg"; exit 1; }
mkdir -p "$TMP"

# The config is changed and put back. The pictures are English because README.md is, and the
# default text size because that is what a new install has — shooting at whatever this machine
# happens to be set to is how a page ends up documenting one person's preferences.
BACKUP="$TMP/config-before-shoot.json"

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

strip() {  # strip <name> <script> <seconds> <width> [extra query] [fps]
  local fps="${6:-24}"
  rm -rf "$TMP/$1"
  open "clawdline://filmstrip?dir=$TMP/$1&script=$2&fps=$fps&seconds=$3${5:-}"
  # Wait for the frames rather than guessing: a short clip on a loaded machine still takes as
  # long as the machine takes, and a sleep that is usually enough is a sleep that fails on the
  # day somebody is watching.
  want_frames=$(python3 -c "print(int($3 * $fps))")
  for _ in $(seq 1 60); do
    [ "$(ls "$TMP/$1" 2>/dev/null | grep -c '^f[0-9]*\.png$')" -ge "$want_frames" ] && break
    sleep 1
  done
  [ -f "$TMP/$1/f0000.png" ] || { echo "!! $1: no frames rendered"; exit 1; }
  # **Count the frames that came back, and do not accept fewer than were asked for.** This is
  # what `picker-live.gif` needed and did not have: ffmpeg's image-sequence reader stopped part
  # way through, wrote a GIF of the first half of the clip, and the run carried on. The file
  # existed, its first frame was right, and the half of the clip in which the thing being
  # demonstrated actually happens was simply not in it.
  got=$(ls "$TMP/$1" | grep -c '^f[0-9]*\.png$')
  [ "$got" -ge "$want_frames" ] || { echo "!! $1: $got frames, wanted $want_frames"; exit 1; }
  ffmpeg -v error -y -framerate "$fps" -i "$TMP/$1/f%04d.png" \
    -vf "fps=$fps,scale=$4:-2:flags=lanczos,palettegen=stats_mode=diff" "$TMP/$1/pal.png"
  ffmpeg -v error -y -framerate "$fps" -i "$TMP/$1/f%04d.png" -i "$TMP/$1/pal.png" \
    -filter_complex "fps=$fps,scale=$4:-2:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=3" \
    -loop 0 "$OUT/$1.gif"
  # And the same question asked of the GIF, because the two ffmpeg runs above are exactly where
  # the frames went missing last time.
  out_frames=$(ffprobe -v error -select_streams v -count_frames \
    -show_entries stream=nb_read_frames -of csv=p=0 "$OUT/$1.gif")
  [ "$out_frames" -ge "$want_frames" ] || {
    echo "!! $1.gif has $out_frames of $want_frames frames — the encode dropped the rest"; exit 1; }
  echo "→ $1.gif ($out_frames frames)"
}

# ---- the two pictures that are of a browser --------------------------------
#
# The interface you reach from a phone, and the notification that lands on it, are not AppKit and
# cannot come out of `clawdline://filmstrip`. They come from Chrome instead, driven over the
# DevTools protocol by tools/shoot-web.js — same shape as `strip` above: something plays a fixed
# storyboard, writes `f0000.png…`, and ffmpeg turns them into a GIF. No Screen Recording
# permission there either; the frames come from the renderer, not from the screen.

WEB_PAGE="file://$PWD/Resources/web/index.html?write=1"

need_node() { command -v node >/dev/null || { echo "!! node not found — the browser pictures need it (brew install node)"; exit 1; }; }

webstrip() {  # webstrip <name> <url> <script> <width> <fps> [extra args for shoot-web.js…]
  need_node
  local name="$1" url="$2" script="$3" width="$4" fps="$5"; shift 5
  rm -rf "$TMP/$name"
  node tools/shoot-web.js --url "$url" --script "$script" --dir "$TMP/$name" --fps "$fps" "$@"
  [ -f "$TMP/$name/f0000.png" ] || { echo "!! $name: no frames rendered"; exit 1; }
  # Two things about this line are not stylistic.
  #
  # **One pass, with `split`, rather than a palette written to a file and read back.** The
  # two-input form drops every frame after about a second whenever the clip holds still — and a
  # notification that sits on screen to be read is nothing but held frames, so it came out as a
  # one-second clip of a six-second event. The single graph keeps them.
  #
  # **`dither=none`.** The app's own clips are a drawing with gradients in it and want the ordered
  # dither; this is flat interface colour and type, where a dither adds nothing anybody can see
  # and about a third to the file.
  ffmpeg -v error -y -framerate "$fps" -i "$TMP/$name/f%04d.png" \
    -filter_complex "fps=$fps,scale=$width:-2:flags=lanczos,split[x][y];[y]palettegen=stats_mode=diff[p];[x][p]paletteuse=dither=none" \
    -loop 0 "$OUT/$name.gif"
  # And the same question `strip` asks of its own output, for the same reason — with one word
  # changed. **A GIF encoder drops a frame identical to the one before it and does not hand the
  # time back**, so a clip whose page holds still for a beat comes out shorter than it was played,
  # with the pauses eaten unevenly. That is a fault in the page rather than in the encode: give it
  # something that moves and the frames come back. So this says the number rather than stopping on
  # it, and the difference is the thing to go and look at.
  local want out
  want=$(ls "$TMP/$name"/f*.png | wc -l | tr -d ' ')
  out=$(ffprobe -v error -count_frames -select_streams v:0 \
        -show_entries stream=nb_read_frames -of csv=p=0 "$OUT/$name.gif")
  echo "→ $name.gif ($out frames)"
  [ "$out" -lt "$want" ] && echo "   ·· $((want - out)) of $want held frames were dropped as duplicates — the clip plays short"
  true
}

webshot() {  # webshot <name> <url> <script> [extra args…]
  need_node
  local name="$1" url="$2" script="$3"; shift 3
  rm -f "$OUT/$name.png"
  node tools/shoot-web.js --url "$url" --script "$script" --png "$OUT/$name.png" "$@"
  [ -s "$OUT/$name.png" ] || { echo "!! $name did not render"; exit 1; }
  echo "→ $name.png"
}

want() {
  local w
  for w in "${WANT[@]}"; do
    if [ "$w" = all ] || [ "$w" = "$1" ]; then return 0; fi
  done
  return 1
}

# Which of the wanted pictures need the app at all. `web` and `web-wide` are shot from a `file://`
# copy of the page, so they neither read the config nor talk to anything — and asking only for
# those should not put somebody's Mac into English, restart it twice and put it back.
needs_app() {
  local w
  for w in "${WANT[@]}"; do
    case "$w" in web|web-wide|fleet-phone|fleet-wide|tabs) ;; *) return 0 ;; esac
  done
  return 1
}

# `cmd || true` was swallowing ffmpeg's exit code, so a clip that never rendered still printed a
# tick. A shooting tool that lies about what it produced is worse than one that stops.
BACKED_UP=0
restore() {
  # A guard rather than a comment. `$BACKUP` is a file in a git-ignored directory that survives
  # between runs, so "the backup exists" is not the same question as "this run made it" — and
  # answering the first one is how a config from another day gets copied over a live one.
  if [ "$BACKED_UP" != 1 ]; then return; fi
  cp "$BACKUP" "$CFG"
  relaunch
  echo "→ config restored"
}

run_if() { local n="$1"; shift; if want "$n"; then "$@"; fi; }

# **The backup is taken here and nowhere earlier.** It used to be near the top, above the
# definition of `needs_app` — so the test silently failed, no copy was made, and `restore` at the
# end of the run put *yesterday's* leftover `config-before-shoot.json` over a live config. That
# took the remote server off a machine whose phone was using it. Hence both halves below: the copy
# is made immediately before the config is touched, and `restore` refuses to run unless this
# particular run is the one that made it.
if needs_app; then
  cp "$CFG" "$BACKUP"
  BACKED_UP=1
  trap restore EXIT
  set_cfg 'language="en"' 'output_size=11.5' 'mascot="clawd"'
  relaunch
fi

# The session list, twice, and the difference between them is the argument each one is making.
#
# `sessions-live` is for the top of the page, where the claim is that the *terminal* follows the
# selection — so it has a terminal drawn behind it with a tab bar and a status line to follow in.
# `sessions` is for the section, where the claim is about the rows themselves: one answered and
# going quiet, one finishing, one starting to ask. That was a still until it was pointed out that
# a still is the one thing which cannot show a state changing, in a section that is entirely
# about states changing. Same storyboard, same five made-up projects, no terminal — the furniture
# there would be arguing with the subject.
run_if sessions   strip sessions      rows     5.2 820
run_if sessions   strip sessions-live sessions 5.2 760
# The picker, still and moving. Any pack that did not ship with the app is moved aside first:
# the picture is what a fresh install has, and somebody else's character does not belong in this
# repository's README whatever it is doing on this machine.
if want picker; then
  ASIDE="$TMP/packs-aside"; mkdir -p "$ASIDE"
  for f in "$HOME/.config/clawdline/mascots/"*.json; do
    [ -e "$f" ] || continue
    n=$(basename "$f")
    [ -e "Resources/mascots/$n" ] || mv "$f" "$ASIDE/$n"
  done
  restore_packs() { for f in "$ASIDE"/*.json; do [ -e "$f" ] && mv "$f" "$HOME/.config/clawdline/mascots/"; done; }
  trap 'restore_packs; restore' EXIT
  relaunch
  shot picker "list=mascots"
  # Two packs ship, so the walk goes down the list and back up: three beats, two changes, in
  # opposite directions. One change in the middle of a clip reads as a picture with two halves;
  # the second change is what says the first one was a selection moving and not a cut. Longer
  # beats than the other clips for the same reason — the whole claim is that you *look* at it.
  strip picker-live mascots 5.4 620
  restore_packs
  trap restore EXIT
  relaunch
fi
run_if transcript shot transcript "output=1&full=0&transcript=$T" 6
run_if fullscreen shot fullscreen "output=1&full=1&transcript=$T" 7

# The notch, demonstrating itself.
#
# It used to be three stills padded out with grey and concatenated, which put the shape in the
# middle of a grey rectangle that read as nothing at all, and could not show the one verb the
# caption ends on — it *dances* when a long job finishes. It is a clip now, drawn frame by frame
# by the island itself: the menu bar with the hole in it, and the shape growing out of the hole
# through the four states in the order the sentence goes. Twelve frames a second, because the
# character is a pixel drawing in a thirty-eight point band and nobody is counting its legs.
run_if island strip island island 8.6 1140 "&island=1" 12

run_if demo    strip demo    demo    4.4 760 "&text=add retry with backoff to the upload handler"
run_if dance   strip dance   dance   2   420
run_if stretch strip stretch stretch 2   420

if want voice; then
  strip voice voice 7.5 760 "&text=cambia el retry a exponential backoff|cambia el retry a exponential backoff, y después corre los tests."
fi

# The interface, on a phone.
#
# Shot from a **`file://` copy of the page**, which is the mode its own fixtures were written for
# — "enough of a machine to see every state, every animation and the reconnect, from a file://
# copy with nothing running". Two things follow from that and both matter here. Nothing on this
# machine can reach the frame: there is no server in it, so no repository of yours, no branch of
# yours and no conversation of yours can appear the way they twice have in the app's pictures.
# And anybody can reshoot it — no app, no pairing, no sessions, just a checkout and a Chrome.
#
# The words are English because a `file://` copy has no `/v1/strings` to ask, and the page's own
# built-in copy is the English one. That is the same reason it does not disturb the language
# setting on the way past.
run_if web      webstrip web      "$WEB_PAGE" web  390 16
# And the same page on a laptop, where both panes fit at once. A still, because what is worth
# saying about the wide layout is a fact about the layout and not a thing that happens.
run_if web-wide webshot  web-wide "$WEB_PAGE" open --saying "investigate the webhook" --dwell 2500 \
                                  --desktop --width 1180 --height 760 --scale 2

# The same page, twice more, for the one thing the two above cannot show: **who dispatched whom**.
# A session that sent work away carries the sessions it started underneath it, indented, and that
# grouping is the picture — so both of these wait for the page's `orchestrator` frame to land
# rather than for a clock. Before it does the list is flat, and a flat list is the argument these
# two exist to refute. Same fixtures as everything else on this page: made-up projects, a `file://`
# copy, nothing of this machine anywhere near the frame.
run_if fleet-phone webshot fleet-phone "$WEB_PAGE" fleet --dwell 1400 \
                                       --width 390 --height 844 --scale 2
run_if fleet-wide  webshot fleet-wide  "$WEB_PAGE" fleet-wide --dwell 2000 \
                                       --desktop --width 1180 --height 760 --scale 2

# **The other side of the comparison, and the only picture here that is a drawing.**
#
# The README opens by saying a terminal cannot show you the shape of a fleet — that it offers tab
# titles, and a tab title is a task. That was prose standing next to a picture of the app doing
# the opposite, which asks the reader to imagine the thing being argued with. This draws it:
# eleven sessions as a terminal has them, every title clipped from the left until nothing is left
# but the process in parentheses.
#
# It is a drawing, and it is allowed to be one because of what it draws. Nothing about Clawdline
# is staged in it — no list, no states, no tree — so the rule the rest of this file keeps, that a
# picture of a feature is taken from the feature, is not bent. tools/tabs/index.html has the same
# note at the top of it.
#
# Shot narrow on purpose: a bar of eleven tabs is only an argument if the tabs are legible, and
# 940 points scaled down to 760 keeps the type where the screenshot this was drawn from had it.
run_if tabs        webstrip tabs "file://$PWD/tools/tabs/index.html" tabs 760 12 \
                                       --desktop --width 940 --height 215 --scale 2

# The notification.
#
# **The banner in this one is a real notification.** tools/phone/index.html draws a phone and
# nothing else: it subscribes the browser to Web Push against this Mac's own VAPID key, asks the
# app to send one through `POST /v1/push/test`, and prints whatever the service worker is handed.
# So the words on the glass are the app's, in the app's language, and if the push path is broken
# the shoot fails rather than producing a picture of a feature that no longer works.
#
# It needs the app running with **Answer over HTTP** on, because there is a real server on the
# other end of it. tools/web-serve.py is what puts the drawn phone and the app's `/v1/` on one
# origin, which is the only arrangement a browser will subscribe under.
if want web-push; then
  PORT=$(python3 -c "import json,io,os;print(json.load(io.open(os.path.expanduser('~/.config/clawdline/config.json'))).get('remote_port',7717))")
  if ! curl -s -m 3 "http://127.0.0.1:$PORT/v1/health" >/dev/null; then
    echo "!! the app is not answering on 127.0.0.1:$PORT — Settings → Remote → Answer over HTTP"
    exit 1
  fi
  python3 tools/web-serve.py --root tools/phone --port 7788 &
  SERVE=$!
  trap 'kill $SERVE 2>/dev/null; restore' EXIT
  sleep 1.5
  webstrip web-push "http://127.0.0.1:7788/" push 390 16 --grant notifications
  kill $SERVE 2>/dev/null || true
  trap restore EXIT
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
