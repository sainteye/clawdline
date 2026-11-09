<!-- clawdline-doc: kind=reference audience=both -->

# Browser voice recording recovery

The current Go console records in the browser and sends audio to the existing
Whisper host. Dictation inserts recognised words into the composer; it never
sends a Session message. This page covers the current web recording lifecycle;
[the earlier Whisper design record](whisper.md) retains its historical context.

Pressing **Done** calls `MediaRecorder.stop()` and immediately stops every
microphone track and the level-meter context in the same event handler. The
final `dataavailable` and `stop` callbacks remain attached until finalization;
closing capture must not detach the handlers that deliver the recorded bytes.
The UI distinguishes finishing the recording, preparing audio in the browser,
and waiting for host transcription. The model-loading explanation appears only
in the last phase.

The browser is asked for one-second checkpoints. These are kept in order, with
the recorded MIME type. The browser may delay checkpoints; individual chunks
are not promised to be playable. Only the combined recording is decoded.

If finishing, preparation or transcription stalls, the UI offers recovery and
keeps the audio already delivered by the browser. **Cancel processing** pauses
waiting and keeps that recording. **Discard recording** explicitly clears it.
A normal successful transcript clears the recording and inserts the words once;
an empty transcript remains retryable. Retry uses the recording already held,
without acquiring a new microphone. A still-pending preparation or host request
is reused rather than started again on each press. A late answer from a paused
or discarded attempt cannot insert words. If final bytes arrive during a partial
attempt, that attempt is fenced and the completed recording is offered for retry;
a full retry waits for any previous host request to settle before issuing another.

If the browser has delivered only checkpoints, the recovery row explicitly warns
that the audio may be incomplete before allowing an attempt. If it has delivered
no bytes, recovery says so and can wait for the existing recorder again. It does
not silently start another recording. Final bytes arriving after a timeout are
kept and make a full retry available. Backgrounding and `pagehide` finish capture.

Retention is **one recording in the current page's memory**, not persistent
browser storage. Leaving the owning composer, reloading, closing the page or
explicit discard releases it. A recording is limited to 16 MiB of encoded chunks;
new bytes over the cap are refused, capture stops, and recovery identifies the
kept audio as potentially incomplete. The existing three-minute duration limit
continues to apply. This cannot recover bytes the browser never delivered.

The recovery deadlines are 5 seconds for recorder finalization, 15 seconds for
browser preparation, and 120 seconds for a host response. They bound the UI wait,
not remote execution: cancelling or timing out cannot recall a Whisper request
already executing. Retry shares that pending request. Byte and deadline policies
are registered under `console.voice_*` in `internal/domain/capacity`, mirrored to
the browser with a drift check, and exposed through the capacity measurement map.
The machine cannot measure another browser's recording memory.

Cloud recovery events use the existing `diagnostics.events` delivery path.
`voice.recording.recovery` contains machine, stage, a typed recovery code, byte
count and completion flag. Audio, words, Session titles and raw browser error
messages are not included. This is diagnostic delivery, not audio backup.

## Verification

`web/console/src/legacy/voice-bridge.test.mjs` executes the actual module against
controlled browser events, timers, decoding and host results. It covers delayed
or missing stop events, checkpoints, zero bytes, late final data, host failure,
repeated retry, cancellation, stalled operations, stale events from a discarded
recording, empty recognition, byte overflow, backgrounding and decoder cleanup.
`VOICE_TEST_REV=<revision>` runs the same test against a specified Git revision.

The browser fixture at `web/console/tools/fixtures/voice-recovery.html` uses the
real Composer, transcript, voice module and production styles. Fault modes replace
only the recorder/stream and transcription boundary. `mode=native` uses the
browser's real MediaRecorder with a locally generated tone stream and real audio
decoding/resampling. It never opens a physical microphone, contacts a daemon,
sends a Session message, or invokes a remote transcription provider.

Start the existing Vite server on loopback, with
`CLAWDLINE_NEXT_ORIGIN=http://127.0.0.1:1` so an unhandled request cannot reach a
person's daemon. Run `web/console/src/session/voice-recovery.e2e.mjs` with
`VOICE_UI_ORIGIN`, `PLAYWRIGHT_MODULE` pointing to an existing Playwright entry,
and optional `VOICE_UI_OUTPUT`. The test captures V-01–V-12, exercises 320/390 px
mobile and 1100 px English desktop layouts, and checks native recorder completion
in WebKit and Chromium. Screenshots are labeled local fault injection. They do
not establish physical iPhone microphone-indicator behavior or Cloud deployment.
