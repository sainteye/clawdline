# MP4 playback in Clawdline

The Documents page also lists published MP4 videos. Open a Session's menu, choose
**Documents**, then select a video, or follow its canonical Cloud document link.
The reader shows transfer progress and then native playback, pause, seeking and
full-screen controls. Playback requires a click; it never starts automatically.
For the widest phone compatibility, export MP4 with H.264 video and AAC audio.
Unsupported codecs or malformed content produce a visible playback error.

## Publishing and identity

Publish an `.mp4` file under the Session project's `artifacts/` directory or its
owned task's `artifacts/` directory. A path in a conversation alone does not publish
it. Use the same strict machine, Session, scope, task and relative-path locator as
[document links](web-pages.md#document-links). Share constructs the canonical
`https://app.clawdline.com/` fragment without credentials. There is no public media
URL and no external hosting or transcoding service.

Placing a file under this publication root makes it readable to devices already
authorized to read that Session. The same configured project-root containment
policy applies. Relative paths reject hidden segments, traversal, absolute paths
and backslashes. Video opens use Go's `os.Root` so a symlink changed between path
validation and open cannot escape the authorized root. As with existing artifacts,
a local actor who can write this publication directory can publish bytes there;
this does not defend against that actor creating hard links or preserving file
metadata while changing content.

## Bounded encrypted reads

Text and sanitized HTML retain their 2 MiB cap and inert renderer. MP4 has a
separate 32 MiB file limit (`artifacts.video_bytes`) and a 512 KiB chunk limit
(`artifacts.video_chunk_bytes`). The file's bytes must be identified as MP4;
its extension alone is insufficient. The daemon does not decode or transcode
video. The browser's media decoder still determines codec support and validity.

A video uses the `video` Cloud read operation, never the text `document` operation.
The existing authenticated document route returns `VideoChunk` JSON for MP4 only,
with explicit `offset` and `revision` queries. Scope, task, path, MIME, offset,
revision, total length, chunk length and canonical base64 must match before the
browser retains bytes. Every chunk passes through the existing pairing and
revocation gates, including the check after reading. Generation-pinned content
reads also retain their execution proof. Legacy Session reads preserve their
existing authorization semantics.

The first chunk starts at zero with an empty requested revision. Following chunks
repeat its size/mtime revision at exact 512 KiB boundaries. Ordinary file changes
stop a transfer with `video_changed`; this marker is not a cryptographic content
hash. The open descriptor is checked before and after each chunk. No chunk can
request an arbitrary amount of disk data or an arbitrary external URL.

The browser reads sequentially, without prefetch or automatic chunk retries, then
assembles one bounded Blob for native playback and local seeking. A trusted
iframe contains the native player and a nonce-authorized event adapter. Its
sandbox gives it an opaque origin with no same-origin privilege. Its CSP permits
Blob media, inline styling and that one trusted script only; it denies external
resources, connections, other scripts, forms and base URLs. The parent transfers
the verified Blob to the player, which creates its URL in its own origin. The
parent accepts error and dimension messages only from the exact player window with its random
per-player token. Author bytes and filenames never become frame HTML. This first
version downloads the complete video before playback; it is not streaming or an
unlimited media library. Navigation aborts pending reads, prevents another chunk
from starting, requests native player cleanup and removes the player browsing
context, which also releases that document's Blob URLs. Temporary browser copies and decoder memory are bounded by the file size and browser behavior;
the file cap is not a claim that total heap use equals that exact size.

Revocation prevents subsequent reads. It cannot erase content a device has
already decrypted, downloaded or copied. A failed or disconnected transfer needs
an explicit **Read again** action. A console without the `video` word gets the
existing unsupported-feature refusal from an older daemon. Deploy the console
and update the publishing daemon to enable playback; a hosted bundle alone does
not add video support to an older daemon. An older console cannot validate an
MP4 listing, so reload it after updating.

## Verification

Behavior tests cover chunk boundaries and version changes, traversal and symlink
escape, fake MP4 bytes, oversize rejection, strict answer identity and byte counts,
revocation before and during Cloud reads, sequential assembly and cancellation.
Capacity registration is checked against daemon measurements and both snapshots.
Native browser playback acceptance is separate from wire and authorization tests;
a desktop WebKit test is not an actual iPhone acceptance receipt.

Local acceptance on 2026-10-27 used the real Documents controller, translation
adapter and player in installed Chrome and desktop WebKit at 390 and 1280 CSS
pixels. Both English (the Project default) and Taiwan Traditional Chinese were tested;
all eight cases decoded an 18-second 1920×1080 H.264/AAC MP4, sought to
nine seconds, played and paused, with no horizontal overflow. The 14,689,551-byte
sample required 29 sequential chunks. Leaving removed the player browsing context and its document-owned
Blob URL. Leaving during the first pending read kept the request count at
one and created no player or Blob URL. These are local component measurements;
they do not establish authenticated production Cloud playback or actual iPhone
acceptance.

Before registration, `TestEveryBoundIsRegistered` failed on both new video byte
constants. After registration, the same guard, copy checks and both capacity
snapshot checks passed. The red output is retained in the task's local evidence;
private paths from that run are not published here.

The [browser record](performance/video-artifact-browser.json) retains the eight
playback, opaque-origin, cancellation and CSP cases. The local HTTP collector
received eight parent-origin control requests and zero injected image, media or
fetch requests from the player. Injected script did not run. A separate malformed
Blob produced the native error, shown in both English and Taiwan Traditional Chinese. The record
observes browsing-context removal, not a browser heap-reclamation measurement.
