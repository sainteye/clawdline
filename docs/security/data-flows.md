# Content and outbound data boundaries

This is a source inventory for `bbd850a596d18805d1298356e05393df34c04a9b`, with
synthetic evidence described in [egress-evidence.md](egress-evidence.md).
It is an engineering description, not a legal compliance determination. No
jurisdiction, legal processing basis, service region or contractual retention
schedule was established by this work. Default destinations below are code
defaults, not DNS observations or a list of every possible configured endpoint.

## Inventory

| Trigger and purpose | Data collected locally | Recipient and content boundary | Metadata and disclosure | Retention and deletion evidence |
| --- | --- | --- | --- | --- |
| Local console reads | Existing provider conversation records, project files and terminal screen needed for the selected feature | Local daemon and browser receive plaintext; this harness does not start a daemon or exercise provider ingestion | Local browser and machine are trusted; source records can contain secrets | Source/provider records remain on disk; this work does not establish deletion of source archives, backups or browser copies |
| Cloud enabled, Session publication | Session IDs, execution/state/freshness facts; legacy full presentation can include project/title text | Default `wss://relay.clawdline.com/v1/connect`; `ss/` projection excludes transcript and presentation prose; full `s/` presentation and pinned list reads are separate. Payloads are sealed locally | Account/device identity, channel (including machine/Session IDs), class, sequence, timestamp, key ID, ciphertext length, timing and source network address remain service-visible; encrypted status facts are not plaintext relay fields | Local outbound spool owns retries; service retained rows, backups and deletion were not inspected |
| Open conversation or history | Requested transcript/page and request identifiers | Viewer request crosses relay, machine routes a read, and reply is locally sealed. Default API `https://api.clawdline.com` supplies account/control operations. No claim that the whole conversation archive is uploaded as a backup | Relay routing/traffic metadata remains; authorized browser gets plaintext after decrypting | Source stays local; reply/spool and browser copies have separate lifetimes; no service-side purge verified |
| Open document or image | Selected filename/path, document bytes, image bytes and potentially embedded metadata; original and thumbnail are distinct reads | Content reads use the Cloud bridge and sealed answers; direct carrier may carry supported reads. Sealing does not remove EXIF, secrets in text or sensitive filenames from the authorized viewer | Routing IDs and traffic size remain visible outside content encryption; viewer learns content and embedded metadata | This package does not verify filesystem authorization, thumbnail caches, browser caching or deletion |
| Terminal use | Terminal screen, history, input and control messages | Relay `term/`, `termd/`, `termr/` output uses a connection key (`rk-`); direct carrier is an alternate path, with encrypted relay fallback | Channel includes viewer/connection identifiers; screen content is plaintext on endpoints. Input can execute code under separately enforced authority | No terminal archive deletion or live direct cleanup was measured by this harness |
| Send notification | Synthetic equivalent of task title, project/state summary, deep link and tag | Machine seals Web Push to subscriber keys. Cloud `POST https://api.clawdline.com/v1/push/send` forwards ciphertext; local subscriptions instead POST to their registered push-service endpoint | Cloud sees subscription ID, content type, TTL, urgency, hashed topic, length and timing. Push provider sees endpoint, delivery metadata and sealed payload. Browser/OS can display title/body on a lock screen | Local subscriptions have a store/removal path. Provider TTL is a delivery request, not proof of deletion from logs/backups or the phone |
| Background build comparison, including Cloud off | No conversation needed | Default `GET https://app.clawdline.com/BUILD.json`, no request body; remote build stamp is downloaded | Host/IP, request path, timing and ordinary HTTP headers visible to host; current local version comparison happens locally | Checker keeps last result; host logging/retention not established |
| Release check/download, including Cloud off | Selected channel/version; platform/architecture used for artifact selection | GitHub release manifest/signature GETs; beta discovery uses `https://api.github.com/repos/sainteye/clawdline/releases?per_page=30`; archives use URLs in the signed manifest | Version in URL where selected; artifact filename/URL can reveal OS/architecture. Redirect/CDN destinations and network address visible to recipients. Manifest GET does not upload a conversation or a current-version body | Local release/update state and downloaded artifacts are separate from conversation storage; GitHub/CDN retention not verified |
| Direct connection negotiation | ICE candidates and connection descriptions | STUN defaults `stun:stun.cloudflare.com:3478`, `stun:stun.l.google.com:19302`; direct peer is paired browser/machine. Cloud still carries signalling | STUN services learn the requesting network address. Peer negotiation can disclose candidate addresses. Content encryption does not hide network topology from those participants | No STUN log retention, geographic destination or live ICE traffic measured |

## Public source anchors

These links pin the reviewed production source. New tests are linked in the
[evidence page](egress-evidence.md). Re-check anchors when changing the wire.

- Defaults: [Cloud settings, lines 69–75](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/adapters/cloud/settings.go#L69).
- Session field minimization: [SessionStatus allowlist and projection, lines 20–54](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/transport/cloud/session_status.go#L20). Prefer this projection for fleet discovery; titles/documents should remain requested content rather than extra routing metadata.
- Requested content: [bridge content-read vocabulary, authorization and reply binding, lines 373–469](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/app/cloudops/cloudops.go#L373).
- Local sealing and terminal key selection: [Relay.PublishTracked, lines 272–389](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/transport/cloud/relay.go#L272); [envelope fields and serialization, lines 108–160](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/domain/cloud/envelope.go#L108).
- Retry storage boundary: [outbound spool](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/adapters/cloud/spool.go#L1). It is not a server retention audit.
- Push plaintext/lock-screen boundary: [Notification, lines 13–45](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/adapters/push/message.go#L13); [sender](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/adapters/push/send.go#L229); [Cloud payload fields, lines 65–93](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/adapters/cloud/push_send.go#L65).
- Updates: [build comparison source and fetch, lines 51–145](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/adapters/updatecheck/updatecheck.go#L51); [release defaults, lines 128–130](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/adapters/release/updater/updater.go#L128); [release HTTP GET and signature fetch, lines 89–145](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/adapters/release/updater/fetch.go#L89).
- Address disclosure: [STUN list, lines 56–58](https://github.com/sainteye/clawdline/blob/bbd850a596d18805d1298356e05393df34c04a9b/internal/transport/cloud/direct.go#L56).

## Trust and minimization

The machine, its local records, paired browser, browser-delivered application,
and notification display remain trusted endpoints. A browser supplied with
malicious application code could disclose plaintext after legitimate decryption.
Revoking access cannot erase content already read or previously obtained keys
and ciphertext. This evidence does not establish future-key rotation, revocation
timing, legal compliance, service retention or absence of every outbound flow.

For each optional feature, the data should be limited to its purpose: projected
status for discovery, requested pages/files for reading, and only necessary
summary text for notification display. The inventory above does not establish
whether a privacy notice or consent UI states a legal processing basis. No new
production collection, destination or retention policy is introduced here.
