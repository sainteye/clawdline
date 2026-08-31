/**
 * The hosted console bundle: the same bytes twice, and the right bytes once.
 *
 * `tools/build-web-app.py` produces what a person uploads to Cloudflare Pages by hand. There is
 * no CI behind it and no build log kept, so the only way "is what I am about to deploy the
 * reviewed tree?" stays answerable is if the answer is a hash — which requires the build to be
 * a function of the tree and nothing else. A timestamp, a counter, or a directory walk that
 * returns files in filesystem order would each quietly break that, and none of them would break
 * anything else, which is exactly why this is checked rather than assumed.
 */
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, readdirSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, relative } from "node:path";

const root = mkdtempSync(join(tmpdir(), "clawdline-web-app-"));
const first = join(root, "one");
const second = join(root, "two");

function build(out, extra) {
    return spawnSync("python3", ["tools/build-web-app.py", "--out", out].concat(extra || []),
        { encoding: "utf8" });
}

function walk(directory) {
    const found = [];
    (function descend(current) {
        for (const entry of readdirSync(current).sort()) {
            const path = join(current, entry);
            if (statSync(path).isDirectory()) descend(path);
            else found.push(relative(directory, path));
        }
    })(directory);
    return found;
}

try {
    const one = build(first);
    assert.equal(one.status, 0, "the first build succeeds: " + (one.stderr || one.stdout));
    const two = build(second);
    assert.equal(two.status, 0, "and so does the second: " + (two.stderr || two.stdout));

    const files = walk(first);
    assert.deepEqual(files, walk(second), "both builds contain the same files");
    for (const name of files) {
        assert.deepEqual(readFileSync(join(first, name)), readFileSync(join(second, name)),
            name + " is byte-identical between two builds of the same tree");
    }
    assert.equal(one.stdout, two.stdout,
        "and the build says the same thing about itself both times");

    /* ---- what has to be in it -------------------------------------------- */

    const index = readFileSync(join(first, "index.html"), "utf8");
    assert.ok(!index.includes("clawdline:cloud"),
        "the cloud slot is filled, not shipped as a comment");
    assert.ok(!index.includes("clawdline:modules"), "and so is the module preload slot");
    assert.ok(!index.includes("apple-touch-startup-image"),
        "the splash links are dropped — the Mac draws those on demand and Pages cannot");

    const declaration = /window\.__clawdlineCloud = (\{.*?\});/.exec(index);
    assert.ok(declaration, "the page declares which Clawdline it belongs to");
    const config = JSON.parse(declaration[1]);
    assert.equal(config.v, 1);
    assert.equal(config.app_origin, "https://app.clawdline.com");
    assert.equal(config.api_origin, "https://api.clawdline.com");
    assert.equal(config.relay_url, "wss://relay.clawdline.com/v1/connect");

    const stamp = config.build;
    assert.match(stamp, /^b[0-9a-f]{24}$/, "the asset stamp is a content hash, not a clock");
    assert.ok(index.includes('src="/app/' + stamp + '/js/main.js"'),
        "the entry module is served from under that stamp");
    assert.ok(index.includes('href="/app/' + stamp + '/css/tokens.css"'),
        "and so is every stylesheet");
    assert.ok(!/["']\/app\/(?!b[0-9a-f]{24}\/)/.test(index),
        "nothing still points at the unstamped path");

    const preloads = index.match(/rel="modulepreload"/g) || [];
    assert.ok(preloads.length > 30,
        "every module but the entry point is preloaded, not just a few: " + preloads.length);
    assert.ok(index.includes('href="/app/' + stamp + '/js/net/cloud-boot.js"'),
        "including the one that decides this is a cloud console");
    assert.ok(files.includes("app/" + stamp + "/js/vendor/qr-scanner.min.js"),
        "the PWA ships its QR decoder instead of sending camera frames to a service");
    assert.ok(files.includes("app/" + stamp + "/js/vendor/qr-scanner-worker.min.js"),
        "and its offline decoder worker is in the same immutable bundle");
    assert.ok(files.includes("app/" + stamp + "/js/vendor/qr-scanner.LICENSE.txt"),
        "the distributed third-party decoder carries its license");

    /* ---- the headers the stamp earns, and the policy it allows ----------- */

    const headers = readFileSync(join(first, "_headers"), "utf8");
    assert.ok(headers.includes("/app/*\n  Cache-Control: public, max-age=31536000, immutable"),
        "stamped assets are immutable");
    assert.ok(headers.includes("/index.html\n  Cache-Control: no-store"),
        "and the document that names them never is");
    const policy = /Content-Security-Policy: (.*)/.exec(headers)[1];
    assert.ok(policy.includes("default-src 'none'"), "the policy starts closed");
    assert.ok(!policy.includes("'unsafe-inline'"),
        "inline scripts are allowed by hash, never by blanket permission");
    assert.equal((policy.match(/'sha256-/g) || []).length,
        (index.match(/<script>/g) || []).length,
        "one hash per inline script actually emitted");
    assert.ok(policy.includes("connect-src 'self' https://api.clawdline.com wss://relay.clawdline.com"),
        "the page may reach its own API and relay and nothing else");
    assert.ok(policy.includes("frame-ancestors 'none'"), "and cannot be framed");

    /* ---- the manifest names icons that are actually there ---------------- */

    const manifest = JSON.parse(readFileSync(join(first, "manifest.webmanifest"), "utf8"));
    assert.equal(manifest.id, "/", "reinstalling names the same PWA identity");
    assert.ok(manifest.icons.length > 0, "the manifest ships icons");
    for (const icon of manifest.icons) {
        const name = icon.src.replace(/^\//, "");
        assert.ok(files.includes(name), icon.src + " is in the bundle rather than a 404");
        const png = readFileSync(join(first, name));
        assert.deepEqual(png.subarray(0, 8), Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
            icon.src + " is a real PNG copied out of the app icon");
    }

    /* ---- the receipt a deploy is checked against ------------------------- */

    const sums = readFileSync(join(first, "SHA256SUMS"), "utf8").trim().split("\n");
    assert.equal(sums.length, files.length - 1,
        "every file but the sums file itself is listed");
    for (const line of sums) {
        assert.match(line, /^[0-9a-f]{64} {2}\S/, "each line is a hash and a path: " + line);
    }

    const record = JSON.parse(readFileSync(join(first, "BUILD.json"), "utf8"));
    assert.equal(record.stamp, stamp, "the operator record names the same stamp");
    assert.equal(record.startup_images_removed, 20,
        "and says how many splash links it took out");
    assert.ok(!("built_at" in record), "nothing in the record is a clock");

    /* ---- a build for another origin is a different build ----------------- */

    const staging = join(root, "three");
    const other = build(staging, ["--app-origin", "https://staging.clawdline.com"]);
    assert.equal(other.status, 0, "a build for another origin succeeds");
    const otherIndex = readFileSync(join(staging, "index.html"), "utf8");
    const otherStamp = /window\.__clawdlineCloud = (\{.*?\});/.exec(otherIndex)[1];
    assert.notEqual(JSON.parse(otherStamp).build, stamp,
        "and is stamped differently, so the two cannot be confused in a cache");

    const refused = build(join(root, "four"), ["--app-origin", "http://app.clawdline.com"]);
    assert.notEqual(refused.status, 0, "a plaintext app origin is refused");

    console.log("web app build tests passed");
} finally {
    rmSync(root, { recursive: true, force: true });
}
