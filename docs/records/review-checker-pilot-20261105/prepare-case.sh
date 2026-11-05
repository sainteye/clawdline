#!/bin/sh
# Prepare only. This script does not launch a reviewer or authorize spending.
# usage: sh prepare-case.sh /absolute/local/clawdline COMMIT /empty/output/case
set -eu
review_source=$1
review_commit=$(git -C "$review_source" rev-parse --verify "$2^{commit}")
review_case=$3
if [ -e "$review_case" ]; then
  echo "case output must not exist" >&2
  exit 1
fi
mkdir -p "$review_case"
git -C "$review_case" init -q
git -C "$review_case" fetch -q --depth 2 "file://$review_source" "$review_commit"
git -C "$review_case" checkout -q --detach FETCH_HEAD
