# MP4 playback in Clawdline

Published MP4 links open a dedicated video lightbox above the current conversation.
The conversation stays mounted at its original scroll position. The lightbox shows
buffering progress and native playback, pause, seeking and full-screen controls.
Playback can begin while later encrypted chunks are still arriving.
Close, Escape, or browser Back dismisses it. Forward can reopen a lightbox
closed with Back. Close and Escape synchronously mark its history entry closed;
immediate reopening reuses that entry rather than waiting for an asynchronous
Back event. Closing aborts the transfer and destroys the player context.
An existing canonical Cloud document link to an MP4 also opens the lightbox on a
Session screen, rather than navigating to the Documents page. Markdown, text and
HTML links keep their existing document reader. The Documents inventory still
lists published videos. Playback requires a click; it never starts automatically.
For the widest phone compatibility, export MP4 with H.264 video and AAC audio.
Unsupported codecs or malformed content produce a visible playback error.

## Conversation previews

Assistant messages with complete same-origin MP4 publication links also show a
thumbnail card with a play button. Only actual rendered links qualify; code
examples, external links and incomplete locators never trigger thumbnail reads.
A card starts reading when it enters the viewport, pauses when it leaves, and
samples random-access frames from later parts of the movie. It uses the same authenticated, encrypted,
revision-pinned video chunks as playback. No thumbnail is uploaded or published.

Each browser permits one thumbnail read at a time, reads at most 4 MiB of source
bytes per attempt, waits at most 30 seconds and scales the longest thumbnail side
to 640 pixels. These limits are registered as `console.video_preview_*`. Visible
cards borrow a slot without an unbounded waiting queue; opening a lightbox pauses
thumbnail work. A failed preview retains a usable play link and an explicit
fallback label. A large index or large keyframe may exhaust the preview budget
while the full player remains usable. There is no automatic failure retry.

The candidates start near 35%, 60%, 12% and 82% of the duration, decoding from the nearest
preceding random-access frame through the requested time. Keyframes often land
on transitions, so the keyframe itself is not the thumbnail. Four candidates share one revision-pinned chunk
cache and byte budget, including metadata; repeated chunks are reused. Only the
video track is remuxed for sampling. The native decoder seeks to the requested buffered
time and stops before another candidate is opened. This requires no external
service, installed media utility or new daemon read operation.

Decoded pixels are ranked by luminance contrast, edge detail and colour diversity
inside the central image. Near-solid black, white, coloured intros and fades rank
below frames with visible detail. This is a visual heuristic, not semantic scene
understanding. The highest-ranked completed frame wins; budget exhaustion or the
sampling deadline preserves an already decoded candidate. External cancellation
discards it, and authorization refusals are propagated. Unsupported media retains
the usable play link.

A thumbnail belongs to its mounted conversation card and is revoked on unmount.
The modal borrows a separate URL for that image, so closing the modal cannot
invalidate the card. The lightbox reserves a 16:9 stage before its first response,
shows the available poster with a play button and loading indicator, then replaces
the cover with native video after a person presses Play. Different video aspect
ratios fit inside this reserved stage. The status line retains its space after
loading, keeping the player and Close button stationary.

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
reads also retain their execution proof. A link in one conversation may name
another Session on the same paired machine. The linked Session's current execution
is resolved from the verified machine projection, independently of the open
conversation. One transfer signal pins it across metadata, poster candidates and
playback chunks. A source execution replacement refuses later chunks; only a new
attempt may resolve a new execution. This does not change action destinations or
permit another machine, unverified Session, or revoked device. Legacy Session
reads preserve their existing authorization semantics.

The first chunk starts at zero with an empty requested revision. Following chunks
repeat its size/mtime revision at exact 512 KiB boundaries. Ordinary file changes
stop a transfer with `video_changed`; this marker is not a cryptographic content
hash. The open descriptor is checked before and after each chunk. No chunk can
request an arbitrary amount of disk data or an arbitrary external URL.

The lightbox validates the first chunk before mounting native video controls. It
uses the pinned BSD-3-Clause [MP4Box.js](https://github.com/gpac/mp4box.js) library
(its [license notice](../web/console/public/licenses/mp4box-BSD-3-Clause.txt) ships with the console)
to parse the MP4 index and remux complete samples into Media Source Extensions
fragments locally. This changes the container, without re-encoding or an external
transcoding service. A tail `moov` index is discovered by jumping over media data;
its authenticated chunks are reused when the media pass returns to the start.
Files with unusually large indexes still need those indexes before playback.

The media pass reads sequentially, with one authorized chunk request in flight.
It waits for the decoder's append acknowledgement before fetching another chunk.
Validated sample extents must fit the published file, and each pending fragment
batch shares the existing 32 MiB video-byte ceiling. Parsed sample data is released
after fragmentation. A browser can play the buffered prefix while later chunks
arrive; seeking beyond the buffered prefix waits for those later chunks. There is
no automatic read retry. The progress label describes transferred source bytes,
not elapsed playback or an exact decoder-memory measurement.

Streaming uses a native `<video>` element in the trusted console lightbox. Both
tracks are declared before either initialization segment reaches the decoder.
MediaSource is used in supporting browsers; WebKit's ManagedMediaSource is used
when available, with remote playback explicitly disabled as required for iPhone.
AirPlay is therefore unavailable for this streaming player. Playback still needs
a click and never starts automatically. A current browser with MSE or managed MSE
and compatible codecs is required. Physical iPhone acceptance is separate from
desktop WebKit testing.

The streaming player is trusted UI, rather than an author document: its DOM is
created by the console and its only source is a console-created MediaSource URL.
No file name, metadata or author bytes become HTML, script, CSS or a network URL.
An opaque-origin MSE Blob is refused by WebKit, so the streaming controls do not
use the earlier opaque iframe. This is an explicit change of rendering boundary;
it preserves encrypted chunk authorization but does not claim opaque-origin
isolation for the streaming media element. Sanitized HTML retains its separate
sandbox. The legacy complete-Blob document preview retains its nonce-authorized
opaque player, where the Blob URL is created in the player's own origin.

Closing or navigating aborts pending reads and decoder acknowledgements, pauses
and resets the native player, revokes its MediaSource URL and releases fragment
queues. Native decoder failures and late read refusals stop further requests and
leave an explicit **Read again** action. Browser decoder memory and remux metadata
are additional to source bytes; the file cap is not an exact browser heap bound.

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
revocation before and during Cloud reads, sequential assembly and cancellation. Streaming tests additionally exercise tail-index
discovery, decoder backpressure, late revocation and decoder refusal.
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

The [lightbox browser record](performance/video-lightbox-browser.json) adds eight
local Chrome/WebKit cases in English and Taiwan Traditional Chinese at 390 and
1280 CSS pixels. They verify playing and seeking the same sample, unchanged
conversation address, draft and scroll position, focus restoration, Escape from
inside the isolated player, Back/Forward, cancellation despite a late answer,
explicit retry after refusal, and a cold MP4 link. These component cases are
separate from production Cloud acceptance and actual phone testing.

## Streaming acceptance

The [streaming browser record](performance/video-streaming-browser.json) covers
local Chrome and desktop WebKit in English and Taiwan Traditional Chinese at
390 and 1280 CSS pixels. The media transfer is deliberately held before its final
chunks; native playback must advance before that gate is released. The same cases
cover seeking after completion, conversation address/draft/scroll preservation,
Escape, focus restoration, history, cancellation, retry and cold links. A second
run uses a remuxed MP4 whose index is at the end. These controlled component
measurements are separate from authenticated production Cloud acceptance.

`web/console/src/cloud/testdata/video-stream.mp4` is a generated 32-by-32 colour
clip with a generated tone, used for parser and backpressure tests; it contains no
Session artifact or conversation content.

## Preview card acceptance

The [preview card browser record](performance/video-preview-cards-browser.json)
records installed Chrome acceptance in English and Taiwan Traditional Chinese.
An offscreen card issued no read; its first visible attempt decoded a 640-pixel
thumbnail after four sequential chunks (2 MiB), then stopped. The same thumbnail
appeared immediately in the lightbox. Player geometry matched before and after
loading at both measured widths, with no phone-width horizontal overflow.
Card and text links reopened after Close; Back/Forward, draft, scroll and card
focus also remained usable. This is local component evidence, separate from
production Cloud and physical iPhone acceptance.

The representative-thumbnail fixture `web/console/src/cloud/testdata/video-poster.mp4`
is a generated 96-by-54 test pattern with regular keyframes. Every keyframe is
black and the following frames contain detail, so capturing the keyframe instead
of decoding through the requested time is a visible near-miss. Tests verify later
sample times, multiple distinct positions, shared chunk reuse, cancellation,
read limits, partial-result preservation and authorization refusal propagation.
Pixel tests cover black, white, coloured and near-uniform fades versus visible
subjects. The earlier first-frame acceptance above records the superseded policy.

The [representative poster browser record](performance/video-representative-posters-browser.json)
records two 40-second samples in installed Chrome. Both selected visible subjects,
interfaces and captions instead of their solid opening frames. The black-keyframe
regression clip selected a detailed non-keyframe; ordinary playback and the modal
poster remained usable. The larger sample reached its 4 MiB budget and retained
an already decoded candidate. These are local component observations, separate
from production Cloud and physical phone acceptance.
