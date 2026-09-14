# Install and validate Clawdline Linux on AWS

This runbook is for an open-source operator who wants a disposable Ubuntu 24.04 amd64 executor on
Amazon EC2. It installs the signed Clawdline Linux package, enrolls the machine with Clawdline
Cloud, and verifies that the hosted console can list and create Sessions on an allowlisted project.
It is not a production-control-plane deployment guide.

The safe default is an EC2 instance with no inbound security-group rules. Use AWS Systems Manager
Session Manager for administration and allow only the outbound traffic needed for Ubuntu package
mirrors, GitHub, the provider, `api.clawdline.com`, and `relay.clawdline.com`. Require IMDSv2 and an
encrypted EBS volume. Never put a device token, provider credential, GitHub credential, Session
content, or package signing private key in user data, tags, command output, or logs.

## 1. Host prerequisites

Use Ubuntu 24.04 on amd64. The current release contract requires `systemd`, `tmux`, `python3`,
`openssl`, Git, and the selected provider executable. Node and `jq` are needed by the repository
validation and first-install configuration steps. The signed installer creates the non-root
`clawdline` service account; create and authenticate the project only after that first install.

```sh
sudo apt-get update
sudo apt-get install -y ca-certificates git jq nodejs openssl python3 systemd tmux
```

If the security group permits HTTPS but not HTTP, change every Ubuntu apt source to HTTPS before
`apt-get update`. Otherwise the host can reach Cloud and SSM while package installation fails with
misleading mirror timeouts.

Install Claude Code and/or Codex from their official distribution at exact executable paths such as
`/usr/bin/claude` and `/usr/bin/codex`. Authentication and the project checkout happen after the
installer has created the service identity and its protected HOME. For the W5-4 acceptance example
the sole allowlisted project is:

```text
/var/lib/clawdline-projects/reaver
```

The browser receives that bounded place identity, not an arbitrary filesystem picker. The provider
may read and write only admitted roots after the Linux containment policy is installed.

## 2. Build on Ubuntu or in the pinned Swift image

The supported source gate is:

```sh
./tools/swift-core-application-linux-build.sh
```

It must run on Ubuntu 24.04 amd64 as a non-root user and leaves the static executable under
`.build`. When a disposable EC2 host does not carry a Swift toolchain, use the exact image digest
printed in `tools/swift-core-application-linux-build.sh` and mount a writable source checkout.
SwiftPM needs to write `.build`; a read-only mount is intentionally rejected.

Do not blindly run `useradd -u 1000` inside the official Swift image. Its Ubuntu base can already
contain `ubuntu:x:1000:1000`, in which case `useradd` fails before compilation. Reuse the existing
numeric identity instead:

```sh
docker run --rm --platform linux/amd64 \
  -v "$PWD:/workspace" \
  swift:6.1.3-noble@sha256:1d10836ca929943d70b2254e555b17247b132f6eed984654351cb6a8cc531613 \
  bash -lc 'set -eu
    sed -i "s,http://,https://,g" /etc/apt/sources.list \
      /etc/apt/sources.list.d/ubuntu.sources 2>/dev/null || true
    apt-get update -qq
    apt-get install -y --no-install-recommends tmux nodejs python3 openssl systemd >/dev/null
    mkdir -p /workspace/.native-tmp /workspace/.build
    chown -R ubuntu:ubuntu /workspace/.native-tmp /workspace/.build
    runuser -u ubuntu -- env HOME=/home/ubuntu TMPDIR=/workspace/.native-tmp \
      SWIFT_CORE_APPLICATION_LINUX_BUILD_CONFIGURATION=release \
      SWIFT_CORE_APPLICATION_LINUX_BUILD_JOBS=2 \
      bash /workspace/tools/swift-core-application-linux-build.sh'
```

Keep the test `TMPDIR` short. The documented `/workspace/.native-tmp` produces a 106-byte tmux
socket path once the runtime UUID and `/state/runtime/clawdline.sock` are appended, only one byte
below Linux's 107-byte usable `sun_path` limit. A longer checkout or temp root can make tmux socket
creation fail. Prefer a short, owned `0700` root such as `/tmp/clawdline-native` when adapting the
command, and check the final socket path's UTF-8 byte count rather than its character count.

Before using UID 1000, inspect it with `getent passwd 1000`; if it differs, use the owner of the
writable checkout and set a real writable `HOME`. Do not run the product gate as root merely to
avoid a host/container ownership mismatch.

If source reaches EC2 through S3, grant the instance role only `s3:GetObject` for the exact source
prefix, verify the bundle SHA-256 before `git fetch`, and keep the bucket private and versioned.
An SSM command does not inherit access merely because an administrator can download the object.
Do not use a public object or presigned URL as a credential workaround.

## 3. Build and verify a signed package

Generate a release signing key outside durable service state. The private key is release input and
must not be copied into `/opt/clawdline`, `/var/lib/clawdline`, an artifact bucket, or a receipt.
For a disposable acceptance environment an ephemeral key may be generated and destroyed after the
signed archive, provenance, signature, and public key have been retained together.

```sh
umask 077
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out release-signing-key.pem

./tools/linux-package.sh build \
  --binary .build/release/ClawdlineLinux \
  --version 0.5.4-aws-alpha.1 \
  --build-identity aws-alpha-1 \
  --source-commit "$(git rev-parse HEAD)" \
  --signing-key release-signing-key.pem \
  --output-dir package-out \
  --source-date-epoch "$(git show -s --format=%ct HEAD)"

./tools/linux-package.sh verify \
  package-out/0.5.4-aws-alpha.1-linux-amd64.tar.gz \
  package-out/0.5.4-aws-alpha.1-linux-amd64.provenance.json \
  package-out/0.5.4-aws-alpha.1-linux-amd64.provenance.sig \
  package-out/0.5.4-aws-alpha.1-linux-amd64.pub.pem
```

The candidate must use signed schema 2. The installer can read an already installed signed schema-1
release only for exact upgrade/rollback compatibility; schema 1 is not accepted for a new package.

## 4. Install, enroll, and start

Install with the intended Cloud write gate but do not start the daemon yet. The write-gate value is
an immutable explicit installation choice: a later install refuses to silently toggle it. For an
executor that the hosted console may use to create or send to Sessions, choose `true` on this first
install:

```sh
sudo ./tools/linux-package.sh install \
  --archive package-out/0.5.4-aws-alpha.1-linux-amd64.tar.gz \
  --provenance package-out/0.5.4-aws-alpha.1-linux-amd64.provenance.json \
  --signature package-out/0.5.4-aws-alpha.1-linux-amd64.provenance.sig \
  --public-key package-out/0.5.4-aws-alpha.1-linux-amd64.pub.pem \
  --cloud-commands-enabled true \
  --no-restart
```

The no-restart install has now created the `clawdline` identity, its protected state/HOME, and the
default configuration. Authenticate providers and clone the approved project as that identity, not
as root or `ubuntu`:

```sh
sudo install -d -o clawdline -g clawdline -m 0700 /var/lib/clawdline/home
sudo -u clawdline env HOME=/var/lib/clawdline/home /usr/bin/codex login
# Run the official Claude Code login flow the same way if Claude is enabled.
# Configure GitHub authentication in this same HOME before testing a push to a private repository.
sudo -u clawdline env HOME=/var/lib/clawdline/home \
  git clone https://github.com/sainteye/reaver.git /var/lib/clawdline-projects/reaver
sudo chown -R clawdline:clawdline /var/lib/clawdline-projects/reaver
```

Before the first daemon start, replace the broad template root with the exact allowlisted project
and add the bounded AWS presentation. The temporary file is separate, 0640, root/service-group
owned, atomically renamed, and the containing directory is synced:

```sh
config_tmp=$(mktemp)
sudo jq '.runtime.projectRoots=["/var/lib/clawdline-projects/reaver"]
    | .runtime.displayName="Clawdline Linux on AWS"
    | .runtime.infrastructureProvider="aws"' \
  /etc/clawdline/daemon.json > "$config_tmp"
sudo install -o root -g clawdline -m 0640 "$config_tmp" /etc/clawdline/.daemon.json.new
sudo mv -f /etc/clawdline/.daemon.json.new /etc/clawdline/daemon.json
sudo sync -f /etc/clawdline
rm -f "$config_tmp"
```

Review `/etc/clawdline/daemon.json`. It must name numeric service uid/gid, exactly
`/var/lib/clawdline-projects/reaver`, exact provider/tmux executables, the bounded display label,
and `"infrastructureProvider":"aws"`. The tmux unit must use this exact foreground command:

```text
/usr/bin/tmux -D -S /run/clawdline/clawdline.sock
```

Adding `new-session` to that `tmux -D` invocation is invalid on tmux 3.4 and creates a systemd
restart loop.

The daemon has not started. Run `cloud-login` through the packaged wrapper as the service identity.
Only the one-time user code and verification URL may be copied to the browser:

```sh
sudo -u clawdline /opt/clawdline/current/bin/clawdline-daemon-wrapper \
  cloud-login --config /etc/clawdline/daemon.json
```

After the invitation is approved, start the two installed services. Do not reinstall to change the
gate, and do not edit the durable identity store, writer locks, spool, or Session rows by hand.

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now clawdline-tmux.service clawdline-daemon.service
```

## 5. Acceptance checklist

The installation is accepted only when all of these are observed on the exact signed source:

1. `systemctl is-active clawdline-tmux.service clawdline-daemon.service` remains active for at
   least 60 seconds, and both existing lock inodes are reacquired after restart without deletion.
2. The packaged configured-health command below reports the signed package/build/source tuple; bare
   `ClawdlineLinux health` is only the unconfigured compiled-adapter diagnostic and is insufficient:

   ```sh
   sudo -u clawdline /opt/clawdline/current/bin/clawdline-daemon-wrapper \
     health --config /etc/clawdline/daemon.json
   ```

   Also prove the newline-JSON loopback listener without printing its real secret or causing an
   effect. This deliberately invalid credential must return typed `ingress_unauthorized`:

   ```sh
   python3 - <<'PY'
   import json, socket
   request = {"authorization":"aW52YWxpZC1wcm9iZQ==","operation":"observe",
              "commandID":"listener-probe","taskID":"listener-probe","sessionID":"%999999"}
   with socket.create_connection(("127.0.0.1", 7718), timeout=5) as connection:
       connection.sendall(json.dumps(request, separators=(",", ":")).encode() + b"\n")
       connection.shutdown(socket.SHUT_WR)
       response = connection.makefile("rb").readline()
   value = json.loads(response)
   assert value.get("error", {}).get("code") == "ingress_unauthorized", value
   print(json.dumps({"listener":"bounded","authorization":"refused"}))
   PY
   ```
3. The Relay authenticates without logging the Bearer or plaintext Session data. Revocation stops
   reconnect; transient failure retries with the bounded policy.
4. `app.clawdline.com` shows the Linux/AWS machine label on its Session rows. The New Session sheet
   can select that machine and only the configured project (`reaver`).
5. A hosted command creates a Claude Code or Codex Session, applies one benign change on a new
   branch, runs the repository test, and pushes that branch. The default branch is unchanged and
   the exact remote commit is recorded.
6. Restart and EC2 reboot preserve the machine identity, Session inventory, ready/sent durable
   rows, ordering, and the allowlist. No manual state deletion is part of recovery.
7. Install a second signed compatible package, prove rollback to the first and forward recovery to
   the second, then retain the exact receipts. Image rollback is never database rollback.

Common failure classifications:

- `useradd: UID 1000 is not unique`: reuse the image's existing UID instead of creating it.
- `swift test` reports `Unknown option '--static-swift-stdlib'`: static linkage belongs on the
  product `swift build` command, not the XCTest invocation; use the repository script containing
  this correction.
- XCTest linking reports missing `_FoundationCollections`, `_FoundationCShims`,
  `_DispatchStubs`, or `CoreFoundation` after the static product succeeds: do not reuse the
  product's `.build` objects for the dynamically linked test runner. The repository source gate
  deliberately uses `.build-linux-tests` for XCTest and `.build` for the packaged static binary.
- apt mirror timeout with otherwise healthy HTTPS: the restricted egress policy still references
  `http://` package sources.
- S3 `AccessDenied`: the EC2 instance profile lacks exact-prefix `s3:GetObject`; do not make the
  object public.
- Git reports `detected dubious ownership` after a root SSM step hands the checkout to the non-root
  build user: run subsequent Git commands as that checkout owner. For a root-only orchestration
  step, scope `git -c safe.directory=/the/exact/checkout ...` to that one canonical path; never set
  global `safe.directory=*`.
- a real tmux create exits immediately with `tmux created no identity-bearing PTY receipt` and the
  sandbox helper exits 126: use a release whose Landlock rules distinguish directories, ordinary
  files, executables, and writable character devices. Applying the directory-only `READ_DIR`
  right to `/etc/*`, the provider executable, or `/dev/*` is rejected by Linux before the provider
  can start; granting `/dev/null` read-only also makes the containment probe fail for the wrong
  reason. Do not disable Landlock as a workaround.
- a real tmux create reaches the provider but ends as `terminal_timeout`: older Linux builds used
  Foundation's inherited process-liveness descriptor. The persistent tmux server kept that hidden
  descriptor open after the direct tmux client exited, so Clawdline waited for the server and then
  mislabeled a successful create as a timeout. Use a release with daemon-safe descriptor closure,
  reconcile the exact tmux inventory before retrying, and never widen the deadline, delete the
  socket, or disable Landlock to hide the ambiguity.
- a create returns `malformed_terminal_reply` even though `tmux list-panes` shows the new PTY:
  tmux 3.4 renders a control-character field separator as the printable octal escape `\037`.
  Install a release that accepts tmux's pinned rendered format; do not weaken PID, TTY, or procfs
  identity validation and do not treat the existence of a pane alone as a complete receipt.
- resize reports success but the detached PTY remains at its old dimensions: tmux 3.4 ignores a
  pane-only resize for a single-pane detached window. Use a release that resolves the pane's exact
  window identity, resizes that owned window, and reads the pane dimensions back before reporting
  success.
- a failed post-create readiness check removes the provider but reports an uncertain effect:
  tmux 3.4 can return exit zero and an empty `#{pane_id}` field when `display-message` addresses a
  pane just removed by `kill-pane`. Use a release that compares the returned canonical pane id and
  separately proves the pinned PID/start token disappeared. Do not use command status alone, and
  do not manually kill a different pane to make reconciliation look complete.
- daemon active but no hosted Sessions: inspect typed Relay authorization/readiness and the
  authoritative per-session channels; do not infer readiness from systemd alone.
- `clawdline-tmux` restart loop: compare the installed unit's `ExecStart` byte-for-byte with the
  foreground command above.
- package health rollback refusal: preserve the transition journal and durable authority. Never
  delete state to make the old image start.

Keep a receipt with instance id, region, source commit/tree, image digest, package/provenance
digests, service uid/gid, test counts, restart/reboot observations, hosted Session id, branch and
remote commit. Redact account tokens, invitation secrets, provider credentials, file contents, and
Session transcript text.
