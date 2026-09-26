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
git worktree add -f <scratch>/hosted HEAD
cd <scratch>/hosted/web && npm install
rm -rf console/dist            # a stale BUILD.json survived a build once and lied about the stamp
VITE_HOSTED_CONSOLE='{"v":1,"app_origin":"https://app.clawdline.com","api_origin":"https://api.clawdline.com","relay_url":"wss://relay.clawdline.com/v1/connect"}' \
  npm run build
```

The three endpoints are the production ones in `internal/adapters/cloud/settings.go`. Write a
`BUILD.json` into `console/dist` carrying the commit sha as `stamp`, so a deploy can be identified
afterwards — but see below for what it does and does not prove.

## Deploy

Cloudflare Pages, project `clawdline-app`, production branch `main`. The credentials belong to
the person and live in the cloud service's own checkout, not in this repository and not in git;
the account that has them knows where. From that checkout:

```sh
( set -a; . ./.env.pages.local; set +a
  ./relay/node_modules/.bin/wrangler pages deploy <scratch>/hosted/web/console/dist \
    --project-name=clawdline-app --branch=main --commit-hash=<sha> )
```

**Deploy only a commit that descends from what production serves, and check it immediately
before the deploy, not when you started.** Several sessions land and deploy from the same `main`.
On 2026-09-26 a session read production as its merge's parent, spent a few minutes rebuilding the
app, and deployed; in that gap another session had deployed a newer `main` that already
contained the change, and the deploy put production back twelve commits. So, in the same shell
as the deploy:

```sh
live=$(curl -s https://app.clawdline.com/BUILD.json | python3 -c 'import json,sys; print(json.load(sys.stdin)["stamp"])')
git merge-base --is-ancestor "$live" <sha> || { echo "production is at $live, not behind <sha>: do not deploy"; exit 1; }
```

When it refuses and `<sha>` is already an ancestor of `$live`, production already carries the
change: there is nothing to deploy.

## The check that answers the right question

```sh
curl -s https://app.clawdline.com/BUILD.json                            # 1
curl -s https://app.clawdline.com/ | grep -o 'assets/main-[^"]*\.js'    # 2
curl -s https://app.clawdline.com/<that main.js> | grep -c CloudGate    # 3 — must be > 0
```

**Three is the one that matters.** One says a file changed, not that the right file went up — a
`BUILD.json` left behind by an earlier build reported the previous stamp after a deploy that
replaced everything else. Two only names the bundle. Three asks whether the bundle that is now
being served took the Cloud branch, which is the question the failure turns on.

Grepping the bundle for a string you added is not this check. It answers whether your change
shipped, which was true in the broken deploy as well.

## Verifying a preview deployment is not possible

The console refuses to run from an origin it was not built for, so `--branch=next` shows
"This console was built for https://app.clawdline.com …so it does not connect". Rebuilding with
the preview origin gets past that and then fails differently:
`api.clawdline.com` answers `access-control-allow-origin: https://app.clawdline.com` and nothing
else. A hosted console can only be verified in production, which is why check 3 exists and why
the rollback below is read before deploying, not after.

## Rollback

`wrangler pages deployment list --project-name=clawdline-app` names every deployment with its
source commit. Note the current production id **before** deploying. Wrangler 4 has no
`pages deployment rollback` (it answers "Unknown arguments"); the rollback is the Pages API's,
with the same credentials:

```sh
( set -a; . ./.env.pages.local; set +a       # same checkout as the deploy above
  curl -s -X POST -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
    "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects/clawdline-app/deployments/<id>/rollback" )
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
