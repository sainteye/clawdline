#!/bin/bash
# Builds Clawdline.app. No Xcode project, no package manager: a few .swift files and one .js,
# compiled straight by swiftc — one less layer of "what does the build config actually say".
set -euo pipefail
cd "$(dirname "$0")"
. tools/swift-source-manifest.sh
verify_swift_source_manifest production


# --- The machine-level heavy-compile lease -------------------------------------------------
#
# One full Swift compile is the most expensive thing that happens on this Mac, and four
# `swift-frontend` processes have force-rebooted it. `/tmp/clawdline-suite.lock` is the truth:
# holding it is what `mkdir` says it is, which keeps a contributor with no Clawdline running
# from colliding with one that has it. The broker is the registry and the queue in front of that
# directory, so `curl` failing costs the queue and the visibility, never the exclusion.
#
# **Fail closed.** No lease means the compile does not run. There is no proceed-anyway, and
# nothing here ever ends anybody else's process.
CLAWDLINE_LEASE_DIR="${CLAWDLINE_LEASE_DIR:-/tmp/clawdline-suite.lock}"
CLAWDLINE_LEASE_ID=""
CLAWDLINE_LEASE_MODE=""
CLAWDLINE_LEASE_DONE=""
CLAWDLINE_SUITE_JOBS="${CLAWDLINE_SUITE_JOBS:-}"
CLAWDLINE_SUITE_JOBS_SOURCE="unset"
CLAWDLINE_LEASE_WAIT_SECONDS="${CLAWDLINE_LEASE_WAIT_SECONDS:-1800}"

clawdline_lease_port() {
  /usr/bin/python3 -c 'import json,os;print(json.load(open(os.path.expanduser("~/.config/clawdline/config.json"))).get("remote_port",7717))' 2>/dev/null || echo 7717
}

# `pid` is the process doing the work, never a sentinel. `phase` and `heartbeat` are refreshed
# by the supervisor loop below.
clawdline_lease_holder_file() {
  local phase="${1:-analysing}" pid="${2:-$$}"
  printf 'holder=%s\npid=%s\nphase=%s\nheartbeat=%s\nstarted=%s\ntree=%s\nlog=%s\nnote=%s\nwork=%s\ndone_flag=%s\nrenewed=%s\n' \
    "build.sh $(id -un) pid $$" "$pid" "$phase" "$CLAWDLINE_LEASE_DIR/beat" \
    "$CLAWDLINE_LEASE_STARTED" "$(pwd)" "" "building Clawdline.app" "$pid" \
    "$CLAWDLINE_LEASE_DONE" "$(date +%s)" > "$CLAWDLINE_LEASE_DIR/holder.txt"
}

# One beat. It lives inside the lock directory, so `rmdir` takes it with the lock and no orphan
# heartbeat can outlive the work it stood for.
clawdline_lease_beat() {
  local phase="${1:-analysing}" pid="${2:-$$}"
  : > "$CLAWDLINE_LEASE_DIR/beat" 2>/dev/null || true
  clawdline_lease_holder_file "$phase" "$pid" 2>/dev/null || true
  if [ "$CLAWDLINE_LEASE_MODE" = broker ] && [ -r "$CLAWDLINE_LEASE_TOKEN" ]; then
    curl -s --max-time 5 -X POST \
      "http://127.0.0.1:$CLAWDLINE_LEASE_PORT/v1/orchestrator/leases/$CLAWDLINE_LEASE_ID/renew" \
      -H "X-Clawdline-Orchestrator: $(cat "$CLAWDLINE_LEASE_TOKEN")" \
      -H 'Content-Type: application/json' \
      -d "{\"holder\":\"build.sh $(id -un) pid $$\",\"phase\":\"$phase\",\"work_pids\":\"$pid\"}" \
      >/dev/null 2>&1 || true
  fi
}

# **The heartbeat is emitted by the loop that supervises the compiler, and by nothing else.**
#
# This is the whole difference between a heartbeat and the `sleep 14400` this design exists to
# fix. A detached timer —
#
#     while true; do : > beat; sleep 60; done &      # a sentinel
#
# — keeps beating after the work it claims to represent has died, which is a sentinel in a new
# coat. Here the loop's own condition is the compiler still being alive, so when `swiftc` exits,
# or when this shell is killed, the beat stops with it. `kill -0` sends no signal; it asks
# whether the process exists, and nothing in this file ever signals a process it did not start.
clawdline_lease_supervise() {
  local compiler=$1
  while kill -0 "$compiler" 2>/dev/null; do
    clawdline_lease_beat compiling "$compiler"
    sleep 20
  done
}

clawdline_lease_acquire() {
  CLAWDLINE_LEASE_PORT=$(clawdline_lease_port)
  CLAWDLINE_LEASE_TOKEN="$HOME/.config/clawdline/orchestrator-token"
  CLAWDLINE_LEASE_ID="build-$$-$(date +%s)"
  CLAWDLINE_LEASE_STARTED=$(date +%s)
  CLAWDLINE_LEASE_DONE="${TMPDIR:-/tmp}/clawdline-build-done-$$"
  rm -f "$CLAWDLINE_LEASE_DONE"
  local deadline=$(( $(date +%s) + CLAWDLINE_LEASE_WAIT_SECONDS ))
  local announced=0
  while :; do
    if [ -r "$CLAWDLINE_LEASE_TOKEN" ] && command -v curl >/dev/null 2>&1; then
      local answer
      answer=$(curl -s --max-time 10 -X POST \
        "http://127.0.0.1:$CLAWDLINE_LEASE_PORT/v1/orchestrator/leases" \
        -H "X-Clawdline-Orchestrator: $(cat "$CLAWDLINE_LEASE_TOKEN")" \
        -H 'Content-Type: application/json' \
        -d "{\"request_id\":\"$CLAWDLINE_LEASE_ID\",\"resource\":\"heavy_compile\",
             \"pid\":$$,\"process_start\":$CLAWDLINE_LEASE_STARTED,
             \"holder\":\"build.sh $(id -un) pid $$\",\"phase\":\"analysing\",
             \"heartbeat\":\"$CLAWDLINE_LEASE_DIR/beat\",
             \"done_flag\":\"$CLAWDLINE_LEASE_DONE\",
             \"reason\":\"building Clawdline.app\",\"tree\":\"$(pwd)\"}" 2>/dev/null) || answer=
      local state
      state=$(printf '%s' "$answer" | /usr/bin/python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("state",""))
except Exception: print("")' 2>/dev/null)
      case "$state" in
        granted)
          CLAWDLINE_LEASE_MODE=broker
          CLAWDLINE_SUITE_JOBS=$(printf '%s' "$answer" | /usr/bin/python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("budget",{}).get("parallelism",""))
except Exception: print("")' 2>/dev/null)
          CLAWDLINE_SUITE_JOBS_SOURCE="the broker lease budget"
          echo "→ heavy-compile lease granted by the broker ($CLAWDLINE_LEASE_ID)"
          clawdline_lease_holder_file 2>/dev/null || true
          return 0
          ;;
        queued)
          if [ "$announced" = 0 ]; then
            printf '%s' "$answer" | /usr/bin/python3 -c 'import json,sys
try:
    a = json.load(sys.stdin)
    h = (a.get("lease") or {}).get("holder") or {}
    print("→ waiting for the heavy-compile slot: position %s, %s; held by %s" % (
        a.get("position"), a.get("holdReason"), h.get("holder", "nobody recorded")))
except Exception: print("→ waiting for the heavy-compile slot")' 2>/dev/null
            announced=1
          fi
          ;;
        "")
          # The broker did not answer. The directory is still the truth, so fall back to it
          # rather than proceeding without a lease.
          if mkdir "$CLAWDLINE_LEASE_DIR" 2>/dev/null; then
            CLAWDLINE_LEASE_MODE=directory
            CLAWDLINE_SUITE_JOBS_SOURCE="unset (no broker answered, so no budget)"
            clawdline_lease_holder_file
            echo "→ heavy-compile lock taken directly; the broker did not answer"
            return 0
          fi
          if [ "$announced" = 0 ]; then
            echo "→ waiting for $CLAWDLINE_LEASE_DIR, held by:"
            sed 's/^/    /' "$CLAWDLINE_LEASE_DIR/holder.txt" 2>/dev/null | head -4
            announced=1
          fi
          ;;
        *)
          echo "!! the heavy-compile lease was refused, and this build will not compile without it:" >&2
          printf '%s\n' "$answer" >&2
          return 1
          ;;
      esac
    else
      if mkdir "$CLAWDLINE_LEASE_DIR" 2>/dev/null; then
        CLAWDLINE_LEASE_MODE=directory
        CLAWDLINE_SUITE_JOBS_SOURCE="unset (no orchestrator token, so no budget)"
        clawdline_lease_holder_file
        echo "→ heavy-compile lock taken directly; this Mac has no orchestrator token"
        return 0
      fi
    fi
    if [ "$(date +%s)" -ge "$deadline" ]; then
      echo "!! gave up waiting ${CLAWDLINE_LEASE_WAIT_SECONDS}s for the heavy-compile slot." >&2
      echo "   Nothing was killed and nothing was compiled. Look at $CLAWDLINE_LEASE_DIR/holder.txt." >&2
      return 1
    fi
    sleep 5
  done
}

# Release is idempotent and removes the directory only while it still names this process — the
# one rule a release path may never break is removing a lock somebody else owns.
clawdline_lease_release() {
  [ -n "$CLAWDLINE_LEASE_MODE" ] || return 0
  if [ "$CLAWDLINE_LEASE_MODE" = broker ] && [ -r "$CLAWDLINE_LEASE_TOKEN" ]; then
    curl -s --max-time 5 -X POST \
      "http://127.0.0.1:$CLAWDLINE_LEASE_PORT/v1/orchestrator/leases/$CLAWDLINE_LEASE_ID/release" \
      -H "X-Clawdline-Orchestrator: $(cat "$CLAWDLINE_LEASE_TOKEN")" \
      -H 'Content-Type: application/json' \
      -d "{\"holder\":\"build.sh $(id -un) pid $$\"}" >/dev/null 2>&1 || true
  fi
  if [ -r "$CLAWDLINE_LEASE_DIR/holder.txt" ] \
      && grep -qx "holder=build.sh $(id -un) pid $$" "$CLAWDLINE_LEASE_DIR/holder.txt" 2>/dev/null; then
    rm -rf "$CLAWDLINE_LEASE_DIR"
  fi
  CLAWDLINE_LEASE_MODE=""
}

APP="${CLAWDLINE_APP:-$HOME/Applications/Clawdline.app}"
APP_PARENT="$(dirname "$APP")"
APP_NAME="$(basename "$APP")"
BUNDLE_ID="com.tsunamiworks.clawdline"
LOCAL_SIGN_IDENTITY_NAME="Clawdline Local Development"
LOCAL_SIGNING=0
# BEGIN keychain-rebuild-focused: bounded signing commands
# Every Keychain-touching command below can reach a system dialog — "unlock the login keychain",
# "codesign wants to access key" — that waits for a person who may not be at the Mac. macOS ships
# no timeout(1) and /bin/bash here is 3.2, so `wait -n` is out too: a watchdog subshell it is.
#
# The marker file is what tells a timeout apart from an ordinary non-zero exit. Deriving it from
# the signal number cannot: 143 is also what a command killed by anything else reports.
CLAWDLINE_SIGN_QUERY_TIMEOUT="${CLAWDLINE_SIGN_QUERY_TIMEOUT:-30}"
CLAWDLINE_CODESIGN_TIMEOUT="${CLAWDLINE_CODESIGN_TIMEOUT:-120}"
clawdline_require_positive_integer() {
  local name=$1 value=$2
  case "$value" in
    ''|*[!0-9]*|0)
      echo "!! $name must be a positive integer, got: $value" >&2
      return 2
      ;;
  esac
}
clawdline_require_positive_integer CLAWDLINE_SIGN_QUERY_TIMEOUT "$CLAWDLINE_SIGN_QUERY_TIMEOUT" || exit $?
clawdline_require_positive_integer CLAWDLINE_CODESIGN_TIMEOUT "$CLAWDLINE_CODESIGN_TIMEOUT" || exit $?

# `CLAWDLINE_BOUNDED_OUTCOME` is the typed side channel. Exit 124 alone is ambiguous because the
# child itself is allowed to exit 124; only `timeout` means the watchdog killed a live process.
CLAWDLINE_BOUNDED_OUTCOME=not_run
clawdline_bounded() {
  local seconds=$1 outfile=$2
  shift 2
  local marker="$outfile.timed-out"
  rm -f "$marker"
  "$@" >"$outfile" 2>&1 &
  local pid=$!
  (
    sleep "$seconds"
    kill -TERM "$pid" 2>/dev/null && : > "$marker"
    sleep 2
    kill -KILL "$pid" 2>/dev/null
  ) >/dev/null 2>&1 &
  local watchdog=$!
  local status=0
  wait "$pid" 2>/dev/null || status=$?
  kill -TERM "$watchdog" 2>/dev/null || true
  wait "$watchdog" 2>/dev/null || true
  if [ -e "$marker" ]; then
    rm -f "$marker"
    CLAWDLINE_BOUNDED_OUTCOME=timeout
    return 124
  fi
  CLAWDLINE_BOUNDED_OUTCOME=exit
  return "$status"
}
# END keychain-rebuild-focused: bounded signing commands
# BEGIN keychain-rebuild-focused: signing identity selection
# The login keychain is only ever *probed*, never unlocked: a build that could unlock somebody's
# Keychain would be a build that could be asked to.
LOCAL_SIGN_KEYCHAIN="${CLAWDLINE_LOCAL_SIGN_KEYCHAIN:-$HOME/Library/Keychains/login.keychain-db}"
if [ -n "${CLAWDLINE_KEYCHAIN_STATUS_HELPER:-}" ]; then
  keychain_status_command=("$CLAWDLINE_KEYCHAIN_STATUS_HELPER")
else
  keychain_status_command=(xcrun swift tools/keychain-status.swift)
fi
signing_probe_out=$(mktemp "${TMPDIR:-/tmp}/clawdline-signing-probe.XXXXXX")
trap 'rm -f "$signing_probe_out" "$signing_probe_out.timed-out"' EXIT
if [ "${CLAWDLINE_SIGN_IDENTITY+x}" = x ]; then
  # An explicit value keeps its historical meaning, including an empty value becoming ad-hoc.
  SIGN_IDENTITY="${CLAWDLINE_SIGN_IDENTITY:--}"
elif [ "${CLAWDLINE_SIGN_ADHOC:-0}" = 1 ]; then
  # The documented ad-hoc contract: chosen, not fallen into. Nothing below is consulted, so it
  # is also the way past a locked Keychain without unlocking anything.
  SIGN_IDENTITY=-
  echo "→ CLAWDLINE_SIGN_ADHOC=1; signing ad-hoc by explicit request"
  echo "  Ad-hoc means a new code identity every rebuild: macOS re-asks to authorise iTerm2"
  echo "  automation, and the Cloud Keychain items are re-authorised on first use."
else
  identity_status=0
  clawdline_bounded "$CLAWDLINE_SIGN_QUERY_TIMEOUT" "$signing_probe_out" \
    security find-identity -v -p codesigning "$LOCAL_SIGN_KEYCHAIN" || identity_status=$?
  if [ "$identity_status" -eq 0 ]; then
    identity_output=$(cat "$signing_probe_out")
    identity_hashes=$(printf '%s\n' "$identity_output" \
      | awk -v name="$LOCAL_SIGN_IDENTITY_NAME" \
          'index($0, "\"" name "\"") { print $2 }')
    identity_count=$(printf '%s\n' "$identity_hashes" \
      | awk 'NF { count++ } END { print count + 0 }')
    if [ "$identity_count" -eq 1 ]; then
      # An identity that exists in a locked Keychain is worse than one that does not: codesign
      # finds it, then stops on an unlock dialog. Ask first, and say so instead of hanging.
      keychain_status=0
      clawdline_bounded "$CLAWDLINE_SIGN_QUERY_TIMEOUT" "$signing_probe_out" \
        "${keychain_status_command[@]}" "$LOCAL_SIGN_KEYCHAIN" || keychain_status=$?
      if [ "$keychain_status" -eq 0 ]; then
        SIGN_IDENTITY=$(printf '%s\n' "$identity_hashes" | awk 'NF { print; exit }')
        LOCAL_SIGNING=1
      else
        if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
          echo "!! the login Keychain did not answer within ${CLAWDLINE_SIGN_QUERY_TIMEOUT}s" >&2
        else
          echo "!! the login Keychain is locked or unreadable: $LOCAL_SIGN_KEYCHAIN (status $keychain_status)" >&2
        fi
        echo "   $LOCAL_SIGN_IDENTITY_NAME exists there, so signing would stop on an unlock" >&2
        echo "   dialog. Clawdline will not unlock a Keychain for you." >&2
        echo "   Unlock it yourself:  security unlock-keychain $LOCAL_SIGN_KEYCHAIN" >&2
        echo "   Or build ad-hoc:     CLAWDLINE_SIGN_ADHOC=1 ./build.sh" >&2
        exit 1
      fi
    elif [ "$identity_count" -gt 1 ]; then
      echo "!! multiple valid code-signing identities are named $LOCAL_SIGN_IDENTITY_NAME" >&2
      printf '   %s\n' $identity_hashes >&2
      echo "   Remove or rename the extra identity; Clawdline will not choose by Keychain order." >&2
      exit 1
    else
      echo "!! no valid $LOCAL_SIGN_IDENTITY_NAME identity exists in $LOCAL_SIGN_KEYCHAIN" >&2
      echo "   Run tools/setup-local-signing-identity.sh, or explicitly choose ad-hoc:" >&2
      echo "     CLAWDLINE_SIGN_ADHOC=1 ./build.sh" >&2
      exit 1
    fi
  else
    if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
      echo "!! code-signing identity lookup did not answer within ${CLAWDLINE_SIGN_QUERY_TIMEOUT}s" >&2
    else
      echo "!! code-signing identity lookup failed with status $identity_status" >&2
    fi
    echo "   Refusing an implicit ad-hoc fallback. Inspect $LOCAL_SIGN_KEYCHAIN, or explicitly choose:" >&2
    echo "     CLAWDLINE_SIGN_ADHOC=1 ./build.sh" >&2
    exit 1
  fi
fi
# END keychain-rebuild-focused: signing identity selection
# The probe's own trap is replaced by cleanup_build further down, so retire it here rather than
# leaving the file behind for whoever empties TMPDIR next.
rm -f "$signing_probe_out" "$signing_probe_out.timed-out"
trap - EXIT

mkdir -p "$APP_PARENT"
# Build beside the installed app, on the same filesystem. The final rename is then quick and
# cannot turn into a slow copy just when the running app has been stopped.
STAGE_ROOT="$(mktemp -d "$APP_PARENT/.clawdline-build.XXXXXX")"
STAGED_APP="$STAGE_ROOT/$APP_NAME"
BIN="$STAGED_APP/Contents/MacOS/Clawdline"
RES="$STAGED_APP/Contents/Resources"
BACKUP="$STAGE_ROOT.previous"
cleanup_build() {
  if [ "${MAINTENANCE_ACTIVE:-0}" = 1 ] && [ -n "${MAINTENANCE_REQUEST_ID:-}" ] \
      && [ -r "${TOKEN_FILE:-}" ] && [ -n "${PORT:-}" ]; then
    curl -sS --max-time 5 -X DELETE \
      "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
      -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
      -H 'Content-Type: application/json' \
      -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" >/dev/null 2>&1 || true
  fi
  clawdline_lease_release
  rm -rf "$STAGE_ROOT"
}
trap cleanup_build EXIT

echo "→ building staged app for $APP"
mkdir -p "$(dirname "$BIN")" "$RES"

clawdline_lease_acquire || exit 1

# Say which ceiling was used and where the number came from, so a slow build is explained rather
# than mysterious. `-j` is what decides how many `swift-frontend` processes exist at once, which
# is the quantity that reboots this Mac.
compile_jobs=()
if [ -n "$CLAWDLINE_SUITE_JOBS" ]; then
  compile_jobs=(-j "$CLAWDLINE_SUITE_JOBS")
  echo "→ compiling with -j $CLAWDLINE_SUITE_JOBS, from $CLAWDLINE_SUITE_JOBS_SOURCE"
else
  echo "→ compiling with swiftc's own default parallelism ($CLAWDLINE_SUITE_JOBS_SOURCE)"
fi

swiftc \
  -swift-version 5 \
  -target arm64-apple-macos13.0 \
  -O \
  ${compile_jobs[@]+"${compile_jobs[@]}"} \
  -o "$BIN" \
  "${clawdline_production_sources[@]}" \
  -framework AppKit -framework Carbon -framework ServiceManagement -framework Speech -framework AVFoundation -framework Network &
CLAWDLINE_COMPILER=$!
clawdline_lease_supervise "$CLAWDLINE_COMPILER"
wait "$CLAWDLINE_COMPILER"

# The work is over: say so positively, then give the slot back. `done_flag` existing is what lets
# another line take the lock at once instead of waiting out a heartbeat threshold. Packaging,
# signing and installing are not what this lease protects.
: > "$CLAWDLINE_LEASE_DONE" 2>/dev/null || true
clawdline_lease_release

cp Resources/iterm.js "$RES/"
cp Resources/Clawdline.icns "$RES/"
cp Resources/clawdline-hook.sh "$RES/"
# The default dispatch policy. It is a document people read and edit, so it ships as a file rather
# than as a string literal in the source; Orchestrator writes it out once if the machine has none.
cp Resources/dispatch-policy.md "$RES/"
cp -R Resources/mascots "$RES/"
# The web interface, served by RemoteServer when it is switched on.
[ -d Resources/web ] && cp -R Resources/web "$RES/"

cat > "$STAGED_APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Clawdline</string>
  <key>CFBundleDisplayName</key><string>Clawdline</string>
  <key>CFBundleIdentifier</key><string>com.tsunamiworks.clawdline</string>
  <key>CFBundleExecutable</key><string>Clawdline</string>
  <key>CFBundleIconFile</key><string>Clawdline</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.6.0</string>
  <key>CFBundleVersion</key><string>0.6.0</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHumanReadableCopyright</key><string>Copyright © 2026 TsunamiWorks Co., Ltd.</string>
  <!-- Without this line macOS kills the app the first time it talks to iTerm2 -->
  <key>NSAppleEventsUsageDescription</key>
  <string>Clawdline needs to control iTerm2 so it can put what you type into Claude Code.</string>
  <!-- Asked for only when the microphone button is pressed. Recognition runs on this Mac when
       the dictation language is installed, and goes to Apple when it is not — the bar says
       which, while it is listening. -->
  <key>NSMicrophoneUsageDescription</key>
  <string>Clawdline uses the microphone only while you hold a dictation session open, so you can talk into the prompt instead of typing.</string>
  <key>NSSpeechRecognitionUsageDescription</key>
  <string>Clawdline turns your speech into text. It uses this Mac when the dictation language is installed, and Apple's service when it is not.</string>
  <!-- clawdline://open so any tool can summon it, not just the built-in hotkey -->
  <key>CFBundleURLTypes</key>
  <array><dict>
    <key>CFBundleURLName</key><string>com.tsunamiworks.clawdline</string>
    <key>CFBundleURLSchemes</key><array><string>clawdline</string></array>
  </dict></array>
</dict>
</plist>
PLIST

# A stable local certificate keeps the Keychain ACL's designated requirement unchanged across
# rebuilds. A release still supplies the company Developer ID identity and receives Hardened
# Runtime, a trusted timestamp, and only the two resource entitlements the app actually uses.
# The private key for either identity never enters this repository.
# BEGIN keychain-rebuild-focused: signing branches
codesign_out=$(mktemp "${TMPDIR:-/tmp}/clawdline-codesign.XXXXXX")
if [ "$SIGN_IDENTITY" = - ]; then
  adhoc_sign_status=0
  clawdline_bounded "$CLAWDLINE_CODESIGN_TIMEOUT" "$codesign_out" \
    codesign --force --sign - --identifier "$BUNDLE_ID" "$STAGED_APP" \
    || adhoc_sign_status=$?
  if [ "$adhoc_sign_status" -ne 0 ]; then
    cat "$codesign_out" >&2
    if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
      echo "!! ad-hoc codesign timed out after ${CLAWDLINE_CODESIGN_TIMEOUT}s; staged bundle state is unknown" >&2
    else
      echo "!! ad-hoc codesign failed with status $adhoc_sign_status" >&2
    fi
    exit "$adhoc_sign_status"
  fi
elif [ "$LOCAL_SIGNING" = 1 ]; then
  echo "→ using stable local signing"
  local_sign_status=0
  clawdline_bounded "$CLAWDLINE_CODESIGN_TIMEOUT" "$codesign_out" \
    codesign --force --sign "$SIGN_IDENTITY" --keychain "$LOCAL_SIGN_KEYCHAIN" \
    --identifier "$BUNDLE_ID" "$STAGED_APP" \
    || local_sign_status=$?
  if [ "$local_sign_status" -ne 0 ]; then
    cat "$codesign_out" >&2
    if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
      # The one failure the person cannot see, because the dialog it is waiting on may be
      # behind another window or on another Space.
      echo "!! codesign did not finish within ${CLAWDLINE_CODESIGN_TIMEOUT}s" >&2
      echo "   That is what an unanswered Keychain access dialog looks like from here." >&2
      echo "   Configure the key's code-signing access yourself in Keychain Access or with" >&2
      echo "   SecurityTool. Its set-key-partition-list command requires '-k password'," >&2
      echo "   which Clawdline does not accept or pass." >&2
    else
      echo "!! signing with $LOCAL_SIGN_IDENTITY_NAME failed (exit $local_sign_status)" >&2
    fi
    echo "   Or build ad-hoc:    CLAWDLINE_SIGN_ADHOC=1 ./build.sh" >&2
    rm -f "$codesign_out" "$codesign_out.timed-out"
    exit "$local_sign_status"
  fi
  echo "✓ signed with stable local identity $LOCAL_SIGN_IDENTITY_NAME"
  echo "  After changing signing identity, first use may show up to three Keychain prompts (machine credential and two Cloud keys); approve each item you use."
else
  signed=0
  release_sign_status=0
  for attempt in 1 2 3; do
    if clawdline_bounded "$CLAWDLINE_CODESIGN_TIMEOUT" "$codesign_out" \
        codesign --force --sign "$SIGN_IDENTITY" --identifier "$BUNDLE_ID" \
        --options runtime --timestamp --entitlements Resources/Clawdline.entitlements \
        "$STAGED_APP"; then
      signed=1
      break
    else
      release_sign_status=$?
    fi
    cat "$codesign_out" >&2
    if [ "$CLAWDLINE_BOUNDED_OUTCOME" = timeout ]; then
      echo "!! Developer ID codesign timed out after ${CLAWDLINE_CODESIGN_TIMEOUT}s; staged bundle state is unknown" >&2
      exit 124
    fi
    [ "$attempt" = 3 ] && break
    echo "  Apple timestamp service did not answer; retrying Developer ID signing ($attempt/3)"
    sleep $((attempt * 5))
  done
  [ "$signed" = 1 ] || {
    echo "!! Developer ID signing failed after 3 attempts"
    rm -f "$codesign_out" "$codesign_out.timed-out"
    exit "$release_sign_status"
  }
fi
rm -f "$codesign_out" "$codesign_out.timed-out"
# END keychain-rebuild-focused: signing branches

# Packaging and CI build beside the installed app but must never inspect, stop, replace, or reopen
# the live Clawdline process. Their caller owns the fresh output path and receives only the bundle.
if [ "${CLAWDLINE_BUILD_ONLY:-0}" = 1 ]; then
  [ ! -e "$APP" ] || { echo "!! build-only destination already exists: $APP"; exit 1; }
  mv "$STAGED_APP" "$APP"
  echo "✓ built $APP"
  exit 0
fi

# Nothing installed or running has been touched until here. Remember the state at the instant
# of replacement — somebody who deliberately quit while a long build was running should not
# have the app reopened against their wishes.
echo "→ installing finished build"
WAS_RUNNING=0
pgrep -x Clawdline >/dev/null 2>&1 && WAS_RUNNING=1

# A dispatched task lives through a restart once it has been briefed: its durable record is on disk,
# its secret has reached the child, and its child is a terminal tab this script does not touch. A
# `briefed` task is therefore evidence of live work, not a restart blocker. In the seconds
# *before* that,
# it does not — the plaintext secret is only in the old process's memory, so a child whose tab has
# opened but whose first message has not been typed can never be briefed, and comes back as
# `spawn_failed: the app restarted before the child was briefed`.
#
# That window is about a second wide per task, which is small until several people share this
# working copy: one session runs ./build.sh, another one's grandchildren die, and the message it
# gets back blames the app rather than the person who rebuilt it. So look, and say so. Not a
# refusal — this is somebody's own machine and their own build — just the one fact that turns a
# baffling failure into an obvious one.
if [ "$WAS_RUNNING" = 1 ] && command -v curl >/dev/null 2>&1; then
  PORT=$(/usr/bin/python3 -c 'import json,os;print(json.load(open(os.path.expanduser("~/.config/clawdline/config.json"))).get("remote_port",7717))' 2>/dev/null || echo 7717)
  TOKEN_FILE="$HOME/.config/clawdline/orchestrator-token"
  if [ -r "$TOKEN_FILE" ]; then
    if ! TASK_SNAPSHOT=$(curl -s --max-time 2 \
        "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" 2>/dev/null); then
      TASK_SNAPSHOT=
    fi
    MIDFLIGHT=$(printf '%s' "$TASK_SNAPSHOT" | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
for x in t:
    if x.get("state") in ("queued", "spawning"):
        print("  %s  %s" % (x.get("id","")[:8], x.get("title","")[:48]))' 2>/dev/null)
    BRIEFED=$(printf '%s' "$TASK_SNAPSHOT" | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
for x in t:
    if x.get("state") == "briefed":
        print("  %s  %s" % (x.get("id","")[:8], x.get("title","")[:48]))' 2>/dev/null)
    if [ -n "$BRIEFED" ]; then
      echo "→ briefed task(s) are durable and do not block this restart"
      echo "$BRIEFED"
    fi
    if [ -n "$MIDFLIGHT" ]; then
      # Wait rather than warn. The window is seconds wide and closes on its own, while the thing
      # on the other side of it is somebody's dispatched task dying with a message that blames
      # the app. A printed warning is the right shape for a person and the wrong one here: on a
      # machine where several sessions share a checkout, the one running this is usually another
      # agent, and it will not stop to read a line it did not ask for.
      echo "→ a dispatched task is mid-spawn; waiting for it to be briefed (up to 90s)"
      echo "$MIDFLIGHT"
      for _ in $(seq 1 90); do
        sleep 1
        STILL=$(curl -s --max-time 2 "http://127.0.0.1:$PORT/v1/orchestrator/tasks" \
            -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" 2>/dev/null \
          | /usr/bin/python3 -c 'import json,sys
try: t = json.load(sys.stdin).get("tasks", [])
except Exception: raise SystemExit
print("".join("x" for x in t if x.get("state") in ("queued", "spawning")))' 2>/dev/null)
        [ -z "$STILL" ] && { echo "   clear — carrying on"; break; }
      done
      # Ninety seconds is the whole of the patience. Past that the task is not mid-spawn any
      # more, it is stuck, and holding a build hostage to it helps nobody.
      [ -n "$STILL" ] && echo "   still spawning after 90s; restarting anyway"
    fi
  fi
fi

# New runtimes own the replacement boundary: close admission, let already-admitted Apple Events
# drain, persist `ready`, and keep admission closed until the replacement's own complete Session
# inventory reconciles every briefed executor. The first build that installs this protocol is
# necessarily talking to an older runtime; only an exact 404 takes the documented bootstrap path
# above. Every other refusal is typed and stops before the running app is touched.
MAINTENANCE_REQUEST_ID=
MAINTENANCE_ACTIVE=0
if [ "$WAS_RUNNING" = 1 ] && command -v curl >/dev/null 2>&1 && [ -r "${TOKEN_FILE:-}" ]; then
  MAINTENANCE_REQUEST_ID=$(/usr/bin/uuidgen | tr '[:upper:]' '[:lower:]')
  MAINTENANCE_REPLY=$(mktemp "$STAGE_ROOT/maintenance.XXXXXX")
  MAINTENANCE_STATUS=$(curl -sS --max-time 5 -o "$MAINTENANCE_REPLY" -w '%{http_code}' \
      -X POST "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
      -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
      -H 'Content-Type: application/json' \
      -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" || true)
  if [ "$MAINTENANCE_STATUS" = 404 ]; then
    echo "→ installed runtime has no restart-maintenance route; using one-time bootstrap preflight"
    MAINTENANCE_REQUEST_ID=
  elif [ "$MAINTENANCE_STATUS" != 200 ]; then
    echo "!! restart maintenance was refused (HTTP ${MAINTENANCE_STATUS:-unreachable})"
    /usr/bin/python3 -c 'import json,sys
try:
    e=json.load(open(sys.argv[1])).get("error",{})
    print("   %s: %s" % (e.get("code","unknown"),e.get("message","no message")))
except Exception: pass' "$MAINTENANCE_REPLY"
    exit 1
  else
    MAINTENANCE_ACTIVE=1
    echo "→ restart maintenance admitted; waiting for the terminal broker to drain"
    for _ in $(seq 1 120); do
      PHASE=$(/usr/bin/python3 -c 'import json,sys
try: print(json.load(open(sys.argv[1])).get("restart",{}).get("phase",""))
except Exception: pass' "$MAINTENANCE_REPLY")
      [ "$PHASE" = ready ] && break
      curl -sS --max-time 5 -o "$MAINTENANCE_REPLY" \
        "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" || true
      PHASE=$(/usr/bin/python3 -c 'import json,sys
try: print(json.load(open(sys.argv[1])).get("restart",{}).get("phase",""))
except Exception: pass' "$MAINTENANCE_REPLY")
      [ "$PHASE" = ready ] && break
      sleep 1
    done
    if [ "${PHASE:-}" != ready ]; then
      curl -sS --max-time 5 -X DELETE \
        "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
        -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
        -H 'Content-Type: application/json' \
        -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" >/dev/null || true
      echo "!! terminal broker did not drain in 120s; maintenance was aborted and nothing was replaced"
      exit 1
    fi
  fi
fi

pkill -x Clawdline 2>/dev/null || true
# **`pkill` asks; it does not wait.** Moving the bundle while a process on its way out is still
# reading it can make AppKit teardown ask CoreFoundation for files that have just moved away.
# Wait for the old process to be genuinely gone before the two quick renames below.
for _ in $(seq 1 60); do
  pgrep -x Clawdline >/dev/null 2>&1 || break
  sleep 0.1
done

# Keep the old bundle recoverable until the staged one is in its final name. Both moves are on
# the same filesystem; if the second one fails, put the first one back before reporting failure.
HAD_OLD=0
if [ -e "$APP" ]; then
  if ! mv "$APP" "$BACKUP"; then
    echo "!! could not move the existing app aside; it has not been changed"
    [ "$WAS_RUNNING" = "1" ] && open "$APP"
    exit 1
  fi
  HAD_OLD=1
fi
if ! mv "$STAGED_APP" "$APP"; then
  echo "!! could not install the finished build"
  if [ "$HAD_OLD" = "1" ]; then
    if mv "$BACKUP" "$APP"; then
      echo "   restored the previous app"
    else
      echo "   previous app is still recoverable at: $BACKUP"
    fi
  fi
  [ "$WAS_RUNNING" = "1" ] && [ -d "$APP" ] && open "$APP"
  exit 1
fi
[ "$HAD_OLD" = "1" ] && rm -rf "$BACKUP"

if [ "$WAS_RUNNING" = "1" ]; then
  open "$APP"
  # **Say it came back only if it came back.** This used to print "relaunched" the instant
  # `open` returned, which says nothing: `open` hands the request to Launch Services and exits.
  # A build that killed the app and failed to start it again reported success, and the person
  # watching saw their bar vanish with no reason given — which reads as a crash, not as a build.
  for _ in $(seq 1 50); do
    pgrep -x Clawdline >/dev/null 2>&1 && break
    sleep 0.1
  done
  if pgrep -x Clawdline >/dev/null 2>&1; then
    if [ "$MAINTENANCE_ACTIVE" = 1 ]; then
      echo "→ replacement is listening; waiting for fresh executor reconciliation"
      RECONCILED=0
      for _ in $(seq 1 180); do
        RESTART_REPLY=$(curl -sS --max-time 5 \
          "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
          -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" || true)
        PHASE=$(printf '%s' "$RESTART_REPLY" | /usr/bin/python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("restart",{}).get("phase",""))
except Exception: pass')
        if [ "$PHASE" = complete ]; then RECONCILED=1; break; fi
        sleep 1
      done
      if [ "$RECONCILED" != 1 ]; then
        curl -sS --max-time 5 -X DELETE \
          "http://127.0.0.1:$PORT/v1/orchestrator/maintenance/restart" \
          -H "X-Clawdline-Orchestrator: $(cat "$TOKEN_FILE")" \
          -H 'Content-Type: application/json' \
          -d "{\"request_id\":\"$MAINTENANCE_REQUEST_ID\"}" >/dev/null || true
        echo "!! replacement did not reconcile in 180s; maintenance was explicitly aborted"
        exit 1
      fi
      MAINTENANCE_ACTIVE=0
    fi
    echo "✓ done (relaunched, since it was running before)"
  else
    echo "!! it was running before and did not come back — start it with:"
    echo "   open \"$APP\""
    exit 1
  fi
else
  echo "✓ done (it was not running, so it was left closed)"
fi
echo "  run:    open \"$APP\""
echo "  config: ~/.config/clawdline/config.json"
