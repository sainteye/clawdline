#!/bin/bash
# The software bill of materials of each release archive
# (docs/security/release-evidence.md).
#
# usage: tools/release/sbom.sh generate <release-dir>
#        tools/release/sbom.sh verify <release-dir>
#
# generate writes <archive>.spdx.json (SPDX 2.3) beside every archive that
# <release-dir>/manifest.json lists. verify fails unless every one of them is
# there and still describes its archive: the archive digest is the manifest's,
# the file list and every file's sha256 are what the archive holds, the Go
# modules linked into each Go binary are what `go version -m` reads from it,
# and the carried tmux is what its tmux.json names and the manifest commit's
# sources.sh pins.
#
# Two kinds of entry are kept apart, and each package says which it is:
#   artifact content  read from the unpacked archive itself (files, Go
#                     modules from the binaries' build information, tmux.json)
#   source inventory  read from the commit (web/package-lock.json, the tmux
#                     source pins): what the build was given, not proof that
#                     every listed package ended up in the minified bundle
#
# Both commands read web/package-lock.json and tools/release/tmux/sources.sh
# at the manifest's commit, never this checkout's copies, so they
# run in a checkout that has that commit. Node and Go are the only tools used.
set -euo pipefail
cd "$(dirname "$0")/../.."

[ "$#" = 2 ] || { echo "usage: tools/release/sbom.sh generate|verify <release-dir>" >&2; exit 2; }
mode=$1 dir=$2
case "$mode" in generate|verify) ;; *) echo "unknown mode $mode" >&2; exit 2 ;; esac
[ -f "$dir/manifest.json" ] || { echo "sbom: $dir has no manifest.json" >&2; exit 1; }

work=$(mktemp -d)
finished=0
trap 's=$?; rm -rf -- "$work"; if [ "$finished" != 1 ] && [ "$s" = 0 ]; then exit 1; fi' EXIT

commit=$(node -e 'process.stdout.write(JSON.parse(require("fs").readFileSync(process.argv[1])).commit||"")' "$dir/manifest.json")
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || { echo "sbom: manifest.json names no commit" >&2; exit 1; }
git cat-file -e "$commit^{commit}" 2>/dev/null || { echo "sbom: commit $commit is not in this checkout; fetch it first" >&2; exit 1; }
git show "$commit:web/package-lock.json" >"$work/package-lock.json"
# The tmux pins are the commit's too, not this checkout's: a release built
# before a pin moved still names the pins it was built with. They are read as
# NAME=value lines, not sourced, so the commit's file runs no code here.
git show "$commit:tools/release/tmux/sources.sh" >"$work/sources.sh" 2>/dev/null ||
  { echo "sbom: commit $commit has no tools/release/tmux/sources.sh" >&2; exit 1; }
for name in TMUX LIBEVENT UTF8PROC NCURSES; do
  for field in VERSION URL SHA256; do
    v=$(sed -n "s#^${name}_${field}=\([A-Za-z0-9._:/+-]*\)\$#\1#p" "$work/sources.sh" | tail -n 1)
    [ -n "$v" ] || { echo "sbom: commit $commit's sources.sh pins no ${name}_${field}" >&2; exit 1; }
    printf -v "${name}_${field}" '%s' "$v"
  done
done

TMUX_VERSION=$TMUX_VERSION TMUX_URL=$TMUX_URL TMUX_SHA256=$TMUX_SHA256 \
LIBEVENT_VERSION=$LIBEVENT_VERSION LIBEVENT_URL=$LIBEVENT_URL LIBEVENT_SHA256=$LIBEVENT_SHA256 \
UTF8PROC_VERSION=$UTF8PROC_VERSION UTF8PROC_URL=$UTF8PROC_URL UTF8PROC_SHA256=$UTF8PROC_SHA256 \
NCURSES_VERSION=$NCURSES_VERSION NCURSES_URL=$NCURSES_URL NCURSES_SHA256=$NCURSES_SHA256 \
  node - "$mode" "$dir" "$work" <<'JS'
const fs = require("fs"), path = require("path"), crypto = require("crypto"), cp = require("child_process");
const [mode, dir, work] = process.argv.slice(2);
const fail = (msg) => { console.error("sbom: " + msg); process.exit(1); };
const sha256 = (p) => crypto.createHash("sha256").update(fs.readFileSync(p)).digest("hex");
const manifest = JSON.parse(fs.readFileSync(path.join(dir, "manifest.json")));
const lock = JSON.parse(fs.readFileSync(path.join(work, "package-lock.json")));
const lockSHA = sha256(path.join(work, "package-lock.json"));
const env = process.env;
const pins = {
  tmux: { version: env.TMUX_VERSION, url: env.TMUX_URL, sha256: env.TMUX_SHA256, license: "ISC" },
  libevent: { version: env.LIBEVENT_VERSION, url: env.LIBEVENT_URL, sha256: env.LIBEVENT_SHA256, license: "BSD-3-Clause" },
  utf8proc: { version: env.UTF8PROC_VERSION, url: env.UTF8PROC_URL, sha256: env.UTF8PROC_SHA256, license: "MIT" },
  ncurses: { version: env.NCURSES_VERSION, url: env.NCURSES_URL, sha256: env.NCURSES_SHA256, license: "X11" },
};
// id makes an SPDX identifier; the hash suffix keeps two names that sanitize
// alike apart.
const id = (s) => "SPDXRef-" + s.replace(/[^A-Za-z0-9.-]/g, "-").slice(0, 60) + "-" + crypto.createHash("sha1").update(s).digest("hex").slice(0, 10);

// walk lists every regular file and symlink under root, sorted.
function walk(root, rel = "") {
  let out = [];
  for (const e of fs.readdirSync(path.join(root, rel), { withFileTypes: true }).sort((a, b) => a.name < b.name ? -1 : 1)) {
    const r = rel ? rel + "/" + e.name : e.name;
    if (e.isDirectory()) out = out.concat(walk(root, r));
    else out.push(r);
  }
  return out;
}
const magic = (p) => {
  const fd = fs.openSync(p, "r"), b = Buffer.alloc(4);
  fs.readSync(fd, b, 0, 4, 0); fs.closeSync(fd);
  const h = b.toString("hex");
  return h === "7f454c46" || ["cffaedfe", "cefaedfe", "cafebabe"].includes(h);
};
// goModules reads the build information Go writes into a binary; a binary
// without it is not a Go binary.
function goModules(p) {
  let out;
  try { out = cp.execFileSync("go", ["version", "-m", "-json", p], { stdio: ["ignore", "pipe", "ignore"] }); }
  catch { return null; }
  const bi = JSON.parse(out);
  const settings = Object.fromEntries((bi.Settings || []).map((s) => [s.Key, s.Value]));
  return {
    go: bi.GoVersion, main: bi.Main && bi.Main.Path || bi.Path, vcs: settings["vcs.revision"] || "",
    modified: settings["vcs.modified"] || "",
    deps: (bi.Deps || []).map((d) => { const m = d.Replace || d; return { path: m.Path, version: m.Version, sum: m.Sum || "" }; })
      .sort((a, b) => (a.path + a.version < b.path + b.version ? -1 : 1)),
  };
}

// inspect reads what one unpacked archive holds.
function inspect(root) {
  const files = walk(root).map((rel) => {
    const p = path.join(root, rel), st = fs.lstatSync(p);
    if (st.isSymbolicLink()) return { rel, sha256: crypto.createHash("sha256").update("symlink:" + fs.readlinkSync(p)).digest("hex"), link: true };
    return { rel, sha256: sha256(p), exec: magic(p) };
  });
  const binaries = files.filter((f) => f.exec).map((f) => ({ rel: f.rel, info: goModules(path.join(root, f.rel)) })).filter((b) => b.info);
  const tmux = files.filter((f) => /(^|\/)tmux\.json$/.test(f.rel)).map((f) => ({ rel: f.rel, json: JSON.parse(fs.readFileSync(path.join(root, f.rel))) }));
  const console = files.some((f) => /(^|\/)(dist|web)\/BUILD\.json$/.test(f.rel));
  return { files, binaries, tmux, console };
}

function unpack(name) {
  const d = path.join(work, "unpacked", name);
  fs.mkdirSync(d, { recursive: true });
  cp.execFileSync("tar", ["-xzf", path.join(dir, name), "-C", d]);
  return d;
}

function npmPackages() {
  const out = [];
  for (const [key, v] of Object.entries(lock.packages || {})) {
    if (!key.includes("node_modules/") || v.link) continue;
    const name = key.slice(key.lastIndexOf("node_modules/") + "node_modules/".length);
    out.push({ name, version: v.version, resolved: v.resolved || "NOASSERTION", integrity: v.integrity || "", build: !!(v.dev || v.devOptional) });
  }
  const seen = new Set();
  return out.filter((p) => { const k = p.name + "@" + p.version; if (seen.has(k)) return false; seen.add(k); return true; })
    .sort((a, b) => (a.name + a.version < b.name + b.version ? -1 : 1));
}

function generate(a) {
  const root = unpack(a.name), what = inspect(root);
  const doc = {
    spdxVersion: "SPDX-2.3", dataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT",
    name: a.name,
    documentNamespace: `https://github.com/sainteye/clawdline/releases/download/${manifest.version}/${a.name}.spdx.json#${a.sha256}`,
    comment: "Packages marked artifact-content were read from the unpacked archive; packages marked source-inventory were read from the commit and were not individually traced into the bundle.",
    creationInfo: { created: manifest.committed_at, creators: ["Tool: clawdline tools/release/sbom.sh"] },
    packages: [], files: [], relationships: [],
  };
  const pkgs = new Map();
  const add = (p) => { if (!pkgs.has(p.SPDXID)) pkgs.set(p.SPDXID, p); return p.SPDXID; };
  const rel = (a, t, b) => doc.relationships.push({ spdxElementId: a, relationshipType: t, relatedSpdxElement: b });
  const archiveID = "SPDXRef-Archive";
  add({ SPDXID: archiveID, name: a.name, versionInfo: manifest.version, downloadLocation: a.url, filesAnalyzed: true,
    checksums: [{ algorithm: "SHA256", checksumValue: a.sha256 }], primaryPackagePurpose: "ARCHIVE",
    sourceInfo: `built from https://github.com/sainteye/clawdline commit ${manifest.commit}`,
    packageVerificationCode: { packageVerificationCodeValue: crypto.createHash("sha1").update(what.files.map((f) => f.sha256).sort().join("")).digest("hex") },
    licenseConcluded: "NOASSERTION", licenseDeclared: "NOASSERTION", copyrightText: "NOASSERTION" });
  rel("SPDXRef-DOCUMENT", "DESCRIBES", archiveID);
  const fileID = (r) => id("File-" + r);
  for (const f of what.files) {
    doc.files.push({ SPDXID: fileID(f.rel), fileName: "./" + f.rel, checksums: [{ algorithm: "SHA256", checksumValue: f.sha256 }],
      licenseConcluded: "NOASSERTION", copyrightText: "NOASSERTION", ...(f.link ? { comment: "symbolic link; the digest is of its target text" } : {}) });
    rel(archiveID, "CONTAINS", fileID(f.rel));
  }
  for (const b of what.binaries) {
    const m = b.info;
    const mainID = add({ SPDXID: id("Go-main-" + b.rel), name: m.main, versionInfo: m.vcs || "NOASSERTION", downloadLocation: "https://github.com/sainteye/clawdline",
      filesAnalyzed: false, summary: "artifact-content", comment: `Go main module of ./${b.rel}, ${m.go}, vcs.modified=${m.modified || "unknown"}`,
      licenseConcluded: "NOASSERTION", licenseDeclared: "NOASSERTION", copyrightText: "NOASSERTION" });
    rel(fileID(b.rel), "GENERATED_FROM", mainID);
    for (const d of m.deps) {
      const did = add({ SPDXID: id(`Go-${d.path}@${d.version}`), name: d.path, versionInfo: d.version, downloadLocation: "NOASSERTION",
        filesAnalyzed: false, summary: "artifact-content", comment: `Go module linked into the binary; go.sum ${d.sum || "none"}`,
        externalRefs: [{ referenceCategory: "PACKAGE-MANAGER", referenceType: "purl", referenceLocator: `pkg:golang/${d.path}@${d.version}` }],
        licenseConcluded: "NOASSERTION", licenseDeclared: "NOASSERTION", copyrightText: "NOASSERTION" });
      rel(fileID(b.rel), "STATIC_LINK", did);
    }
  }
  for (const t of what.tmux) {
    const tid = add({ SPDXID: id("Native-tmux-" + t.rel), name: "tmux", versionInfo: t.json.tmux.version, downloadLocation: pins.tmux.url,
      filesAnalyzed: false, summary: "artifact-content", checksums: [{ algorithm: "SHA256", checksumValue: t.json.tmux.sha256 }],
      comment: `carried tmux described by ./${t.rel} (${t.json.goos}/${t.json.goarch}); checksum is of the source tarball; binary before re-signing ${t.json.binary_sha256}`,
      licenseConcluded: "NOASSERTION", licenseDeclared: pins.tmux.license, copyrightText: "NOASSERTION" });
    rel(fileID(t.rel), "DESCRIBES", tid);
    for (const lib of ["libevent", "utf8proc", "ncurses"]) {
      if (!t.json[lib]) continue;
      const lid = add({ SPDXID: id(`Native-${lib}-${t.rel}`), name: lib, versionInfo: t.json[lib].version, downloadLocation: pins[lib].url,
        filesAnalyzed: false, summary: "artifact-content", checksums: [{ algorithm: "SHA256", checksumValue: t.json[lib].sha256 }],
        comment: "statically linked into the carried tmux; checksum is of the source tarball",
        licenseConcluded: "NOASSERTION", licenseDeclared: pins[lib].license, copyrightText: "NOASSERTION" });
      rel(tid, "STATIC_LINK", lid);
    }
    if (t.json.image) {
      const iid = add({ SPDXID: id("Native-musl-" + t.rel), name: "musl (Alpine build image)", versionInfo: t.json.image, downloadLocation: "NOASSERTION",
        filesAnalyzed: false, summary: "artifact-content", comment: "the C library the static Linux tmux carries, from the pinned Alpine image digest",
        licenseConcluded: "NOASSERTION", licenseDeclared: "MIT", copyrightText: "NOASSERTION" });
      rel(tid, "STATIC_LINK", iid);
    } else {
      doc.relationships.push({ spdxElementId: tid, relationshipType: "DEPENDS_ON", relatedSpdxElement: "NONE",
        comment: "darwin tmux links the system libncurses and libSystem dynamically; they are not in the archive" });
    }
  }
  if (what.console) {
    const cid = add({ SPDXID: "SPDXRef-Console", name: "@clawdline/console", versionInfo: manifest.commit, downloadLocation: "https://github.com/sainteye/clawdline",
      filesAnalyzed: false, summary: "source-inventory", checksums: [{ algorithm: "SHA256", checksumValue: lockSHA }],
      comment: "the local console bundle; checksum is of web/package-lock.json at the commit. Runtime packages are the lockfile's non-dev entries, build packages its dev entries; neither list was traced module by module into the minified bundle.",
      licenseConcluded: "NOASSERTION", licenseDeclared: "NOASSERTION", copyrightText: "NOASSERTION" });
    rel(archiveID, "CONTAINS", cid);
    for (const p of npmPackages()) {
      const pid = add({ SPDXID: id(`npm-${p.name}@${p.version}`), name: p.name, versionInfo: p.version, downloadLocation: p.resolved,
        filesAnalyzed: false, summary: "source-inventory",
        ...(p.integrity.startsWith("sha512-") ? { checksums: [{ algorithm: "SHA512", checksumValue: Buffer.from(p.integrity.slice(7), "base64").toString("hex") }] } : {}),
        externalRefs: [{ referenceCategory: "PACKAGE-MANAGER", referenceType: "purl", referenceLocator: `pkg:npm/${p.name.replace("@", "%40")}@${p.version}` }],
        licenseConcluded: "NOASSERTION", licenseDeclared: "NOASSERTION", copyrightText: "NOASSERTION" });
      if (p.build) rel(pid, "BUILD_DEPENDENCY_OF", cid); else rel(cid, "DEPENDS_ON", pid);
    }
  }
  doc.packages = [...pkgs.values()];
  return doc;
}

const canon = (o) => JSON.stringify(o);
let failures = 0;
for (const a of manifest.artifacts || []) {
  const archive = path.join(dir, a.name), out = archive + ".spdx.json";
  if (!fs.existsSync(archive)) fail(`${a.name} is in the manifest but not in ${dir}`);
  const got = sha256(archive);
  if (got !== a.sha256) fail(`${a.name} is ${got}, not the manifest's ${a.sha256}`);
  if (mode === "generate") {
    fs.writeFileSync(out, JSON.stringify(generate(a), null, 1) + "\n");
    console.log(`sbom: wrote ${path.basename(out)}`);
    continue;
  }
  if (!fs.existsSync(out)) { console.error(`sbom: ${a.name} has no ${path.basename(out)}`); failures++; continue; }
  let have;
  try { have = JSON.parse(fs.readFileSync(out)); } catch (e) { console.error(`sbom: ${path.basename(out)} is not JSON`); failures++; continue; }
  // The SBOM is regenerated from the archive and the commit and must match
  // exactly: anything it claims that the archive does not hold, or anything
  // the archive holds that it omits, is a finding.
  const want = generate(a);
  if (canon(have) !== canon(want)) {
    const hp = new Set((have.files || []).map((f) => f.fileName + " " + (f.checksums || [{}])[0].checksumValue));
    const wp = new Set(want.files.map((f) => f.fileName + " " + f.checksums[0].checksumValue));
    const diff = [...wp].filter((x) => !hp.has(x)).concat([...hp].filter((x) => !wp.has(x))).slice(0, 5);
    const hk = new Set((have.packages || []).map((p) => p.SPDXID + "@" + p.versionInfo));
    const pk = want.packages.map((p) => p.SPDXID + "@" + p.versionInfo).filter((x) => !hk.has(x)).slice(0, 5);
    console.error(`sbom: ${path.basename(out)} does not describe ${a.name}` +
      (diff.length ? `; files differ: ${diff.join(", ")}` : "") + (pk.length ? `; packages missing: ${pk.join(", ")}` : ""));
    failures++;
    continue;
  }
  console.log(`sbom: ${path.basename(out)} describes ${a.name} (${want.files.length} files, ${want.packages.length} packages)`);
}
if (!(manifest.artifacts || []).length) fail("manifest.json lists no artifacts");
if (mode === "verify" && failures) process.exit(1);
JS
if [ "$mode" = verify ]; then
  # Every carried tmux.json names the commit's pins; build.sh enforced
  # this at build time and the gate enforces it again on what was downloaded.
  for a in "$dir"/*.tar.gz; do
    d="$work/pins/$(basename "$a")"
    mkdir -p "$d"
    tar -xzf "$a" -C "$d"
    while IFS= read -r t; do
      grep -q "\"version\": \"$TMUX_VERSION\", \"sha256\": \"$TMUX_SHA256\"" "$t" ||
        { echo "sbom: ${t#"$d"/} in $(basename "$a") does not name the pinned tmux $TMUX_VERSION" >&2; exit 1; }
    done < <(find "$d" -name tmux.json -type f)
  done
  echo "sbom: every archive has an SBOM that matches it"
fi
finished=1
