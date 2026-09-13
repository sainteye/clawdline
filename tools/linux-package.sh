#!/usr/bin/env bash
# Build, verify, install, upgrade and roll back a versioned Clawdline Linux release. Installation
# trusts only a caller-pinned public key, verifies the detached signature before reading signed
# metadata, verifies the archive digest before extraction, then verifies every extracted payload
# byte against the internal manifest. Durable state is compatibility-checked but never rolled back.
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
package_helper="$script_dir/linux-package-helper.py"

package_temporary_paths=()
package_prefix=
package_transition_active=false
package_cleanup() {
  local path
  if [ "$package_transition_active" = true ] && [ -n "$package_prefix" ]; then
    python3 "$package_helper" transition abort --prefix "$package_prefix" --owner-pid "$$" \
      >/dev/null 2>&1 || true
  fi
  for path in "${package_temporary_paths[@]}"; do
    [ -n "$path" ] && [ -e "$path" ] && rm -rf -- "$path"
  done
  return 0
}
trap package_cleanup EXIT

fail() {
  echo "linux-package: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is unavailable: $1"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

json_field() {
  python3 - "$1" "$2" <<'PY'
import json, sys
with open(sys.argv[1], "rb") as handle:
    value = json.load(handle)
for component in sys.argv[2].split("."):
    value = value[component]
if isinstance(value, bool) or not isinstance(value, (str, int)):
    raise SystemExit("field is not a scalar")
print(value)
PY
}

safe_token() {
  [[ "$1" =~ ^[A-Za-z0-9._+-]+$ ]] || fail "$2 contains unsafe characters"
}

safe_version() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.+-][A-Za-z0-9.-]+)?$ ]] \
    || fail "package version must be a closed SemVer-like token"
}

root_path() {
  if [ "$install_root" = / ]; then printf '%s\n' "$1"
  else printf '%s%s\n' "${install_root%/}" "$1"
  fi
}

atomic_link() {
  local target=$1 link_path=$2 link_directory link_temp
  link_directory=$(dirname "$link_path")
  mkdir -p "$link_directory"
  link_temp="$link_directory/.link.$$.${RANDOM}"
  ln -s "$target" "$link_temp"
  mv -Tf "$link_temp" "$link_path"
  python3 - "$link_directory" <<'PY'
import os, sys
descriptor = os.open(sys.argv[1], os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
try: os.fsync(descriptor)
finally: os.close(descriptor)
PY
}

validate_provenance() {
  python3 - "$1" <<'PY'
import json, re, sys
with open(sys.argv[1], "rb") as handle:
    value = json.load(handle)
keys = {"schemaVersion", "packageVersion", "buildIdentity", "sourceCommit",
        "architecture", "archiveFile", "archiveSha256", "publicKeySha256",
        "signatureAlgorithm", "configurationSchemaVersion", "configurationReadableMinimum", "durableSchema",
        "protocolIdentity"}
if set(value) != keys:
    raise SystemExit("provenance has unknown or missing fields")
if value["schemaVersion"] != 1 or value["signatureAlgorithm"] != "openssl-rsa-sha256":
    raise SystemExit("unsupported provenance/signature schema")
if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:[.+-][A-Za-z0-9.-]+)?", value["packageVersion"]):
    raise SystemExit("unsafe package version")
for name in ("buildIdentity", "architecture", "protocolIdentity"):
    if not isinstance(value[name], str) or not re.fullmatch(r"[A-Za-z0-9._+-]+", value[name]):
        raise SystemExit("unsafe provenance token: " + name)
if not re.fullmatch(r"[0-9a-f]{40}", value["sourceCommit"]):
    raise SystemExit("sourceCommit must be an exact lowercase git object id")
for name in ("archiveSha256", "publicKeySha256"):
    if not re.fullmatch(r"[0-9a-f]{64}", value[name]):
        raise SystemExit("invalid digest: " + name)
if value["archiveFile"] != value["packageVersion"] + "-linux-amd64.tar.gz":
    raise SystemExit("archive identity does not match package version")
if value["architecture"] != "amd64":
    raise SystemExit("unsupported package architecture")
schema = value["durableSchema"]
if set(schema) != {"writeVersion", "readMinimum", "readMaximum"}:
    raise SystemExit("durable schema identity is incomplete")
if any(isinstance(schema[k], bool) or not isinstance(schema[k], int) or schema[k] < 1
       for k in schema):
    raise SystemExit("durable schema versions must be positive integers")
if not schema["readMinimum"] <= schema["writeVersion"] <= schema["readMaximum"]:
    raise SystemExit("durable schema read/write range is incoherent")
if any(isinstance(value[name], bool) or not isinstance(value[name], int) or value[name] < 1
       for name in ("configurationSchemaVersion", "configurationReadableMinimum")):
    raise SystemExit("configuration schema identity is invalid")
if value["configurationReadableMinimum"] > value["configurationSchemaVersion"]:
    raise SystemExit("configuration reader floor exceeds its write version")
PY
}

verify_snapshot() {
  local archive=$1 provenance=$2 signature=$3 public_key=$4 archive_name=$5
  [ -f "$archive" ] || fail "package archive is missing"
  [ -f "$provenance" ] || fail "signed provenance is missing"
  [ -f "$signature" ] || fail "detached signature is missing"
  [ -f "$public_key" ] || fail "pinned public key is missing"
  openssl dgst -sha256 -verify "$public_key" -signature "$signature" "$provenance" \
    >/dev/null 2>&1 || fail "provenance signature verification failed"
  validate_provenance "$provenance" || fail "signed provenance is invalid"
  local signed_key actual_key signed_archive actual_archive signed_name
  signed_key=$(json_field "$provenance" publicKeySha256)
  actual_key=$(openssl pkey -pubin -in "$public_key" -outform DER 2>/dev/null \
    | { if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi; } \
    | awk '{print $1}')
  [ "$signed_key" = "$actual_key" ] || fail "pinned public key identity does not match provenance"
  signed_name=$(json_field "$provenance" archiveFile)
  [ "$archive_name" = "$signed_name" ] || fail "archive filename does not match signed identity"
  signed_archive=$(json_field "$provenance" archiveSha256)
  actual_archive=$(sha256_file "$archive")
  [ "$signed_archive" = "$actual_archive" ] || fail "archive digest verification failed"
}

verified_archive= verified_provenance= verified_signature= verified_public_key=
verify_bundle() {
  local archive=$1 provenance=$2 signature=$3 public_key=$4 snapshot
  snapshot="${TMPDIR:-/tmp}/clawdline-package-input.$$.${RANDOM}"
  python3 "$package_helper" snapshot --directory "$snapshot" --archive "$archive" \
    --provenance "$provenance" --signature "$signature" --public-key "$public_key" \
    >/dev/null || fail "package inputs could not be pinned into immutable staging"
  package_temporary_paths+=("$snapshot")
  verified_archive="$snapshot/archive"
  verified_provenance="$snapshot/provenance"
  verified_signature="$snapshot/signature"
  verified_public_key="$snapshot/public-key"
  if [ -n "${CLAWDLINE_PACKAGE_AFTER_SNAPSHOT_HOOK:-}" ]; then
    "$CLAWDLINE_PACKAGE_AFTER_SNAPSHOT_HOOK" "$archive" "$provenance" "$signature" "$public_key"
  fi
  verify_snapshot "$verified_archive" "$verified_provenance" \
    "$verified_signature" "$verified_public_key" "$(basename "$archive")"
}

build_package() {
  local binary= version= build_identity= source_commit= signing_key= output_directory=
  local write_schema= read_minimum= read_maximum= source_epoch=0
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --binary) binary=$2; shift 2 ;;
      --version) version=$2; shift 2 ;;
      --build-identity) build_identity=$2; shift 2 ;;
      --source-commit) source_commit=$2; shift 2 ;;
      --signing-key) signing_key=$2; shift 2 ;;
      --output-dir) output_directory=$2; shift 2 ;;
      --durable-write) write_schema=$2; shift 2 ;;
      --durable-read-min) read_minimum=$2; shift 2 ;;
      --durable-read-max) read_maximum=$2; shift 2 ;;
      --source-date-epoch) source_epoch=$2; shift 2 ;;
      *) fail "unknown build option: $1" ;;
    esac
  done
  [ -x "$binary" ] || fail "--binary must name an executable regular file"
  [ -f "$signing_key" ] || fail "--signing-key is required"
  [ -n "$output_directory" ] || fail "--output-dir is required"
  safe_version "$version"
  safe_token "$build_identity" buildIdentity
  [[ "$source_commit" =~ ^[0-9a-f]{40}$ ]] || fail "source commit must be 40 lowercase hex characters"
  [[ "$source_epoch" =~ ^[0-9]+$ ]] || fail "source-date-epoch must be an integer"

  local contract_json configuration_schema configuration_read_minimum
  local binary_write binary_read_minimum binary_read_maximum protocol_identity
  contract_json=$("$binary" release-contract 2>/dev/null) \
    || fail "target binary did not report its machine-readable release contract"
  readarray -t contract_fields < <(BINARY_CONTRACT="$contract_json" python3 - <<'PY'
import json, os
value = json.loads(os.environ["BINARY_CONTRACT"])
keys = {"configurationSchemaVersion", "configurationReadableMinimum",
        "durableSchemaVersion", "durableReadableMinimum", "durableReadableMaximum",
        "protocolIdentity"}
if set(value) != keys:
    raise SystemExit("binary release contract shape is not exact")
for name in keys - {"protocolIdentity"}:
    if isinstance(value[name], bool) or not isinstance(value[name], int) or value[name] < 1:
        raise SystemExit("binary release contract contains an invalid schema")
if not value["durableReadableMinimum"] <= value["durableSchemaVersion"] <= value["durableReadableMaximum"]:
    raise SystemExit("binary durable schema range is incoherent")
if value["configurationReadableMinimum"] > value["configurationSchemaVersion"]:
    raise SystemExit("binary configuration schema range is incoherent")
print(value["configurationSchemaVersion"])
print(value["configurationReadableMinimum"])
print(value["durableSchemaVersion"])
print(value["durableReadableMinimum"])
print(value["durableReadableMaximum"])
print(value["protocolIdentity"])
PY
  )
  [ "${#contract_fields[@]}" -eq 6 ] || fail "target binary release contract is invalid"
  configuration_schema=${contract_fields[0]}; configuration_read_minimum=${contract_fields[1]}
  binary_write=${contract_fields[2]}; binary_read_minimum=${contract_fields[3]}
  binary_read_maximum=${contract_fields[4]}; protocol_identity=${contract_fields[5]}
  safe_token "$protocol_identity" protocolIdentity
  [ -z "$write_schema" ] || [ "$write_schema" = "$binary_write" ] \
    || fail "requested durable writer does not match target binary"
  [ -z "$read_minimum" ] || [ "$read_minimum" = "$binary_read_minimum" ] \
    || fail "requested durable reader minimum does not match target binary"
  [ -z "$read_maximum" ] || [ "$read_maximum" = "$binary_read_maximum" ] \
    || fail "requested durable reader maximum does not match target binary"
  write_schema=$binary_write; read_minimum=$binary_read_minimum; read_maximum=$binary_read_maximum

  mkdir -p "$output_directory"
  local stage archive provenance signature public_key archive_digest key_digest
  stage=$(mktemp -d "${TMPDIR:-/tmp}/clawdline-package-build.XXXXXX")
  package_temporary_paths+=("$stage")
  install -d -m 0755 "$stage/bin" "$stage/lib/systemd/system" \
    "$stage/lib/sysusers.d" "$stage/share/clawdline"
  install -m 0755 "$binary" "$stage/bin/ClawdlineLinux"
  install -m 0755 Packaging/linux/clawdline-daemon-wrapper "$stage/bin/clawdline-daemon-wrapper"
  install -m 0644 Packaging/systemd/clawdline-daemon.service "$stage/lib/systemd/system/clawdline-daemon.service"
  install -m 0644 Packaging/systemd/clawdline-tmux.service "$stage/lib/systemd/system/clawdline-tmux.service"
  install -m 0644 Packaging/linux/clawdline.conf "$stage/lib/sysusers.d/clawdline.conf"
  install -m 0644 Packaging/linux/daemon.json.in "$stage/share/clawdline/daemon.json.in"
  printf 'CLAWDLINE_PACKAGE_VERSION=%s\nCLAWDLINE_BUILD_IDENTITY=%s\nCLAWDLINE_SOURCE_COMMIT=%s\n' \
    "$version" "$build_identity" "$source_commit" > "$stage/release.env"
  chmod 0644 "$stage/release.env"

  python3 - "$stage" "$version" "$build_identity" "$source_commit" \
    "$configuration_schema" "$configuration_read_minimum" \
    "$write_schema" "$read_minimum" "$read_maximum" "$protocol_identity" <<'PY'
import hashlib, json, os, sys
root, version, build, source, config, config_min, write, read_min, read_max, protocol = sys.argv[1:]
paths = [
    "bin/ClawdlineLinux", "bin/clawdline-daemon-wrapper",
    "lib/systemd/system/clawdline-daemon.service",
    "lib/systemd/system/clawdline-tmux.service",
    "lib/sysusers.d/clawdline.conf", "release.env",
    "share/clawdline/daemon.json.in",
]
files = {}
for path in paths:
    with open(os.path.join(root, path), "rb") as handle:
        files[path] = hashlib.sha256(handle.read()).hexdigest()
manifest = {
    "schemaVersion": 1,
    "packageVersion": version,
    "buildIdentity": build,
    "sourceCommit": source,
    "architecture": "amd64",
    "configurationSchemaVersion": int(config),
    "configurationReadableMinimum": int(config_min),
    "durableSchema": {"writeVersion": int(write), "readMinimum": int(read_min),
                      "readMaximum": int(read_max)},
    "protocolIdentity": protocol,
    "files": files,
}
with open(os.path.join(root, "share/clawdline/release-manifest.json"), "w", encoding="utf-8") as handle:
    json.dump(manifest, handle, sort_keys=True, separators=(",", ":"))
    handle.write("\n")
PY

  archive="$output_directory/$version-linux-amd64.tar.gz"
  python3 - "$stage" "$archive" "$source_epoch" <<'PY'
import gzip, io, os, stat, sys, tarfile
root, output, epoch = sys.argv[1], sys.argv[2], int(sys.argv[3])
names = []
for base, directories, files in os.walk(root):
    directories.sort(); files.sort()
    for name in directories + files:
        names.append(os.path.relpath(os.path.join(base, name), root))
with open(output, "wb") as raw:
    with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=epoch) as zipped:
        with tarfile.open(fileobj=zipped, mode="w", format=tarfile.PAX_FORMAT) as archive:
            for name in sorted(names):
                path = os.path.join(root, name)
                metadata = os.lstat(path)
                info = tarfile.TarInfo(name)
                info.mtime = epoch; info.uid = 0; info.gid = 0; info.uname = "root"; info.gname = "root"
                info.mode = stat.S_IMODE(metadata.st_mode)
                if stat.S_ISDIR(metadata.st_mode):
                    info.type = tarfile.DIRTYPE; archive.addfile(info)
                elif stat.S_ISREG(metadata.st_mode):
                    info.size = metadata.st_size
                    with open(path, "rb") as handle: archive.addfile(info, handle)
                else:
                    raise SystemExit("package staging contains a non-regular payload")
PY
  archive_digest=$(sha256_file "$archive")
  public_key="$output_directory/$version-linux-amd64.pub.pem"
  openssl pkey -in "$signing_key" -pubout -out "$public_key" >/dev/null 2>&1 \
    || fail "signing key cannot produce an RSA public key"
  key_digest=$(openssl pkey -pubin -in "$public_key" -outform DER 2>/dev/null \
    | { if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi; } \
    | awk '{print $1}')
  provenance="$output_directory/$version-linux-amd64.provenance.json"
  python3 - "$provenance" "$version" "$build_identity" "$source_commit" \
    "$archive_digest" "$key_digest" "$configuration_schema" "$configuration_read_minimum" \
    "$write_schema" "$read_minimum" "$read_maximum" "$protocol_identity" <<'PY'
import json, os, sys
path, version, build, source, archive_digest, key_digest, config, config_min, write, read_min, read_max, protocol = sys.argv[1:]
value = {
    "schemaVersion": 1, "packageVersion": version, "buildIdentity": build,
    "sourceCommit": source, "architecture": "amd64",
    "archiveFile": version + "-linux-amd64.tar.gz", "archiveSha256": archive_digest,
    "publicKeySha256": key_digest, "signatureAlgorithm": "openssl-rsa-sha256",
    "configurationSchemaVersion": int(config),
    "configurationReadableMinimum": int(config_min),
    "durableSchema": {"writeVersion": int(write), "readMinimum": int(read_min),
                      "readMaximum": int(read_max)},
    "protocolIdentity": protocol,
}
with open(path, "w", encoding="utf-8") as handle:
    json.dump(value, handle, sort_keys=True, separators=(",", ":")); handle.write("\n")
PY
  signature="$output_directory/$version-linux-amd64.provenance.sig"
  openssl dgst -sha256 -sign "$signing_key" -out "$signature" "$provenance" \
    || fail "provenance signing failed"
  verify_bundle "$archive" "$provenance" "$signature" "$public_key"
  rm -rf -- "$stage"
  printf '{"status":"built","packageVersion":"%s","archiveSha256":"%s","sourceCommit":"%s"}\n' \
    "$version" "$archive_digest" "$source_commit"
}

extract_and_verify_release() {
  local archive=$1 provenance=$2 destination=$3
  python3 "$package_helper" extract --archive "$archive" --provenance "$provenance" \
    --destination "$destination"
}

verify_binary_contract() {
  local release_directory=$1 manifest output
  manifest=$release_directory/share/clawdline/release-manifest.json
  output=$("$release_directory/bin/ClawdlineLinux" release-contract 2>/dev/null) || return 1
  BINARY_CONTRACT="$output" python3 - "$manifest" <<'PY'
import json, os, sys
with open(sys.argv[1], "rb") as handle: manifest = json.load(handle)
binary = json.loads(os.environ["BINARY_CONTRACT"])
wanted = {
    "configurationSchemaVersion": manifest["configurationSchemaVersion"],
    "configurationReadableMinimum": manifest["configurationReadableMinimum"],
    "durableSchemaVersion": manifest["durableSchema"]["writeVersion"],
    "durableReadableMinimum": manifest["durableSchema"]["readMinimum"],
    "durableReadableMaximum": manifest["durableSchema"]["readMaximum"],
    "protocolIdentity": manifest["protocolIdentity"],
}
if binary != wanted: raise SystemExit("target binary and signed manifest contract differ")
PY
}

state_is_compatible() {
  local state_file=$1 legacy_state_file=$2 manifest=$3 expected_uid=$4
  local require_authority=${5:-false} options=()
  [ "$require_authority" = true ] && options+=(--require-authority)
  python3 "$package_helper" state-compatible --state "$state_file" \
    --legacy-state "$legacy_state_file" --manifest "$manifest" \
    --expected-uid "$expected_uid" "${options[@]}"
}

write_verified_environment() {
  local destination=$1 provenance=$2
  local package_version build_identity source_commit archive_digest
  package_version=$(json_field "$provenance" packageVersion)
  build_identity=$(json_field "$provenance" buildIdentity)
  source_commit=$(json_field "$provenance" sourceCommit)
  archive_digest=$(json_field "$provenance" archiveSha256)
  safe_version "$package_version"; safe_token "$build_identity" buildIdentity
  printf 'CLAWDLINE_PACKAGE_DIGEST=%s\n' "$archive_digest" > "$destination/verified.env"
  chmod 0644 "$destination/verified.env"
  install -m 0600 "$provenance" "$destination/verified-provenance.json"
}

install_stable_links() {
  local unit_directory sysusers_directory
  unit_directory=$(root_path /etc/systemd/system)
  sysusers_directory=$(root_path /usr/lib/sysusers.d)
  atomic_link ../../../opt/clawdline/current/lib/systemd/system/clawdline-daemon.service \
    "$unit_directory/clawdline-daemon.service"
  atomic_link ../../../opt/clawdline/current/lib/systemd/system/clawdline-tmux.service \
    "$unit_directory/clawdline-tmux.service"
  atomic_link ../../../opt/clawdline/current/lib/sysusers.d/clawdline.conf \
    "$sysusers_directory/clawdline.conf"
}

prepare_host_state() {
  local release_directory=$1
  python3 "$package_helper" prepare-state --install-root "$install_root" \
    --template "$release_directory/share/clawdline/daemon.json.in" \
    --uid "$service_uid" --gid "$service_gid" \
    --cloud-commands-enabled "$cloud_commands_enabled"
}

health_matches_release() {
  local release_directory=$1 config_file=$2 output manifest
  manifest=$release_directory/share/clawdline/release-manifest.json
  output=$("$release_directory/bin/clawdline-daemon-wrapper" health --config "$config_file" 2>/dev/null) \
    || return 1
  HEALTH_JSON="$output" python3 - "$manifest" "$release_directory/verified.env" <<'PY'
import json, os, sys
with open(sys.argv[1], "rb") as handle: manifest = json.load(handle)
with open(sys.argv[2], "r", encoding="utf-8") as handle:
    environment = dict(line.rstrip("\n").split("=", 1) for line in handle if "=" in line)
health = json.loads(os.environ["HEALTH_JSON"])
release = health.get("release", {})
if health.get("service") != "clawdline-daemon" or health.get("serviceReady") is not True:
    raise SystemExit(1)
expected_health = (
    manifest["configurationSchemaVersion"], manifest["configurationReadableMinimum"],
    manifest["durableSchema"]["writeVersion"], manifest["durableSchema"]["readMinimum"],
    manifest["durableSchema"]["readMaximum"], manifest["protocolIdentity"],
)
actual_health = (
    health.get("configurationSchemaVersion"), health.get("configurationReadableMinimum"),
    health.get("durableSchemaVersion"), health.get("durableReadableMinimum"),
    health.get("durableReadableMaximum"), health.get("protocolIdentity"),
)
if actual_health != expected_health:
    raise SystemExit(1)
expected_release = (
    manifest["packageVersion"], manifest["buildIdentity"], manifest["sourceCommit"],
    environment.get("CLAWDLINE_PACKAGE_DIGEST"),
)
actual_release = (
    release.get("packageVersion"), release.get("buildIdentity"), release.get("sourceCommit"),
    release.get("packageDigest"),
)
if actual_release != expected_release:
    raise SystemExit(1)
PY
}

restart_and_check() {
  local release_directory=$1 config_file=$2 attempts=${CLAWDLINE_HEALTH_ATTEMPTS:-20}
  "$systemctl_command" daemon-reload
  "$systemctl_command" enable clawdline-tmux.service clawdline-daemon.service
  "$systemctl_command" restart clawdline-daemon.service
  while [ "$attempts" -gt 0 ]; do
    if health_matches_release "$release_directory" "$config_file"; then return 0; fi
    attempts=$((attempts - 1))
    [ "$attempts" -gt 0 ] && sleep 1
  done
  return 1
}

safe_release_target() {
  [[ "$1" =~ ^releases/[0-9]+\.[0-9]+\.[0-9]+([.+-][A-Za-z0-9.-]+)?-[0-9a-f]{16}$ ]] \
    || fail "release link target is outside releases/<version-digest>"
}

read_release_target() {
  local link=$1
  if [ ! -e "$link" ] && [ ! -L "$link" ]; then return 0; fi
  [ -L "$link" ] || fail "release selector is not a symbolic link"
  local target
  target=$(readlink "$link")
  safe_release_target "$target"
  printf '%s\n' "$target"
}

begin_release_transition() {
  local operation=$1 old_current=${2:-none} old_previous=${3:-none}
  local new_current=${4:-none} new_previous=${5:-none}
  python3 "$package_helper" transition begin --prefix "$package_prefix" --owner-pid "$$" \
    --operation "$operation" --old-current "$old_current" --old-previous "$old_previous" \
    --new-current "$new_current" --new-previous "$new_previous"
}

abort_release_transition() {
  python3 "$package_helper" transition abort --prefix "$package_prefix" --owner-pid "$$"
  package_transition_active=false
}

mark_release_recovery() {
  local code=$1 message=$2
  python3 "$package_helper" transition recovery-required --prefix "$package_prefix" \
    --owner-pid "$$" --recovery-code "$code" --recovery-message "$message"
  package_transition_active=false
}

install_package() {
  local archive= provenance= signature= public_key=
  install_root=/; systemctl_command=/usr/bin/systemctl; restart_service=true
  service_uid= service_gid= cloud_commands_enabled=false
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --archive) archive=$2; shift 2 ;;
      --provenance) provenance=$2; shift 2 ;;
      --signature) signature=$2; shift 2 ;;
      --public-key) public_key=$2; shift 2 ;;
      --root) install_root=$2; shift 2 ;;
      --systemctl) systemctl_command=$2; shift 2 ;;
      --service-uid) service_uid=$2; shift 2 ;;
      --service-gid) service_gid=$2; shift 2 ;;
      --cloud-commands-enabled) cloud_commands_enabled=$2; shift 2 ;;
      --no-restart) restart_service=false; shift ;;
      *) fail "unknown install option: $1" ;;
    esac
  done
  verify_bundle "$archive" "$provenance" "$signature" "$public_key"
  archive=$verified_archive; provenance=$verified_provenance
  signature=$verified_signature; public_key=$verified_public_key
  if [ "$install_root" = / ]; then
    [ "$(id -u)" -eq 0 ] || fail "host installation requires root"
    systemd-sysusers Packaging/linux/clawdline.conf
    service_uid=${service_uid:-$(id -u clawdline)}
    service_gid=${service_gid:-$(id -g clawdline)}
  else
    service_uid=${service_uid:-$(id -u)}
    service_gid=${service_gid:-$(id -g)}
  fi
  [[ "$service_uid" =~ ^[0-9]+$ && "$service_gid" =~ ^[0-9]+$ ]] \
    || fail "service uid/gid must be numeric"
  [ "$cloud_commands_enabled" = true ] || [ "$cloud_commands_enabled" = false ] \
    || fail "cloud commands gate must be exactly true or false"

  local prefix releases staging version digest release_name release_directory
  local current_link previous_link old_target old_previous state_file legacy_state_file
  local config_file receipt receipt_base64 authority_required=false
  prefix=$(root_path /opt/clawdline)
  package_prefix=$prefix
  releases="$prefix/releases"
  mkdir -p "$releases" "$prefix/receipts"
  python3 "$package_helper" transition acquire --prefix "$prefix" --owner-pid "$$" \
    --operation install
  package_transition_active=true
  version=$(json_field "$provenance" packageVersion)
  digest=$(json_field "$provenance" archiveSha256)
  release_name="$version-${digest:0:16}"
  release_directory="$releases/$release_name"
  staging="$prefix/.staging.$$.${RANDOM}"
  package_temporary_paths+=("$staging")
  extract_and_verify_release "$archive" "$provenance" "$staging"
  write_verified_environment "$staging" "$provenance"
  verify_binary_contract "$staging" \
    || fail "signed metadata does not match the target binary release contract"
  python3 "$package_helper" fsync-tree --path "$staging"
  if [ -e "$release_directory" ]; then
    cmp -s "$release_directory/share/clawdline/release-manifest.json" \
      "$staging/share/clawdline/release-manifest.json" \
      || fail "an existing release identity has different manifest bytes"
    cmp -s "$release_directory/verified-provenance.json" "$provenance" \
      || fail "an existing release identity has different signed provenance"
    python3 "$package_helper" verify-release --release "$release_directory" \
      --provenance "$release_directory/verified-provenance.json"
    rm -rf "$staging"; staging=
  else
    mv "$staging" "$release_directory"; staging=
    python3 "$package_helper" fsync-tree --path "$release_directory"
  fi
  current_link="$prefix/current"
  previous_link="$prefix/previous"
  old_target=$(read_release_target "$current_link")
  old_previous=$(read_release_target "$previous_link")
  if [ -n "$old_target" ]; then
    python3 "$package_helper" verify-release --release "$prefix/$old_target" \
      --provenance "$prefix/$old_target/verified-provenance.json"
    verify_binary_contract "$prefix/$old_target" \
      || fail "current release binary no longer matches its durable identity"
  fi
  state_file=$(root_path /var/lib/clawdline/tasks/authority.json)
  legacy_state_file=$(root_path /var/lib/clawdline/records/runtime-state.json)
  if [ -e "$state_file" ] || [ -e "$legacy_state_file" ]; then authority_required=true; fi
  state_is_compatible "$state_file" "$legacy_state_file" \
    "$release_directory/share/clawdline/release-manifest.json" "$service_uid" \
    || fail "candidate image is incompatible with preserved durable state"
  install_stable_links
  prepare_host_state "$release_directory"
  config_file=$(root_path /etc/clawdline/daemon.json)
  begin_release_transition install "${old_target:-none}" "${old_previous:-none}" \
    "releases/$release_name" "${old_target:-none}"
  if [ "$restart_service" = true ]; then
    if ! restart_and_check "$release_directory" "$config_file"; then
      if [ -n "$old_target" ]; then
        if ! state_is_compatible "$state_file" "$legacy_state_file" \
          "$prefix/$old_target/share/clawdline/release-manifest.json" \
          "$service_uid" "$authority_required"; then
          mark_release_recovery rollback_state_incompatible \
            "Failed health changed durable authority; automatic old-image restart is unsafe."
          fail "new release failed exact health identity; automatic rollback was refused because the latest durable authority is unsafe for the old image"
        fi
        abort_release_transition
        "$systemctl_command" daemon-reload || true
        "$systemctl_command" restart clawdline-daemon.service || true
      else
        abort_release_transition
      fi
      fail "new release failed exact health identity; current was restored and staged artifacts were retained"
    fi
    python3 "$package_helper" transition health-passed --prefix "$prefix" --owner-pid "$$"
  fi
  receipt=$(printf '{"status":"installed","packageVersion":"%s","archiveSha256":"%s","previous":"%s"}' \
    "$version" "$digest" "${old_target:-none}")
  receipt_base64=$(RECEIPT="$receipt" python3 -c 'import base64,os; print(base64.b64encode(os.environ["RECEIPT"].encode()).decode())')
  python3 "$package_helper" transition commit --prefix "$prefix" --owner-pid "$$" \
    --receipt-base64 "$receipt_base64"
  package_transition_active=false
  printf '{"status":"installed","packageVersion":"%s","archiveSha256":"%s"}\n' "$version" "$digest"
}

rollback_package() {
  install_root=/; systemctl_command=/usr/bin/systemctl; restart_service=true
  service_uid=
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --root) install_root=$2; shift 2 ;;
      --systemctl) systemctl_command=$2; shift 2 ;;
      --service-uid) service_uid=$2; shift 2 ;;
      --no-restart) restart_service=false; shift ;;
      *) fail "unknown rollback option: $1" ;;
    esac
  done
  if [ "$install_root" = / ]; then service_uid=${service_uid:-$(id -u clawdline)}
  else service_uid=${service_uid:-$(id -u)}
  fi
  [[ "$service_uid" =~ ^[0-9]+$ ]] || fail "service uid must be numeric"
  local prefix current_link previous_link current_target target target_directory
  local state_file legacy_state_file config_file receipt receipt_base64
  prefix=$(root_path /opt/clawdline)
  package_prefix=$prefix
  current_link="$prefix/current"; previous_link="$prefix/previous"
  [ -L "$current_link" ] && [ -L "$previous_link" ] || fail "rollback requires current and previous releases"
  current_target=$(read_release_target "$current_link"); target=$(read_release_target "$previous_link")
  target_directory="$prefix/$target"
  python3 "$package_helper" transition acquire --prefix "$prefix" --owner-pid "$$" \
    --operation rollback
  package_transition_active=true
  python3 "$package_helper" verify-release --release "$target_directory" \
    --provenance "$target_directory/verified-provenance.json" \
    || fail "previous release artifact is not recoverable"
  python3 "$package_helper" verify-release --release "$prefix/$current_target" \
    --provenance "$prefix/$current_target/verified-provenance.json" \
    || fail "current release artifact is not recoverable"
  verify_binary_contract "$target_directory" \
    || fail "rollback target binary does not match its signed durable identity"
  state_file=$(root_path /var/lib/clawdline/tasks/authority.json)
  legacy_state_file=$(root_path /var/lib/clawdline/records/runtime-state.json)
  state_is_compatible "$state_file" "$legacy_state_file" \
    "$target_directory/share/clawdline/release-manifest.json" "$service_uid" \
    || fail "rollback refused: image rollback is not database rollback"
  begin_release_transition rollback "$current_target" "$target" "$target" "$current_target"
  config_file=$(root_path /etc/clawdline/daemon.json)
  if [ "$restart_service" = true ] && ! restart_and_check "$target_directory" "$config_file"; then
    if ! state_is_compatible "$state_file" "$legacy_state_file" \
      "$prefix/$current_target/share/clawdline/release-manifest.json" \
      "$service_uid" true; then
      mark_release_recovery rollback_state_incompatible \
        "Failed rollback health changed durable authority; restarting the original image is unsafe."
      fail "rollback image failed exact health identity; original-image restart was refused because the latest durable authority is unsafe"
    fi
    abort_release_transition
    "$systemctl_command" daemon-reload || true
    "$systemctl_command" restart clawdline-daemon.service || true
    fail "rollback image failed exact health identity; original current was restored"
  fi
  if [ "$restart_service" = true ]; then
    python3 "$package_helper" transition health-passed --prefix "$prefix" --owner-pid "$$"
  fi
  receipt=$(printf '{"status":"rolled_back","current":"%s","previous":"%s"}' \
    "$target" "$current_target")
  receipt_base64=$(RECEIPT="$receipt" python3 -c 'import base64,os; print(base64.b64encode(os.environ["RECEIPT"].encode()).decode())')
  python3 "$package_helper" transition commit --prefix "$prefix" --owner-pid "$$" \
    --receipt-base64 "$receipt_base64"
  package_transition_active=false
  printf '{"status":"rolled_back","current":"%s","previous":"%s"}\n' "$target" "$current_target"
}

require_command openssl
require_command python3
[ "$(uname -s)" = Linux ] || fail "Linux packaging requires a Linux host or private container"
command_name=${1:-}
[ -n "$command_name" ] || fail "usage: linux-package.sh build|verify|install|rollback"
shift
case "$command_name" in
  build) build_package "$@" ;;
  verify)
    [ "$#" -eq 4 ] || fail "verify requires ARCHIVE PROVENANCE SIGNATURE PUBLIC_KEY"
    verify_bundle "$1" "$2" "$3" "$4"
    echo '{"status":"verified"}'
    ;;
  install) install_package "$@" ;;
  rollback) rollback_package "$@" ;;
  *) fail "unknown command: $command_name" ;;
esac
