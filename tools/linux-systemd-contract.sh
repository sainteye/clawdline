#!/usr/bin/env bash
# Accumulated W4-2/W4-3 package/systemd behavior and failure-injection contract. Host systemd is never
# mutated. Exact unit parsing is local; a real disposable PID-1 Ubuntu gate is reported separately.
set -Eeuo pipefail

trap 'status=$?; printf "linux-systemd-contract: unexpected exit %s at line %s (stack %s; callers %s): %s\n" \
  "$status" "$LINENO" "${FUNCNAME[*]:-main}" "${BASH_LINENO[*]:-none}" \
  "$BASH_COMMAND" >&2; exit "$status"' ERR

script_dir=$(cd "$(dirname "$0")" && pwd)
repo_root=$(realpath "$script_dir/..")
cd "$repo_root"

fail() { echo "linux-systemd-contract: $*" >&2; exit 1; }
checks=0
check() { checks=$((checks + 1)); "$@" || fail "check $checks failed: $*"; }
expect_failure() {
  checks=$((checks + 1))
  if "$@" >/dev/null 2>&1; then fail "check $checks expected refusal: $*"; fi
}

inspect_units() {
  python3 - "$1" "$2" <<'PY'
import sys

def parse(path):
    sections = {}; current = None
    for raw in open(path, encoding="utf-8"):
        line = raw.strip()
        if not line or line.startswith("#"): continue
        if line.startswith("[") and line.endswith("]"):
            current = line[1:-1]; sections.setdefault(current, {})
            continue
        if current is None or "=" not in line: raise SystemExit("invalid unit syntax")
        key, value = line.split("=", 1)
        sections[current].setdefault(key, []).append(value)
    return sections

daemon, tmux = parse(sys.argv[1]), parse(sys.argv[2])
def one(unit, section, key, value=None):
    rows = unit.get(section, {}).get(key, [])
    if len(rows) != 1 or value is not None and rows[0] != value:
        raise SystemExit("unit singleton mismatch: %s/%s" % (section, key))
    return rows[0]

for unit in (daemon, tmux):
    one(unit, "Service", "User", "clawdline")
    one(unit, "Service", "Group", "clawdline")
    one(unit, "Service", "UMask", "0077")
    one(unit, "Service", "Restart", "on-failure")
    one(unit, "Unit", "StartLimitIntervalSec", "60s")
    one(unit, "Unit", "StartLimitBurst", "5")
    one(unit, "Install", "WantedBy", "multi-user.target")
one(daemon, "Service", "Type", "simple")
one(daemon, "Unit", "Requires", "clawdline-tmux.service")
if "RuntimeDirectory" in daemon["Service"]:
    raise SystemExit("daemon must not co-own the tmux runtime directory")
one(tmux, "Service", "Type", "simple")
one(tmux, "Service", "RuntimeDirectory", "clawdline")
one(tmux, "Service", "RuntimeDirectoryPreserve", "restart")
one(tmux, "Service", "ExecStartPre", "/usr/bin/rm -f /run/clawdline/clawdline.sock")
one(tmux, "Service", "ExecStart", "/usr/bin/tmux -D -S /run/clawdline/clawdline.sock")
if "RemainAfterExit" in tmux["Service"]:
    raise SystemExit("tmux keeper may not claim active after its PID exits")
PY
}

temporary=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-systemd-contract.XXXXXX")
trap 'rm -rf -- "$temporary"' EXIT
check bash -n tools/linux-package.sh
check bash -n Packaging/linux/clawdline-daemon-wrapper
check env PYTHONPYCACHEPREFIX="$temporary/pycache" python3 -m py_compile tools/linux-package-helper.py
check inspect_units Packaging/systemd/clawdline-daemon.service Packaging/systemd/clawdline-tmux.service
if command -v systemd-analyze >/dev/null 2>&1; then
  unit_root="$temporary/unit-root"
  mkdir -p "$unit_root/etc/systemd/system" "$unit_root/opt/clawdline/current/bin" \
    "$unit_root/usr/bin" "$unit_root/bin"
  cp Packaging/systemd/clawdline-tmux.service Packaging/systemd/clawdline-daemon.service \
    "$unit_root/etc/systemd/system/"
  for target in basic sysinit network-online multi-user; do
    printf '[Unit]\nDescription=fixture target\n' \
      > "$unit_root/etc/systemd/system/$target.target"
  done
  touch "$unit_root/usr/bin/tmux" "$unit_root/usr/bin/rm" "$unit_root/bin/sleep" \
    "$unit_root/opt/clawdline/current/bin/clawdline-daemon-wrapper"
  chmod 0755 "$unit_root/usr/bin/tmux" "$unit_root/usr/bin/rm" "$unit_root/bin/sleep" \
    "$unit_root/opt/clawdline/current/bin/clawdline-daemon-wrapper"
  check systemd-analyze --root="$unit_root" verify clawdline-tmux.service \
    clawdline-daemon.service
fi

shared_runtime="$temporary/shared-runtime.service"
cp Packaging/systemd/clawdline-daemon.service "$shared_runtime"
sed -i 's/ConfigurationDirectory=clawdline/RuntimeDirectory=clawdline\nConfigurationDirectory=clawdline/' "$shared_runtime"
expect_failure inspect_units "$shared_runtime" Packaging/systemd/clawdline-tmux.service
oneshot="$temporary/oneshot.service"
sed 's/Type=simple/Type=oneshot/; s/ -D / /' Packaging/systemd/clawdline-tmux.service > "$oneshot"
expect_failure inspect_units Packaging/systemd/clawdline-daemon.service "$oneshot"

key="$temporary/release-key.pem"
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$key" >/dev/null 2>&1
chmod 0600 "$key"
source_commit=0123456789abcdef0123456789abcdef01234567

make_binary() {
  local path=$1 service_ready=$2 durable_write=${3:-2} durable_max=${4:-2}
  {
    echo '#!/usr/bin/env bash'
    echo 'set -euo pipefail'
    printf 'ready=%q\nwrite=%q\nmaximum=%q\n' "$service_ready" "$durable_write" "$durable_max"
    cat <<'SH'
if [ "${1:-}" = release-contract ]; then
  printf '{"configurationSchemaVersion":2,"configurationReadableMinimum":1,"durableSchemaVersion":%s,"durableReadableMinimum":1,"durableReadableMaximum":%s,"protocolIdentity":"clawdline-linux-local-health-v1"}\n' "$write" "$maximum"
  exit 0
fi
if [ "${1:-}" != health ]; then exit 64; fi
printf '{"service":"clawdline-daemon","serviceReady":%s,"ready":false,"readinessCode":"w4_provider_authentication_not_proven","protocolIdentity":"clawdline-linux-local-health-v1","configurationSchemaVersion":3,"configurationReadableMinimum":1,"durableSchemaVersion":%s,"durableReadableMinimum":1,"durableReadableMaximum":%s,"release":{"packageVersion":"%s","buildIdentity":"%s","sourceCommit":"%s","packageDigest":"%s"},"reconciliation":{"authoritative":true,"stateDisposition":"loaded","schemaVersion":%s,"daemonEpoch":1,"status":"complete","terminalPresent":0,"terminalMissing":0,"terminalUnknown":0,"taskTerminal":0,"taskReconciling":0,"taskUnknown":0,"queueRecoverable":0,"queueUnknown":0,"commandSucceeded":0,"commandInterrupted":0,"commandUnknown":0,"preservedOriginal":null,"reason":null},"providers":[]}\n' \
  "$ready" "$write" "$maximum" "$CLAWDLINE_PACKAGE_VERSION" "$CLAWDLINE_BUILD_IDENTITY" \
  "$CLAWDLINE_SOURCE_COMMIT" "$CLAWDLINE_PACKAGE_DIGEST" "$write"
SH
  } > "$path"
  chmod 0755 "$path"
}

build_release() {
  local version=$1 build=$2 binary=$3 output=$4
  tools/linux-package.sh build --binary "$binary" --version "$version" \
    --build-identity "$build" --source-commit "$source_commit" --signing-key "$key" \
    --output-dir "$output" --source-date-epoch 1 >/dev/null
}
install_release() {
  local version=$1 output=$2 root=$3
  shift 3
  tools/linux-package.sh install --archive "$output/$version-linux-amd64.tar.gz" \
    --provenance "$output/$version-linux-amd64.provenance.json" \
    --signature "$output/$version-linux-amd64.provenance.sig" \
    --public-key "$output/$version-linux-amd64.pub.pem" --root "$root" \
    --service-uid "$(id -u)" --service-gid "$(id -g)" "$@"
}

good_binary="$temporary/good-binary"; bad_binary="$temporary/bad-binary"
schema_one_binary="$temporary/schema-one-binary"
make_binary "$good_binary" true 3 3
make_binary "$bad_binary" false 3 3
make_binary "$schema_one_binary" true 1 1
release_one="$temporary/release-one"; release_two="$temporary/release-two"
old_output="$temporary/old-output"; bad_output="$temporary/bad-output"
mkdir -p "$release_one" "$release_two" "$old_output" "$bad_output"
build_release 1.0.0 build-one "$good_binary" "$release_one"
build_release 1.1.0 build-two "$good_binary" "$release_two"
build_release 0.9.0 schema-one "$schema_one_binary" "$old_output"
build_release 1.2.0 bad-health "$bad_binary" "$bad_output"

check tools/linux-package.sh verify "$release_one/1.0.0-linux-amd64.tar.gz" \
  "$release_one/1.0.0-linux-amd64.provenance.json" \
  "$release_one/1.0.0-linux-amd64.provenance.sig" \
  "$release_one/1.0.0-linux-amd64.pub.pem"

bundle_names=(1.0.0-linux-amd64.tar.gz 1.0.0-linux-amd64.provenance.json \
  1.0.0-linux-amd64.provenance.sig 1.0.0-linux-amd64.pub.pem)
for input_index in 0 1 2 3; do
  for link_kind in symlink hardlink; do
    attack_bundle="$temporary/input-$input_index-$link_kind"; mkdir "$attack_bundle"
    for bundle_name in "${bundle_names[@]}"; do
      cp "$release_one/$bundle_name" "$attack_bundle/$bundle_name"
    done
    attacked_name=${bundle_names[$input_index]}
    rm "$attack_bundle/$attacked_name"
    if [ "$link_kind" = symlink ]; then
      ln -s "$release_one/$attacked_name" "$attack_bundle/$attacked_name"
    else
      ln "$release_one/$attacked_name" "$attack_bundle/$attacked_name"
    fi
    expect_failure tools/linux-package.sh verify \
      "$attack_bundle/${bundle_names[0]}" "$attack_bundle/${bundle_names[1]}" \
      "$attack_bundle/${bundle_names[2]}" "$attack_bundle/${bundle_names[3]}"
    rm "$attack_bundle/$attacked_name"
  done
done

mutated_archive="$temporary/duplicate.tar.gz"
python3 - "$release_one/1.0.0-linux-amd64.tar.gz" "$mutated_archive" <<'PY'
import sys, tarfile
with tarfile.open(sys.argv[1], "r:gz") as source, tarfile.open(sys.argv[2], "w:gz") as output:
    for member in source.getmembers():
        data = source.extractfile(member) if member.isreg() else None
        output.addfile(member, data)
    member = source.getmember("release.env")
    output.addfile(member, source.extractfile(member))
PY
expect_failure python3 tools/linux-package-helper.py extract --archive "$mutated_archive" \
  --provenance "$release_one/1.0.0-linux-amd64.provenance.json" \
  --destination "$temporary/duplicate-extract"

for mutation in symlink hardlink device traversal unexpected; do
  mutated_archive="$temporary/$mutation.tar.gz"
  python3 - "$release_one/1.0.0-linux-amd64.tar.gz" "$mutated_archive" "$mutation" <<'PY'
import copy, io, sys, tarfile
source_path, destination, mutation = sys.argv[1:]
with tarfile.open(source_path, "r:gz") as source, tarfile.open(destination, "w:gz") as output:
    for original in source.getmembers():
        member = copy.copy(original)
        data = source.extractfile(original) if original.isreg() else None
        if original.name == "release.env" and mutation in {"symlink", "hardlink", "device", "traversal"}:
            if mutation == "symlink":
                member.type, member.linkname, member.size, data = tarfile.SYMTYPE, "bin/ClawdlineLinux", 0, None
            elif mutation == "hardlink":
                member.type, member.linkname, member.size, data = tarfile.LNKTYPE, "bin/ClawdlineLinux", 0, None
            elif mutation == "device":
                member.type, member.devmajor, member.devminor, member.size, data = tarfile.CHRTYPE, 1, 3, 0, None
            else:
                member.name = "../escape"
        output.addfile(member, data)
    if mutation == "unexpected":
        member = tarfile.TarInfo("share/clawdline/unsigned-extra")
        member.mode, member.uid, member.gid, member.size = 0o644, 0, 0, 1
        output.addfile(member, io.BytesIO(b"x"))
PY
  expect_failure python3 tools/linux-package-helper.py extract --archive "$mutated_archive" \
    --provenance "$release_one/1.0.0-linux-amd64.provenance.json" \
    --destination "$temporary/$mutation-extract"
done
check test ! -e "$temporary/escape"

swap_archive="$temporary/swap/1.0.0-linux-amd64.tar.gz"; mkdir -p "$(dirname "$swap_archive")"
cp "$release_one/1.0.0-linux-amd64.tar.gz" "$swap_archive"
cp "$release_one/1.0.0-linux-amd64.provenance.json" \
  "$release_one/1.0.0-linux-amd64.provenance.sig" \
  "$release_one/1.0.0-linux-amd64.pub.pem" "$temporary/swap/"
swap_hook="$temporary/swap-hook"
cat > "$swap_hook" <<'SH'
#!/usr/bin/env bash
for input in "$@"; do printf unsigned-replacement > "$input"; done
SH
chmod 0755 "$swap_hook"
swap_root="$temporary/swap-root"
CLAWDLINE_PACKAGE_AFTER_SNAPSHOT_HOOK="$swap_hook" install_release 1.0.0 "$temporary/swap" \
  "$swap_root" --no-restart >/dev/null
check cmp "$swap_root/opt/clawdline/current/bin/ClawdlineLinux" "$good_binary"
for bundle_name in "${bundle_names[@]}"; do
  check grep -q unsigned-replacement "$temporary/swap/$bundle_name"
done

fake_systemctl="$temporary/systemctl"; export FAKE_SYSTEMCTL_LOG="$temporary/systemctl.log"
cat > "$fake_systemctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FAKE_SYSTEMCTL_LOG"
SH
chmod 0755 "$fake_systemctl"

upgrade_root="$temporary/upgrade-root"
install_release 0.9.0 "$old_output" "$upgrade_root" --systemctl "$fake_systemctl" >/dev/null
schema_one_target=$(readlink "$upgrade_root/opt/clawdline/current")
install_release 1.0.0 "$release_one" "$upgrade_root" --systemctl "$fake_systemctl" >/dev/null
schema_three_target=$(readlink "$upgrade_root/opt/clawdline/current")
check test "$schema_one_target" != "$schema_three_target"
mkdir -p "$upgrade_root/var/lib/clawdline/tasks" "$upgrade_root/var/lib/clawdline/records"
# The canonical task authority wins over a stale migration source. If package compatibility
# regresses to the records pathname, this rollback incorrectly succeeds and the check goes red.
printf '{"schemaVersion":3,"minimumReaderVersion":3}\n' > "$upgrade_root/var/lib/clawdline/tasks/authority.json"
chmod 0600 "$upgrade_root/var/lib/clawdline/tasks/authority.json"
printf '{"schemaVersion":1,"minimumReaderVersion":1}\n' > "$upgrade_root/var/lib/clawdline/records/runtime-state.json"
chmod 0600 "$upgrade_root/var/lib/clawdline/records/runtime-state.json"
expect_failure tools/linux-package.sh rollback --root "$upgrade_root" --systemctl "$fake_systemctl"
check test "$(readlink "$upgrade_root/opt/clawdline/current")" = "$schema_three_target"
# A genuinely schema-1 canonical state remains compatible with the signed schema-1 target.
printf '{"schemaVersion":1,"minimumReaderVersion":1}\n' > "$upgrade_root/var/lib/clawdline/tasks/authority.json"
chmod 0600 "$upgrade_root/var/lib/clawdline/tasks/authority.json"
tools/linux-package.sh rollback --root "$upgrade_root" --systemctl "$fake_systemctl" >/dev/null
check test "$(readlink "$upgrade_root/opt/clawdline/current")" = "$schema_one_target"
check grep -q '^enable clawdline-tmux.service clawdline-daemon.service$' "$FAKE_SYSTEMCTL_LOG"
check cmp "$upgrade_root/opt/clawdline/current/lib/systemd/system/clawdline-daemon.service" \
  Packaging/systemd/clawdline-daemon.service

# A fence is non-authority even when both images otherwise accept schema 3.
fence_root="$temporary/fence-root"
install_release 1.0.0 "$release_one" "$fence_root" --systemctl "$fake_systemctl" >/dev/null
install_release 1.1.0 "$release_two" "$fence_root" --systemctl "$fake_systemctl" >/dev/null
fence_current=$(readlink "$fence_root/opt/clawdline/current")
mkdir -p "$fence_root/var/lib/clawdline/records"
printf '{"schemaVersion":3,"minimumReaderVersion":3,"recordKind":"clawdline_linux_task_authority_rollback_fence"}\n' \
  > "$fence_root/var/lib/clawdline/records/runtime-state.json"
chmod 0600 "$fence_root/var/lib/clawdline/records/runtime-state.json"
expect_failure tools/linux-package.sh rollback --root "$fence_root" --systemctl "$fake_systemctl"
check test "$(readlink "$fence_root/opt/clawdline/current")" = "$fence_current"

# Restore the schema-3 selector, then prove every other daemon authority predicate fails before a
# rollback can change that selector: unknown recordKind, service UID, private regular single-link
# file and bounded bytes.
install_release 1.0.0 "$release_one" "$upgrade_root" --systemctl "$fake_systemctl" >/dev/null
check test "$(readlink "$upgrade_root/opt/clawdline/current")" = "$schema_three_target"
authority="$upgrade_root/var/lib/clawdline/tasks/authority.json"
legacy_authority="$upgrade_root/var/lib/clawdline/records/runtime-state.json"
printf '{"schemaVersion":1,"minimumReaderVersion":1,"recordKind":"unknown"}\n' > "$authority"
chmod 0600 "$authority"
expect_failure tools/linux-package.sh rollback --root "$upgrade_root" --systemctl "$fake_systemctl"
check test "$(readlink "$upgrade_root/opt/clawdline/current")" = "$schema_three_target"
printf '{"schemaVersion":1,"minimumReaderVersion":1}\n' > "$authority"
chmod 0600 "$authority"
expect_failure tools/linux-package.sh rollback --root "$upgrade_root" --systemctl "$fake_systemctl" \
  --service-uid "$(( $(id -u) + 1 ))"
check test "$(readlink "$upgrade_root/opt/clawdline/current")" = "$schema_three_target"
authority_original="$temporary/authority-original.json"
mv "$authority" "$authority_original"
ln "$authority_original" "$authority"
expect_failure tools/linux-package.sh rollback --root "$upgrade_root" --systemctl "$fake_systemctl"
check test "$(readlink "$upgrade_root/opt/clawdline/current")" = "$schema_three_target"
rm "$authority"; mv "$authority_original" "$authority"; chmod 0644 "$authority"
expect_failure tools/linux-package.sh rollback --root "$upgrade_root" --systemctl "$fake_systemctl"
check test "$(readlink "$upgrade_root/opt/clawdline/current")" = "$schema_three_target"
chmod 0600 "$authority"; truncate -s $((8 * 1024 * 1024 + 1)) "$authority"
expect_failure tools/linux-package.sh rollback --root "$upgrade_root" --systemctl "$fake_systemctl"
check test "$(readlink "$upgrade_root/opt/clawdline/current")" = "$schema_three_target"
printf '{"schemaVersion":1,"minimumReaderVersion":1}\n' > "$authority"; chmod 0600 "$authority"

# A failed-health candidate may have migrated schema-2 bytes to canonical schema 3. Re-read that
# latest authority before moving the selector: the schema-1 old image must not be restarted, and
# the journal must carry typed operator recovery instead of silently restoring an unsafe image.
migration_root="$temporary/failed-health-migration-root"
install_release 0.9.0 "$old_output" "$migration_root" --systemctl "$fake_systemctl" >/dev/null
migration_old_target=$(readlink "$migration_root/opt/clawdline/current")
mkdir -p "$migration_root/var/lib/clawdline/records"
printf '{"schemaVersion":2,"minimumReaderVersion":1}\n' \
  > "$migration_root/var/lib/clawdline/records/runtime-state.json"
chmod 0600 "$migration_root/var/lib/clawdline/records/runtime-state.json"
migration_systemctl="$temporary/migration-systemctl"
export MIGRATION_SYSTEMCTL_ROOT="$migration_root" MIGRATION_SYSTEMCTL_LOG="$temporary/migration-systemctl.log"
cat > "$migration_systemctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$MIGRATION_SYSTEMCTL_LOG"
if [ "$1" = restart ]; then
  mkdir -p "$MIGRATION_SYSTEMCTL_ROOT/var/lib/clawdline/tasks" \
    "$MIGRATION_SYSTEMCTL_ROOT/var/lib/clawdline/records"
  printf '{"schemaVersion":3,"minimumReaderVersion":3}\n' \
    > "$MIGRATION_SYSTEMCTL_ROOT/var/lib/clawdline/tasks/authority.json"
  printf '{"schemaVersion":3,"minimumReaderVersion":3,"recordKind":"clawdline_linux_task_authority_rollback_fence"}\n' \
    > "$MIGRATION_SYSTEMCTL_ROOT/var/lib/clawdline/records/runtime-state.json"
  chmod 0600 "$MIGRATION_SYSTEMCTL_ROOT/var/lib/clawdline/tasks/authority.json" \
    "$MIGRATION_SYSTEMCTL_ROOT/var/lib/clawdline/records/runtime-state.json"
fi
SH
chmod 0755 "$migration_systemctl"
CLAWDLINE_HEALTH_ATTEMPTS=1 expect_failure install_release 1.2.0 "$bad_output" \
  "$migration_root" --systemctl "$migration_systemctl"
check test "$(readlink "$migration_root/opt/clawdline/current")" != "$migration_old_target"
check grep -q '"phase":"recovery_required"' "$migration_root/opt/clawdline/transition.json"
check grep -q '"code":"rollback_state_incompatible"' \
  "$migration_root/opt/clawdline/transition.json"
check test "$(grep -c '^restart clawdline-daemon.service$' "$MIGRATION_SYSTEMCTL_LOG")" = 1

mismatch_binary="$temporary/mismatch-binary"
make_binary "$mismatch_binary" true 1 1
expect_failure tools/linux-package.sh build --binary "$mismatch_binary" --version 3.0.0 \
  --build-identity mismatch --source-commit "$source_commit" --signing-key "$key" \
  --output-dir "$temporary/mismatch-output" --durable-write 2

attack_root="$temporary/attack-root"
install_release 1.0.0 "$release_one" "$attack_root" --no-restart >/dev/null
outside="$temporary/outside"; mkdir "$outside"; printf sentinel > "$outside/keep"; chmod 0644 "$outside/keep"
before=$(sha256sum "$outside/keep" | awk '{print $1}')
mv "$attack_root/var/lib/clawdline/secrets" "$attack_root/var/lib/clawdline/secrets-real"
ln -s "$outside" "$attack_root/var/lib/clawdline/secrets"
expect_failure install_release 1.1.0 "$release_two" "$attack_root" --no-restart
check test "$(sha256sum "$outside/keep" | awk '{print $1}')" = "$before"
check test "$(stat -c %a "$outside/keep")" = 644

final_root="$temporary/final-root"
install_release 1.0.0 "$release_one" "$final_root" --no-restart >/dev/null
external_file="$temporary/external-file"; printf external > "$external_file"; chmod 0644 "$external_file"
mv "$final_root/var/lib/clawdline/secrets/daemon.secret" \
  "$final_root/var/lib/clawdline/secrets/original.secret"
ln -s "$external_file" "$final_root/var/lib/clawdline/secrets/daemon.secret"
expect_failure install_release 1.1.0 "$release_two" "$final_root" --no-restart
check test "$(stat -c %a "$external_file")" = 644

hardlink_root="$temporary/secret-hardlink-root"
install_release 1.0.0 "$release_one" "$hardlink_root" --no-restart >/dev/null
hardlink_external="$temporary/hardlink-external"; printf hardlink-external > "$hardlink_external"
chmod 0644 "$hardlink_external"
mv "$hardlink_root/var/lib/clawdline/secrets/daemon.secret" \
  "$hardlink_root/var/lib/clawdline/secrets/original.secret"
ln "$hardlink_external" "$hardlink_root/var/lib/clawdline/secrets/daemon.secret"
expect_failure install_release 1.1.0 "$release_two" "$hardlink_root" --no-restart
check test "$(stat -c %a "$hardlink_external")" = 644

swap_state_root="$temporary/state-swap-root"; mkdir -p "$swap_state_root"
swap_external="$temporary/state-swap-external"; mkdir "$swap_external"
swap_state_hook="$temporary/state-swap-hook"
cat > "$swap_state_hook" <<'SH'
#!/usr/bin/env bash
root=$1
mv "$root/var/lib/clawdline/secrets" "$root/var/lib/clawdline/secrets-moved"
ln -s "$CLAWDLINE_STATE_SWAP_EXTERNAL" "$root/var/lib/clawdline/secrets"
SH
chmod 0755 "$swap_state_hook"
export CLAWDLINE_STATE_AFTER_OPEN_HOOK="$swap_state_hook" CLAWDLINE_STATE_SWAP_EXTERNAL="$swap_external"
expect_failure install_release 1.0.0 "$release_one" "$swap_state_root" --no-restart
unset CLAWDLINE_STATE_AFTER_OPEN_HOOK CLAWDLINE_STATE_SWAP_EXTERNAL
check test -z "$(find "$swap_external" -mindepth 1 -print -quit)"

tamper_root="$temporary/tamper-root"
install_release 1.0.0 "$release_one" "$tamper_root" --no-restart >/dev/null
printf tamper >> "$tamper_root/opt/clawdline/current/bin/ClawdlineLinux"
expect_failure install_release 1.0.0 "$release_one" "$tamper_root" --no-restart

transition_fixture() {
  local root=$1
  mkdir -p "$root/releases/1.0.0-aaaaaaaaaaaaaaaa" \
    "$root/releases/0.9.0-bbbbbbbbbbbbbbbb" "$root/releases/1.1.0-cccccccccccccccc"
  ln -s releases/1.0.0-aaaaaaaaaaaaaaaa "$root/current"
  ln -s releases/0.9.0-bbbbbbbbbbbbbbbb "$root/previous"
}
for point in after_lock after_prepared after_previous_link after_current_link after_switched after_health_passed; do
  crash_root="$temporary/crash-$point"; transition_fixture "$crash_root"
  expect_failure bash -c 'set -e; helper=$1; root=$2; point=$3
    CLAWDLINE_TRANSITION_CRASH_AT="$point" python3 "$helper" transition acquire \
      --prefix "$root" --owner-pid "$$" --operation test
    CLAWDLINE_TRANSITION_CRASH_AT="$point" python3 "$helper" transition begin --prefix "$root" \
      --owner-pid "$$" --operation test --old-current releases/1.0.0-aaaaaaaaaaaaaaaa \
      --old-previous releases/0.9.0-bbbbbbbbbbbbbbbb \
      --new-current releases/1.1.0-cccccccccccccccc \
      --new-previous releases/1.0.0-aaaaaaaaaaaaaaaa
    if [ "$point" = after_health_passed ]; then
      CLAWDLINE_TRANSITION_CRASH_AT="$point" python3 "$helper" transition health-passed \
        --prefix "$root" --owner-pid "$$"
    fi' _ \
    "$repo_root/tools/linux-package-helper.py" "$crash_root" "$point"
  python3 tools/linux-package-helper.py transition acquire --prefix "$crash_root" \
    --owner-pid "$$" --operation recovery
  check test "$(readlink "$crash_root/current")" = releases/1.0.0-aaaaaaaaaaaaaaaa
  check test "$(readlink "$crash_root/previous")" = releases/0.9.0-bbbbbbbbbbbbbbbb
  python3 tools/linux-package-helper.py transition abort --prefix "$crash_root" --owner-pid "$$"
done

receipt_crash="$temporary/crash-after-receipt"; transition_fixture "$receipt_crash"
expect_failure bash -c 'set -e; helper=$1; root=$2
  python3 "$helper" transition acquire --prefix "$root" --owner-pid "$$" --operation test
  python3 "$helper" transition begin --prefix "$root" --owner-pid "$$" --operation test \
    --old-current releases/1.0.0-aaaaaaaaaaaaaaaa \
    --old-previous releases/0.9.0-bbbbbbbbbbbbbbbb \
    --new-current releases/1.1.0-cccccccccccccccc \
    --new-previous releases/1.0.0-aaaaaaaaaaaaaaaa
  python3 "$helper" transition health-passed --prefix "$root" --owner-pid "$$"
  CLAWDLINE_TRANSITION_CRASH_AT=after_receipt python3 "$helper" transition commit \
    --prefix "$root" --owner-pid "$$" --receipt-base64 eyJzdGF0dXMiOiJ0ZXN0In0=' _ \
  "$repo_root/tools/linux-package-helper.py" "$receipt_crash"
check grep -q '"phase":"committing"' "$receipt_crash/transition.json"
check grep -q '"status":"test"' "$receipt_crash/transition.json"
check test "$(readlink "$receipt_crash/current")" = releases/1.1.0-cccccccccccccccc
check test "$(readlink "$receipt_crash/previous")" = releases/1.0.0-aaaaaaaaaaaaaaaa
python3 tools/linux-package-helper.py transition acquire --prefix "$receipt_crash" \
  --owner-pid "$$" --operation recovery
check test "$(readlink "$receipt_crash/current")" = releases/1.1.0-cccccccccccccccc
check grep -q '"status":"test"' "$receipt_crash/receipts/last-install.json"
python3 tools/linux-package-helper.py transition abort --prefix "$receipt_crash" --owner-pid "$$"

echo "linux-systemd-contract: PASS — $checks exact-unit, immutable-input, root-boundary, schema-health and crash-recovery checks passed; PID1_UBUNTU=external_required"
