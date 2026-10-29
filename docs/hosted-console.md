# Deploying the hosted console

`app.clawdline.com` serves this repository's React console, built for Clawdline Cloud. It is the
same source as the console the daemon serves and **not the same build**, and that difference is
the whole of this page.

Written on 2026-09-20, after a deploy that was green at every step and broke the hosted console
for twenty-five minutes. There was no written procedure; the session that deployed derived the
commands from a note about the *Swift* console, and every check it ran answered "is my change in
this bundle" — none answered "is this bundle the hosted one".

## The discriminator

`web/console/src/main.tsx` branches on one build-time declaration:

```ts
const declared = import.meta.env.VITE_HOSTED_CONSOLE as string | undefined
if (declared) { /* CloudGate: reads a machine across the relay */ }
else          { /* DoorGate: this daemon's own console          */ }
```

**Without the variable the build succeeds and ships the daemon's console.** Vite does not warn:
nothing is missing, a different branch is taken. Deployed to `app.clawdline.com`, that console
asks its own origin for `/v1/…`, receives nothing, and shows "Waiting for the app" with an
`offline` badge — while the Mac is connected and publishing normally.

## Build

From a **disposable worktree**, never the shared checkout: packaging reads the whole tree, and
somebody else's uncommitted files ship with it.

```sh
hosted=/absolute/path/to/new/hosted-worktree
stamp=$(git rev-parse HEAD)
test ! -e "$hosted"
git worktree add -f "$hosted" "$stamp"
cd "$hosted/web"
test ! -e console/dist          # use a new worktree; never reuse an old build's BUILD.json
npm run build:cloud
```

`npm run build:cloud` owns the production declaration and ignores an inherited value for
`VITE_HOSTED_CONSOLE`. It requires a clean committed worktree, builds directly (frontend builds
need no native compilation slot), checks the compiled declaration, and writes `BUILD.json` from
that exact commit only after success. Keep the `hosted` path for the deploy callback below.
Do not hand-assemble the JSON or substitute a boolean such as `VITE_HOSTED_CONSOLE=1`.

The declaration remains necessary because the same console source serves both a local daemon
and Cloud. It specifies the app origin, API origin, relay address and protocol version; the browser
checks the app origin before connecting. This is deployment configuration, not a user preference.
The production command fixes the endpoints in `web/console/tools/hosted-declaration.mjs`. The runtime parser
is also used by Vite: a present malformed or incomplete declaration fails the build instead of
shipping an error page. An unset declaration continues to produce the local console.

On 2026-10-27 a deployment passed the old CloudGate text check with the declaration `1`; the
browser then reported an empty Cloud declaration. Production was restored to the previous Pages
deployment. The regression test first reproduced the missing build rejection, then proved the
same Vite configuration rejects that input. The served-bundle checker also rejects a bundle that
contains `CloudGate` but lacks the complete production declaration. Checks remain independent:
source stamp, served bundle, usable declaration, catalogs, then actual browser startup.

## PWA launch images

The selected launch screen is the existing warm-orange logo centred on `#0e0e11`,
without a wordmark or an artificial delay. Android derives its launch screen from
`manifest.webmanifest`; iOS uses the device-specific `apple-touch-startup-image`
links in `web/console/index.html`.

The connected mark uses the same compact silhouette as the Console. Large tiles and
launch marks have a subtle grid; tab icons remain solid. The `-brand` generator
also refreshes the native ICNS, browser favicon and PWA icons from `icon.AppIcon`.

The daemon draws launch PNGs through `icon.Splash`, but Cloudflare Pages cannot call
that renderer. The console therefore ships matching static PNGs in `public/`.
Without them, Pages returns its HTML SPA fallback at the image URL, even with HTTP
200, and iOS has no usable launch image.

After changing the renderer or the declared launch geometries, regenerate from the
repository root:

```sh
go run ./internal/domain/icon/cmd/splashgen -brand
go run ./internal/domain/icon/cmd/splashgen -brand -check
```

The generator's Go tests verify that every declared launch image matches the daemon
renderer's decoded pixels and dimensions, including detecting missing files and an
HTML fallback. PNG compression bytes can differ between Go versions, so regeneration
checks compare the image content rather than the encoded bytes.
After deployment, verify every declared URL returns `image/png`, the correct PNG
signature and dimensions, and the same bytes as the built static asset. A successful
HTTP status alone is insufficient. Device-level acceptance still requires launching
the installed PWA on iOS; a browser preview does not exercise the OS launch screen.

## Deploy

Cloudflare Pages, project `clawdline-app`, production branch `main`. The credentials belong to
the person and live in the private Cloud service checkout, not in this repository or git.
Run Wrangler from that checkout as `./relay/node_modules/.bin/wrangler`, through
`scripts/with-deploy-env.py`. Its ignored `.env.pages.local` must be a regular file owned by the
current user with mode 0600. Do not source the file or print its values.

**Deploy only a commit that descends from what production serves, and check it immediately
before the deploy, not when you started.** Several sessions land and deploy from the same `main`.
On 2026-09-26 a session read production as its merge's parent, spent a few minutes rebuilding the
app, and deployed; in that gap another session had deployed a newer `main` that already
contained the change, and the deploy put production back twelve commits. Check ancestry in the
product worktree, where both commits must be available, in the same shell as the deploy.

When it refuses and `<sha>` is already an ancestor of `$live`, production already carries the
change: there is nothing to deploy.

**Do not wait for the deploy in your turn.** Pages takes minutes to serve a new build. From the
product checkout, start one callback for the ancestor check, deploy, and wait. Supply the absolute
paths of the built product worktree and private Cloud service checkout. Read the callback
result when its notice arrives (`clawdline guide`, §5a):

```sh
hosted=/absolute/path/to/built/hosted-worktree
cloud=/absolute/path/to/private-cloud-checkout
stamp=$(git -C "$hosted" rev-parse HEAD)
clawdline callback --title "The hosted console serves $stamp" --timeout 20m -- \
  sh -c '
    set -eu
    hosted=$1
    cloud=$2
    stamp=$(git -C "$hosted" rev-parse HEAD)
    test "$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))[\"stamp\"])" \
      "$hosted/web/console/dist/BUILD.json")" = "$stamp"
    cd "$cloud"
    CLAWDLINE_DEPLOY_ENV_FILE="$PWD/.env.pages.local" \
      python3 scripts/with-deploy-env.py --check
    CLAWDLINE_DEPLOY_ENV_FILE="$PWD/.env.pages.local" \
      python3 scripts/with-deploy-env.py ./relay/node_modules/.bin/wrangler \
      pages deployment list --project-name=clawdline-app
    live=$(curl -fsS https://app.clawdline.com/BUILD.json |
      python3 -c "import json,sys; print(json.load(sys.stdin)[\"stamp\"])")
    git -C "$hosted" merge-base --is-ancestor "$live" "$stamp" || {
      echo "production is at $live, not behind $stamp: do not deploy" >&2
      exit 1
    }
    if [ "$live" = "$stamp" ]; then
      echo "production already serves $stamp; nothing to deploy"
      exit 0
    fi
    CLAWDLINE_DEPLOY_ENV_FILE="$PWD/.env.pages.local" \
      python3 scripts/with-deploy-env.py ./relay/node_modules/.bin/wrangler \
      pages deploy "$hosted/web/console/dist" \
      --project-name=clawdline-app --branch=main --commit-hash="$stamp"
    cd "$hosted"
    tools/wait-hosted-console.sh "$stamp"
  ' sh "$hosted" "$cloud"
```

Record the current production deployment id from the list before deploying, so rollback has an
exact target. A failed ancestry check stops the callback before deployment.

`tools/wait-hosted-console.sh <sha> [--deadline 15m] [--strict-catalogs]` polls `BUILD.json` until its stamp is
`<sha>` or descends from it. Then it runs the three steps below and exits 0 only when the served
bundle has `CloudGate` and its compiled declaration names the exact production app, API and
relay endpoints. It parses JavaScript syntax without executing downloaded code. Builds that ship product catalogs also read every advertised language's
hosted JSON through `tools/check-hosted-catalogs.py`, which runs the same catalog
validator used during Console development. The first multilingual release
requires all nine catalogs to cover the frozen initial baseline and English
and Taiwan Traditional Chinese to cover every current key. The ordinary
checker enforces this and reports later missing keys in the other seven
languages. Use `--strict-catalogs` for a translation catch-up that makes every
current key complete in all nine languages.

## The check that answers the right question

```sh
curl -fsS https://app.clawdline.com/BUILD.json                         # 1
bundle=$(curl -fsS https://app.clawdline.com/ | grep -o 'assets/main-[^"]*\.js' | head -n 1)
printf '%s\n' "$bundle"                                                   # 2
curl -fsS "https://app.clawdline.com/$bundle" | grep -c CloudGate     # 3 — must be > 0
```

These checks establish the source and branch, but `CloudGate` text alone does not establish a
usable configuration. `tools/wait-hosted-console.sh` additionally pipes the served bundle through
`web/console/tools/check-hosted-build.mjs`, which verifies the compiled production declaration.
A stamp can be stale, and a malformed hosted build can still contain the CloudGate import.

After the static checks, reload the production console in the existing browser tab and verify
that the actual Session list appears. Also verify the changed route. Preserve existing sign-in
and pairing; do not create a duplicate viewer tab. Do not report a deployment as accepted before
browser startup succeeds. If it fails, restore the recorded preceding deployment immediately.

## Verifying a preview deployment is not possible

The console refuses to run from an origin it was not built for, so `--branch=next` shows
"This console was built for https://app.clawdline.com …so it does not connect". Rebuilding with
the preview origin gets past that and then fails differently:
`api.clawdline.com` answers `access-control-allow-origin: https://app.clawdline.com` and nothing
else. A hosted console can only be verified in production, which is why check 3 exists and why
the rollback below is read before deploying, not after.

## Rollback

`pages deployment list --project-name=clawdline-app` names every deployment with its
source commit. Note the current production id **before** deploying. Wrangler 4 has no
`pages deployment rollback` (it answers "Unknown arguments"); the rollback is the Pages API's,
with the same credentials:

```sh
cd /absolute/path/to/private-cloud-checkout
CLAWDLINE_DEPLOY_ENV_FILE="$PWD/.env.pages.local" \
  python3 scripts/with-deploy-env.py sh -c '
    curl -fsS -X POST -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
      "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects/clawdline-app/deployments/<id>/rollback"
  '
```

It answers `"success": true` with the restored deployment's id; then run the three checks above.

Both consoles register a service worker with `skipWaiting` and `clients.claim`, so one reload
picks up whichever version is current. A phone stuck on a broken version needs a reload, not a
reinstall.

## When the phone shows nothing, which side is it

Ask the Mac before touching the console:

```sh
curl -s http://127.0.0.1:7727/v1/cloud/status -H "Authorization: Bearer $(cat ~/.config/clawdline-next/local-token)"
grep 'cloud ack' ~/.config/clawdline-next/logs/daemon.log | tail -20
```

`state: connected` with `published` climbing says the Mac is doing its part. `inbound_total: 0`
and `fanout=0` on every ack say no viewer is subscribed — the relay had nobody to hand those
publishes to. That pair separates "the Mac is down" from "the console cannot connect", and on
2026-09-20 it is what showed the Mac was innocent.

## Missing modules after a deployment

An already-open page can still name the preceding deployment's hashed modules.
If a newly opened lazy page requests a removed asset, Pages can answer with its
HTML SPA fallback and HTTP 200. The browser rejects that module even while the
index, API and new deployment's startup assets remain healthy. A live browser
test on 2026-10-27 reproduced this when opening Usage overview from an older
page: its missing JavaScript request returned `text/html`, and the uncaught
React lazy import removed the entire interface.

Registered pages now have individual module-load boundaries. A failed lazy page
shows a quiet localized recovery card while the shell and neighbouring Session stay
mounted. Its primary action reloads the current destination. Back to Sessions uses
the existing in-page navigation, keeping the Session mounted and avoiding a full
reload. The secondary action and its explanatory sentence are absent when startup
has failed before a Session shell exists. The card states that reloading is manual;
it does not claim that unsaved data is durable or that retrying must succeed.
Startup gate imports have the same recovery UI, including removing the initial
boot cover if the gate could not load. Vite preload events identify the
original rejected error; they are not cancelled, so consumers still receive the
real rejection. Ordinary API, cancellation and application errors keep their
existing error paths.

Recovery is manual, like the existing new-build notice: it never automatically
reloads a live editor or enters a refresh loop. The reload keeps the current URL
and uses the existing text-draft persistence at page-exit boundaries. Unsent
pictures belong only to the current editor and must be selected again after
reload; see [the storage decision](cloud-storage-resilience.md). A
page loaded before this protection was deployed needs one ordinary reload to
receive it. A missing initial entry script cannot be handled by JavaScript that
has not itself loaded.

Acceptance keeps an older protected page open while changing the served build,
then opens a previously unvisited lazy page. Verify the recovery action, that
the shell and draft remain available, and that using Reload restores the same
destination and draft. Also exercise a persistent module failure to prove that
no automatic reload loop occurs. A healthy API, a matching build stamp, or a
same-version redeploy alone does not verify this path.

## Startup session checks

Startup session checks distinguish authentication from transport failures. Only the session API's
`401 no_session` permits device registration after a failed check, and only `401 no_login_ticket` from registration
opens the GitHub sign-in card. Unrecognized 401/403 responses (including intermediary HTML),
408/429, server failures, malformed successful responses and network errors stay in the existing
bounded connection retry flow. They do not establish that the browser signed out. A successful
retry restores the existing device; a genuine session refusal still requires sign-in. A valid
session whose device key is no longer stored still registers a replacement device. The startup
failure matrix and recovery path are exercised in `device-limit.test.ts` and `keep-connected.test.ts`.

Control-plane JSON requests use `cache: "no-store"`, including session and capability checks.
The sign-in card retains the connection loop's page listeners: returning to the visible page,
restoring it from the back-forward cache, or regaining network rechecks authorization with the
existing minimum attempt spacing. A signed-out page does not poll. A fresh valid session can
restore the same device without OAuth navigation or replacement registration; a named refusal
keeps the card. Revocation and permanent protocol failures retain their terminal behavior.
If a return happens during the session request, its wake is consumed by a spaced fresh check
before showing a sign-in card, rather than being lost before the loop starts waiting.
These tests establish cache bypass and recovery behavior, not the HTTP response or cookie state
of any particular reported phone launch.

## Explicit send diagnostics

A failed message card's **Record diagnostics** button sends a content-free
`cloud.send.diagnostic_requested` row to the currently selected machine using
`diagnostics.events`. This is one explicit request per press, separate from the
background viewer-event log's daily budget and cross-tab writer lock. It contains
the failure code and browser connectivity state, never message text or pictures.
The button stays busy until the machine returns a receipt naming the same batch
and row count. Only that receipt produces **Diagnostics delivered to the machine**.
A timeout, unsupported machine, or mismatched receipt leaves a retry button; it
makes no promise of an offline queue or automatic redelivery. Background viewer
events keep their existing bounded outbox and automatic retry policy.

On the machine, inspect `cloud: viewer reported: event=cloud.send.diagnostic_requested`
and the batch's `cloud: viewer events delivered:` summary in the daemon log.
A relay acknowledgement alone does not establish that the machine logged the row.

## Community sharing metadata

The Cloud build emits `_worker.js` and a restricted `_routes.json` for `/community` and `/help`.
Assets, the workspace, API and relay traffic remain outside that worker. The worker obtains the
SPA asset through `ASSETS.fetch` and rewrites its head with the same metadata model used by the
client. Its only upstream is the fixed anonymous public API, with no credentials, redirect
following, query forwarding or persistent preview cache. Private/Admin routes never fetch content.
The default static Cloud entry has generic metadata; daemon builds retain their existing entry.

A production acceptance checks the raw GET HTML using an ordinary user agent and representative
share crawlers, not only the browser DOM. Verify distinct list titles, a real public topic title
and original-post excerpt, noindex for private/missing content, one canonical URL without tracking,
one set of OG/Twitter tags, a reachable image/png response with exact 1200×630 dimensions, and SPA
navigation updating both title and metadata. Also open the existing signed-in console and verify
that changing the HTML response has not affected startup, navigation or pairing. No API or relay
redeployment is required for this rendering layer.
