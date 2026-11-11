# Browser invitations

A browser invitation admits one browser that has **no Clawdline account** to **one machine**, with
read and operate permission, for 8 hours, 7 days, or until revoked. It exists for a browser the
owner uses but does not trust with their account: a cloud computer reached over remote desktop,
where typing works but copying text out does not.

The owner starts it on a device already paired with the machine: Cloud's **Devices** page → the
machine → **Name and access** (`web/console/src/pages/devices/BrowserInvites.tsx`). The machine's
own local routes below do the same for a script on the machine; the Local console has no screen
for it yet. The device shows a code such as
`K7M4Q-9X2FTB`. The owner opens `app.clawdline.com/join` in the new browser and types it. Both
screens then show the same six digits; the owner compares them and approves on the inviting device.

## What protects it

| Threat | Defence |
| --- | --- |
| Cloud, or anyone on the relay path, substitutes keys | The code is `id-secret`. Cloud assigns the five-character id and never sees the six-character secret (Crockford base32, 30 bits). The secret is the password of a CPace exchange (`internal/domain/cloud/cpace.go`, draft-irtf-cfrg-cpace-21, ristretto255 + SHA-512). A substituted message gives one online guess, then the invitation is spent. |
| Someone who saw the code types it first | One browser per invitation; a wrong secret spends it. The six digits are derived from the CPace key, so a browser that raced the owner shows different digits from the owner's own browser, and the owner does not approve. |
| Guessing codes | Codes live ten minutes and are single-use. Cloud rate-limits `join` per address and per id. A machine keeps at most three live invitations (`cloud.browser_invites_live`). |
| A hijacked account adds a browser silently | Only the machine creates an invitation (its own token, or the encrypted `browser-invite-start` word from an operate device), and only the machine admits. Cloud cannot create an admitted browser on its own: the machine's pin is what admits. |
| An invited browser escalates | It may not use any `local_exemption` word (`internal/app/cloudops/machine_only.go`): it cannot invite more browsers, carry a pairing to another machine, or change this machine's network. Cloud binds its identity to the one machine; the relay refuses its other channels. |
| Expiry or revocation does not stick | The machine's pin carries `expires_at`. An expired or revoked pin is **refused**, not absent, so the account roster cannot re-admit it; a roster row of kind `invited_browser` never admits without a live pin (`internal/transport/cloud/link.go`). Revocation goes through the ordinary path, which rotates the content key when the key runtime is on. |
| The browser's own host is untrusted | Not solvable by protocol. Limits: one machine, no account pages, chosen expiry, one-press revoke, non-extractable WebCrypto keys. |

Known limit: an invitation that **expires** (rather than being revoked) does not rotate the content
key; the browser keeps a key it can no longer use through Cloud. Revoke instead when the host may
retain data.

## Protocol

M is the machine, P an inviting device, B the invited browser, C Cloud. Initiator is B.

1. **Start.** M draws `secret`, calls `POST /v1/pairing/browser-invites {expiry}` and gets
   `{invite_id, expires_at}` (milliseconds, ten minutes). M shows `invite_id-secret`.
2. **Join.** B parses the code (`ParseInviteCode`: case, spaces and dashes ignored; O→0, I/L→1),
   generates a non-extractable Ed25519 device key, computes CPace `Ya` with
   `PRS = secret`, `CI = lv_cat("clawdline browser invite v1")`,
   `sid = "clawdline-invite:" + invite_id`, `ADa = "clawdline browser invite v1 browser"`, and calls
   `POST /v1/pairing/browser-invites/{id}/join {browser_message, device_public_key}`. C locks the
   invitation to B, records a pending device id, and answers `{join_token, device_id, machine_id,
   machine_name, expiry}`. The machine id is not a CPace input, because B sends its message before
   C names the machine and C is not trusted to; it is bound inside both sealed messages instead.
3. **Answer.** M polls (`.../poll`, status `joined`), computes `Yb` with
   `ADb = "clawdline browser invite v1 machine"` and the ISK, and calls `.../answer {machine_message}`.
4. **Confirm.** B polls with its join token, computes the ISK, derives keys
   (`DeriveInviteKeys`: HKDF-SHA256, salt `"clawdline-invite:" + id`, labels
   `… confirm key`, `… handover key`, `… comparison code`), seals
   `InviteConfirm {invite_id, machine_id, device_id, public_key, name}` (AES-256-GCM, 12-byte nonce
   prepended, invite id as associated data, base64) and calls `.../confirm {browser_confirm}`.
   B shows the six digits.
5. **Compare.** M polls (status `confirmed`) and opens the confirmation. It must name this
   invitation, this machine, and the device id and public key C reported; otherwise the invitation
   is denied (a wrong secret fails here, as AES-GCM authentication). M shows the digits and waits.
6. **Decide.** On approval M **pins first** (`invited: true`, `scope: operate`, `expires_at`), then
   seals `InviteHandover` (the ordinary handover fields plus `device_id`, `scope`, `expires_at`)
   under the handover key and calls `.../admit {device_id, handover, expires_at}`. If admit fails
   the pin is revoked. On denial M calls `.../deny`.
7. **Claim.** B polls (status `admitted`), opens the handover, checks the machine fingerprint
   against the signing key, and calls `.../claim` signed with its device key. C creates the invited
   identity (`web_devices` row, kind `invited_browser`, bound to the machine, ending at
   `expires_at`) and sets its session cookie.

The test vectors are `internal/domain/cloud/testdata/cpace-ristretto255-v1.json` (the draft's) and
`internal/domain/cloud/testdata/browser-invite-v1.json` (this protocol's, recorded from Go with
`CLAWDLINE_INVITE_VECTORS=record`). The browser implementation is held to both.

## Cloud contract

All bodies are JSON. Machine routes carry the machine credential; browser routes carry the join
token issued by `join`, except `join` itself. Every route is rate-limited.

| Route | Caller | Answer |
| --- | --- | --- |
| `POST /v1/pairing/browser-invites` `{expiry}` | machine | `{invite_id, expires_at}` |
| `POST /v1/pairing/browser-invites/poll` `{invite_id}` | machine | `{invite_id, machine_id, status, browser_message?, device_id?, device_public_key?, browser_confirm?}` |
| `POST /v1/pairing/browser-invites/answer` `{invite_id, machine_message}` | machine | `{}` |
| `POST /v1/pairing/browser-invites/admit` `{invite_id, device_id, handover, expires_at}` | machine | `{device_id}` |
| `POST /v1/pairing/browser-invites/deny` `{invite_id}` | machine | `{}` |
| `POST /v1/pairing/browser-invites/revoke-device` `{device_id}` | machine | `{}`; only an invited browser bound to this machine |
| `POST /v1/pairing/browser-invites/{id}/join` | browser, unauthenticated | `{join_token, device_id, machine_id, machine_name, expiry}` |
| `GET /v1/pairing/browser-invites/{id}/browser` | browser | `{status, machine_message?, handover?}` |
| `POST /v1/pairing/browser-invites/{id}/confirm` `{browser_confirm}` | browser | `{}` |
| `POST /v1/pairing/browser-invites/{id}/claim` `{signature}` | browser | sets the invited session cookie |

Statuses: `waiting`, `joined`, `confirmed`, `admitted`, `claimed`, `denied`, `expired`. A gone,
spent or unknown invitation answers `409` or `404`, which the machine reads as
`ErrInviteUnusable`.

An invited session may mint relay tokens, read `GET /v1/machines` (only its machine) and
`GET /v1/devices` (only itself), read `GET /v1/pairing/browser-invites/session` and revoke itself
(`POST …/session/revoke`), and nothing else on the account API. The console boots it from that
session route, and only after this browser adopted an invitation (`clawdline.invited-device`), so
an account browser makes no extra request.
Its relay token names the one machine; the relay refuses every other channel.

## This machine's routes and words

Local routes (this machine's own token only):

- `GET|POST /v1/cloud/browser-invites` — list; start with `{"expiry":"8h"|"7d"|"never"}`.
- `GET /v1/cloud/browser-invites/{id}` — one invitation.
- `POST /v1/cloud/browser-invites/{id}/decision` `{"approve":bool}`; `POST …/{id}/cancel`.
- `POST /v1/cloud/browser-invites/devices/{device}/revoke` — only an invited browser.

Cloud words, all `local_exemption`: `browser-invite-start`, `-list`, `-status`, `-decide`,
`-cancel`, `-revoke`. The code is shown only while the invitation waits for a browser; the digits
only while it waits for a decision. Neither is logged.
