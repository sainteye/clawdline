#!/bin/bash
# Record the voice copilot demo from the built console, in English and/or
# Taiwan Traditional Chinese. docs/copilot-demo.md is the whole of it.
#
#   tools/copilot-demo.sh <output directory> [en] [zh-Hant] [--phone]
#
# Builds the console through tools/heavy.sh, then writes
# clawdline-voice-copilot-demo-<lang>.mp4 for each language (both when none is
# named); --phone records a 390 x 844 phone instead, as <lang>-phone.mp4. Needs Google Chrome and ffmpeg. Spends nothing: the voices are
# committed under tools/copilot-demo/voices. Exit: 0 when every film was
# written and its call ran as written, 1 otherwise, 2 for a usage error.
set -euo pipefail

if [ $# -lt 1 ] || [ ! -d "$1" ]; then
  echo "usage: tools/copilot-demo.sh <existing output directory> [en] [zh-Hant] [--phone]" >&2
  exit 2
fi
out=$(cd "$1" && pwd)
shift
langs=()
phone=""
for word in "$@"; do
  if [ "$word" = "--phone" ]; then phone="--phone"; else langs+=("$word"); fi
done
[ ${#langs[@]} -gt 0 ] || langs=(en zh-Hant)
root=$(cd "$(dirname "$0")/.." && pwd)
command -v ffmpeg >/dev/null || { echo "copilot-demo: ffmpeg is required" >&2; exit 2; }

( cd "$root/web" && "$root/tools/heavy.sh" npm run build )

for lang in ${langs[@]+"${langs[@]}"}; do
  node "$root/tools/copilot-demo/record.mjs" --lang "$lang" --dist "$root/web/console/dist" \
    --out "$out/clawdline-voice-copilot-demo-$lang${phone:+-phone}.mp4" ${phone:+"$phone"}
done
echo "copilot-demo: watch every film before publishing it"
