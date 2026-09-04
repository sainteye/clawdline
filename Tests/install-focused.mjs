#!/usr/bin/env node
// `install.sh` is the path the website and both READMEs recommend, and it runs on a Mac this
// project has never seen. Two things in it were load-bearing and unexercised.
//
// **The one that ended the install.** Step two read GitHub's JSON with `/usr/bin/python3`, which is
// an xcselect shim — `otool -L` on it names `libxcselect.dylib` — so on a Mac without the Command
// Line Tools it opens the "install the command line developer tools" dialog and exits non-zero.
// The script runs under `set -euo pipefail`, so the install ended there, on its second step,
// **without printing a word about why**. Every other failure path in that file says something.
//
// **The one that claimed something it had not checked.** The legacy branch — v0.6.0 and earlier,
// which are ad-hoc signed — did exactly one thing to a bundle fresh off the network: strip the
// quarantine attribute, which is the last guard macOS had on it. No checksum, no signature check.
//
// **Nothing here runs the real `install.sh` against the real machine.** It would download a
// release, replace the app in `/Applications` and launch it. Every command the script reaches out
// with — `curl`, `ditto`, `codesign`, `spctl`, `xattr`, `pkill`, the opener — is answered by a
// stand-in on `PATH` in a scratch directory, the way `Tests/keychain-rebuild-focused.mjs` answers
// `security` and `codesign`, and `DEST` is a temporary directory. The two blocks that carry the
// repairs are also marked in the source so they can be lifted out and driven on their own.

import { chmodSync, mkdtempSync, mkdirSync, readFileSync, writeFileSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const installSource = resolve(process.env.CLAWDLINE_INSTALL_SOURCE || "install.sh");
const install = readFileSync(installSource, "utf8");

let checks = 0;
let failures = 0;
const check = (what, ok) => {
    checks += 1;
    console.log(`  ${ok ? "✓" : "✗"} ${what}`);
    if (!ok) failures += 1;
};
// A guard that cannot find what it guards has to say so and stop, not report a clean scan of
// nothing. The count on the way out is the other half of that.
const stop = (why) => {
    console.log(`  ✗ ${why}`);
    console.log(`install.sh: stopped after ${checks + 1} checks — ${why}`);
    process.exit(1);
};

// The marked blocks, lifted rather than retyped: a copy of the construct would go on passing after
// the original changed, which is the failure this file exists to catch.
const marked = (label) => {
    const begin = `# BEGIN install-focused: ${label}`;
    const end = `# END install-focused: ${label}`;
    const first = install.indexOf(begin);
    const last = install.indexOf(end);
    if (first < 0 || last < first) {
        stop(`install.sh has no "${label}" block to drive; if it was renamed, update this file rather than deleting the checks`);
    }
    return install.slice(install.indexOf("\n", first) + 1, last);
};

const work = mkdtempSync(join(process.env.TMPDIR || tmpdir(), "clawdline-install-focused-"));
const executable = (path, body) => { writeFileSync(path, body, "utf8"); chmodSync(path, 0o755); };
const shell = (name, body) => {
    const p = join(work, name);
    executable(p, `#!/bin/bash\nset -euo pipefail\n${body}\n`);
    return p;
};
const run = (file, args = [], env = {}) => {
    const r = spawnSync("/bin/bash", [file, ...args], {
        encoding: "utf8", cwd: work, env: { ...process.env, ...env },
    });
    return { code: r.status, out: r.stdout ?? "", err: r.stderr ?? "", all: (r.stdout ?? "") + (r.stderr ?? "") };
};
const read = (p) => (existsSync(p) ? readFileSync(p, "utf8") : "");

const RELEASE = "https://github.com/sainteye/clawdline/releases/download/v0.6.0/Clawdline.zip";

try {
    // -----------------------------------------------------------------------------------------
    // 1. The reply reader, lifted out and driven against JSON this file writes.
    const urlBlock = marked("release url");
    check("the release-url block was found, so what follows is not driving an empty string",
          urlBlock.length > 200 && /browser_download_url/.test(urlBlock));
    const askUrl = shell("ask-url.sh", [
        'REPO="sainteye/clawdline"',
        'TMP="$1"',
        urlBlock,
        'echo "URL=$URL"',
    ].join("\n"));
    const reply = (name, body) => {
        const dir = join(work, `reply-${name}`);
        mkdirSync(dir, { recursive: true });
        writeFileSync(join(dir, "release.json"), body);
        return run(askUrl, [dir]);
    };
    // GitHub's own shape, pretty-printed the way the API sends it.
    const ordinary = reply("ordinary", JSON.stringify({
        tag_name: "v0.6.0",
        assets: [{ name: "Clawdline.zip", browser_download_url: RELEASE, size: 1 }],
    }, null, 2));
    check("an ordinary reply gives the download url", ordinary.code === 0 && ordinary.all.includes(`URL=${RELEASE}`));

    // **Field order is not promised, so nothing here counts fields.** The key is looked up by name.
    const reordered = reply("reordered", JSON.stringify({
        assets: [{ browser_download_url: RELEASE, url: "https://api.github.com/x", name: "Clawdline.zip" }],
        tag_name: "v0.6.0",
    }, null, 2));
    check("and so does one whose fields arrive in a different order",
          reordered.code === 0 && reordered.all.includes(`URL=${RELEASE}`));
    // **A stronger reading than "the fields moved."** Moving fields around is survived by a reader
    // that simply takes the last quoted string on the line, which is not what is wanted here — so
    // another key on the same line carries a full https `.zip` URL of its own. A reader that takes
    // the first `.zip`-looking string, or a position, answers with the decoy; one that looks the
    // key up by name does not. Measured: without this case a `-F'"'` positional reader passed the
    // reordering check above.
    const decoy = "https://github.com/sainteye/clawdline/releases/download/v0.5.0/Clawdline-old.zip";
    const decoyed = reply("decoy", JSON.stringify({
        assets: [{ label: decoy, name: "Clawdline.zip", browser_download_url: RELEASE }],
    }));
    check("and a decoy .zip url under another key on the same line is not mistaken for the download",
          decoyed.code === 0 && decoyed.all.includes(`URL=${RELEASE}`) && !decoyed.all.includes(decoy));

    // Compact JSON puts every asset on one line, which is the case a line-oriented reader gets
    // wrong by taking the whole line or by finding the last match instead of the first.
    const compact = reply("compact", JSON.stringify({
        assets: [
            { name: "Clawdline.zip", browser_download_url: RELEASE },
            { name: "Clawdline-debug.zip", browser_download_url: RELEASE.replace("Clawdline.zip", "Clawdline-debug.zip") },
        ],
    }));
    check("a compact reply with two zips on one line gives the first, as the old parse did",
          compact.code === 0 && compact.all.includes(`URL=${RELEASE}\n`));

    // **An asset name may contain whitespace.** Inside a URL it arrives percent-encoded, which is
    // exactly why the key here is the url and not the name: the value cannot contain a space, a
    // quote or a newline, so it survives being read into a shell variable whole.
    const spaced = `https://github.com/sainteye/clawdline/releases/download/v0.6.0/Clawdline%20beta.zip`;
    const spacedReply = reply("spaced", JSON.stringify({
        assets: [{ name: "Clawdline beta.zip", browser_download_url: spaced }],
    }, null, 2));
    check("an asset whose name has a space in it comes back whole and unsplit",
          spacedReply.code === 0 && spacedReply.all.includes(`URL=${spaced}`));

    // JSON permits an escaped solidus. GitHub does not currently send one; a reader that assumed
    // so would fail on the day it did.
    const escaped = reply("escaped", `{"assets":[{"name":"Clawdline.zip","browser_download_url":"${RELEASE.replace(/\//g, "\\/")}"}]}`);
    check("and an escaped solidus is unescaped rather than refused",
          escaped.code === 0 && escaped.all.includes(`URL=${RELEASE}`));

    // -----------------------------------------------------------------------------------------
    // 2. **The failure that used to be silent.** A reply this script cannot read now says so and
    //    exits; it used to end the whole install on `set -e` with nothing on the screen.
    const noZip = reply("nozip", JSON.stringify({
        assets: [{ name: "Clawdline.dmg", browser_download_url: RELEASE.replace(".zip", ".dmg") }],
    }, null, 2));
    check("a release with no .zip attached says so in a sentence and exits, rather than going quiet",
          noZip.code === 1 && /could not find a \.zip download/.test(noZip.all)
            && /releases\/latest/.test(noZip.all) && !/URL=/.test(noZip.all));
    const notJson = reply("notjson", "<html><body>502 Bad Gateway</body></html>\n");
    check("and so does a reply that is not the JSON this script expected at all",
          notJson.code === 1 && /could not find a \.zip download/.test(notJson.all));
    const emptyAssets = reply("empty", JSON.stringify({ tag_name: "v0.6.0", assets: [] }, null, 2));
    check("and so does a release with no assets on it",
          emptyAssets.code === 1 && /could not find a \.zip download/.test(emptyAssets.all));
    // The safety net, which is the one that matters if the reader is ever wrong in a way nobody
    // thought of: whatever comes out is refused unless it is an https .zip.
    const plainHttp = reply("http", JSON.stringify({
        assets: [{ name: "Clawdline.zip", browser_download_url: RELEASE.replace("https:", "http:") }],
    }, null, 2));
    check("a download url that is not https is refused rather than handed to curl",
          plainHttp.code === 1 && /could not find a \.zip download/.test(plainHttp.all));

    // -----------------------------------------------------------------------------------------
    // 3. **The dependency itself, and a control that shows what it cost.**
    //
    //    `/usr/bin/python3` is an absolute path and cannot be shimmed, so this is two readings
    //    together rather than one: the block runs to completion with a poisoned `python3` on
    //    `PATH` that records every call, and it never calls it; and the source no longer names the
    //    interpreter at all. Neither is worth much alone.
    const poison = join(work, "poison");
    mkdirSync(poison, { recursive: true });
    const pythonCalls = join(work, "python-calls");
    executable(join(poison, "python3"), [
        "#!/bin/bash",
        `echo "called $*" >> "${pythonCalls}"`,
        // What `/usr/bin/python3` does on a Mac with no Command Line Tools, which is the machine
        // this whole repair is about.
        'echo "xcode-select: note: No developer tools were found, requesting install." >&2',
        "exit 1",
    ].join("\n"));
    const clean = reply("clean", JSON.stringify({ assets: [{ name: "Clawdline.zip", browser_download_url: RELEASE }] }, null, 2));
    const poisoned = run(askUrl, [join(work, "reply-clean")], { PATH: `${poison}${delimiter}${process.env.PATH}` });
    check("the reader works on a Mac where python3 answers the way an unconfigured one does",
          clean.code === 0 && poisoned.code === 0 && poisoned.all.includes(`URL=${RELEASE}`));
    check("and it did not call python3 at all, rather than calling it and coping",
          !existsSync(pythonCalls));
    check("and install.sh no longer names the interpreter anywhere it could run it",
          !/^[^#\n]*python3/m.test(install));

    // **The control, so the repair is not asserted against nothing.** The old shape is reproduced
    // with a stand-in interpreter — `set -e`, a command substitution, an interpreter that exits
    // non-zero — because the real `/usr/bin/python3` on *this* Mac has the tools installed and
    // succeeds. What the user saw is what this prints: nothing.
    // Its own copy of the stand-in, writing its own log: the control *does* call the interpreter,
    // and sharing one log file with the reading above would leave a call recorded against
    // `install.sh` that `install.sh` never made.
    const controlPython = join(work, "control-python3");
    executable(controlPython, [
        "#!/bin/bash",
        'echo "xcode-select: note: No developer tools were found, requesting install." >&2',
        "exit 1",
    ].join("\n"));
    const oldShape = shell("old-shape.sh", [
        `URL=$("${controlPython}" -c 'print("x")' /dev/null)`,
        'echo "REACHED=yes"',
        'echo "URL=$URL"',
    ].join("\n"));
    const oldRun = run(oldShape);
    check("the shape it replaced ends the script on that step, with nothing said about why",
          oldRun.code !== 0 && !/REACHED=yes/.test(oldRun.out) && !/URL=/.test(oldRun.out)
            && !/could not/.test(oldRun.all));

    // -----------------------------------------------------------------------------------------
    // 4. The whole script, driven end to end against stand-ins. Nothing below touches the network,
    //    `/Applications`, or the app the person running these tests has open.
    const bin = join(work, "bin");
    mkdirSync(bin, { recursive: true });
    const log = join(work, "commands.log");
    executable(join(bin, "curl"), [
        "#!/bin/bash",
        `echo "curl $*" >> "$FAKE_LOG"`,
        'out=""',
        'while [ $# -gt 0 ]; do case "$1" in -o) out="$2"; shift 2 ;; *) shift ;; esac; done',
        'case "$out" in',
        '  *release.json) cat "$FAKE_RELEASE" > "$out"; printf %s "${FAKE_STATUS:-200}" ;;',
        '  *app.zip) printf "not really a zip" > "$out" ;;',
        'esac',
        'exit 0',
    ].join("\n"));
    executable(join(bin, "ditto"), [
        "#!/bin/bash",
        `echo "ditto $*" >> "$FAKE_LOG"`,
        // `ditto -x -k <zip> <dir>` unpacks; `ditto <src> <dst>` copies.
        'if [ "$1" = "-x" ]; then',
        '  mkdir -p "$4/Clawdline.app/Contents"',
        '  echo unpacked > "$4/Clawdline.app/Contents/marker"',
        'else',
        '  mkdir -p "$(dirname "$2")"; cp -R "$1" "$2"',
        'fi',
    ].join("\n"));
    executable(join(bin, "codesign"), [
        "#!/bin/bash",
        `echo "codesign $*" >> "$FAKE_LOG"`,
        'case "$1" in',
        '  --display) printf %s "$FAKE_SIGN_INFO"; exit 0 ;;',
        '  --verify) exit "${FAKE_VERIFY:-0}" ;;',
        'esac',
        'exit 0',
    ].join("\n"));
    for (const name of ["spctl", "xattr", "pkill"]) {
        executable(join(bin, name), `#!/bin/bash\necho "${name} $*" >> "$FAKE_LOG"\nexit 0\n`);
    }
    executable(join(bin, "fake-open"), `#!/bin/bash\necho "open $*" >> "$FAKE_LOG"\nexit 0\n`);

    const DEVELOPER_ID = [
        "Executable=/x/Clawdline.app/Contents/MacOS/Clawdline",
        "Authority=Developer ID Application: TsunamiWorks Co., Ltd. (83D62P566Q)",
        "Authority=Developer ID Certification Authority",
        "TeamIdentifier=83D62P566Q",
        "",
    ].join("\n");
    const AD_HOC = [
        "Executable=/x/Clawdline.app/Contents/MacOS/Clawdline",
        "Signature=adhoc",
        "TeamIdentifier=not set",
        "",
    ].join("\n");

    let installNumber = 0;
    const install_ = (signInfo, releaseBody, env = {}) => {
        installNumber += 1;
        const dest = join(work, `dest${installNumber}`);
        const commands = join(work, `commands${installNumber}.log`);
        const release = join(work, `release${installNumber}.json`);
        writeFileSync(release, releaseBody);
        const r = spawnSync("/bin/bash", [installSource, dest], {
            encoding: "utf8", cwd: work,
            env: {
                ...process.env,
                PATH: `${bin}${delimiter}${process.env.PATH}`,
                FAKE_LOG: commands,
                FAKE_RELEASE: release,
                FAKE_SIGN_INFO: signInfo,
                CLAWDLINE_OPEN_COMMAND: join(bin, "fake-open"),
                ...env,
            },
        });
        return {
            code: r.status, all: (r.stdout ?? "") + (r.stderr ?? ""),
            commands: read(commands), dest, app: join(dest, "Clawdline.app"),
        };
    };
    const goodRelease = JSON.stringify({
        tag_name: "v0.6.0",
        assets: [{ name: "Clawdline.zip", browser_download_url: RELEASE }],
    }, null, 2);

    // 4a. The notarized path, unchanged, as the control for everything below it.
    const signed = install_(DEVELOPER_ID, goodRelease);
    check("a Developer ID release installs and is checked with codesign and spctl",
          signed.code === 0 && existsSync(signed.app)
            && /codesign --verify/.test(signed.commands) && /spctl --assess/.test(signed.commands));
    check("and nothing strips quarantine on that path",
          !/^xattr /m.test(signed.commands));
    check("and the app it installed is the one it opened",
          new RegExp(`open ${signed.app}$`, "m").test(signed.commands));

    // 4b. **The legacy path, which used to strip quarantine off an unexamined binary.**
    const legacy = install_(AD_HOC, goodRelease);
    const verifyAt = legacy.commands.indexOf("codesign --verify");
    const xattrAt = legacy.commands.indexOf("xattr -dr");
    check("a legacy release is verified before anything is done to it",
          legacy.code === 0 && verifyAt >= 0 && xattrAt > verifyAt);
    check("and the quarantine attribute is still cleared once it has verified",
          /xattr -dr com\.apple\.quarantine/.test(legacy.commands) && existsSync(legacy.app));
    check("and the run says what that check does and does not prove",
          /integrity check and not a provenance one/.test(legacy.all)
            && /no Developer ID, no notarization/.test(legacy.all));
    check("and prints the archive's sha256, so a person can compare it with the release page",
          /sha256 of the archive installed: [0-9a-f]{64}/.test(legacy.all));

    // 4c. The same release with a seal that does not verify: this is the case the branch had no
    //     answer for. Nothing may be launched, nothing may be de-quarantined, and the copy that
    //     was unpacked has to go.
    const tampered = install_(AD_HOC, goodRelease, { FAKE_VERIFY: "1" });
    check("a legacy release whose ad-hoc seal does not verify is refused",
          tampered.code === 1 && /does not verify/.test(tampered.all));
    check("and its quarantine attribute is left exactly where macOS put it",
          !/xattr -dr/.test(tampered.commands));
    check("and the copy that was unpacked is removed rather than left where it can be run",
          !existsSync(tampered.app));
    check("and nothing was launched",
          !/^open /m.test(tampered.commands));

    // 4d. The end-to-end shape of the silent death, now that the whole script is being run: a
    //     reply with no zip in it reaches the person as a sentence.
    const noZipEnd = install_(DEVELOPER_ID, JSON.stringify({ tag_name: "v0.6.0", assets: [] }, null, 2));
    check("an install whose release has no .zip stops with a sentence, before downloading anything",
          noZipEnd.code === 1 && /could not find a \.zip download/.test(noZipEnd.all)
            && !/app\.zip/.test(noZipEnd.commands));

    // 4e. The rate-limit path, which already worked, run once so that repairing the step below it
    //     cannot have broken it in silence.
    const limited = install_(DEVELOPER_ID, `{"message":"API rate limit exceeded for 1.2.3.4."}`,
                             { FAKE_STATUS: "403" });
    check("a rate-limited address still gets told it is the limit and not them",
          limited.code === 1 && /hourly limit/.test(limited.all) && /build it yourself/.test(limited.all));

    // 4f. And the whole path once more with `python3` poisoned on `PATH`, because the reading that
    //     matters is not "the block works" but "the install works on that Mac".
    const withoutPython = install_(DEVELOPER_ID, goodRelease,
                                   { PATH: `${poison}${delimiter}${bin}${delimiter}${process.env.PATH}` });
    check("and the whole install runs on a Mac whose python3 opens the developer-tools dialog",
          withoutPython.code === 0 && existsSync(withoutPython.app) && !existsSync(pythonCalls));

    // -----------------------------------------------------------------------------------------
    // 5. The legacy block is marked too, so a future edit that quietly removes the verification
    //    fails here rather than passing as a smaller diff.
    const legacyBlock = marked("legacy integrity");
    check("the legacy block was found and still verifies before it clears anything",
          legacyBlock.indexOf("codesign --verify") >= 0
            && legacyBlock.indexOf("codesign --verify") < legacyBlock.indexOf("xattr -dr"));
} finally {
    rmSync(work, { recursive: true, force: true });
}

console.log(failures === 0
    ? `install.sh: all ${checks} checks passed`
    : `install.sh: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
