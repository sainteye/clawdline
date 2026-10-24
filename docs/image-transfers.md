# Conversation image delivery

Conversation images are stored by the originating daemon. Opening a picture on the hosted
console reads those bytes from that machine; the hosted site is not an image archive. The
machine must be online. A ready WebRTC carrier takes the read directly; otherwise the
end-to-end encrypted relay carries it. Both thumbnails and originals retain relay fallback.
This is separate from the public website's static images and Board reference images.

## Previews and originals

`GET /v1/artifacts/images/{id}?size=thumb` returns a JPEG with a longest edge of 480 pixels,
using the same bounded thumbnail cache as Board reference images. The route resolves the
stored artifact first, so a cached thumbnail cannot outlive a deleted or expired original.
Omitting `size` returns the unchanged stored picture. Other `size` values are refused.
Pinned Cloud reads keep the same machine, Session, execution generation and permission checks
for both variants.

The machine advertises `machine.image_thumbnails_v1`. A new console asks such a machine for
thumbnails; an older machine keeps its original image read rather than receiving an unsupported
request. The image read's optional `size: "thumb"` has the reply name `image.<id>.thumb`.
The original remains `image.<id>`. Their request proofs and waiters cannot be confused.

The conversation starts preview reads when their tiles intersect the viewport. Opening the
preview requests the original, and download and share continue to use that original. Gallery
navigation may open an offscreen original without first loading its thumbnail. A failed transfer
ends its loading state and offers another attempt; expired or missing artifacts remain distinct.
Removing a tile disconnects its visibility observer and releases delivered object URLs, including
answers arriving after removal.

## Ordering and capacity

An ordered DataChannel delivers envelopes in wire order, but asynchronous pairing lookup and
WebCrypto can complete in another order. Applying the sequence check after independently
starting those operations can reject a genuine earlier answer as replay and close the carrier.
The console now authenticates and applies each carrier generation in arrival order. Separate
machines and generations have separate chains; the signature and replay checks remain intact.

The browser limits all pending encoded carrier envelopes together to 32 MiB and 32 rows.
Three maximum-sized direct messages (9 MiB each) fit within the byte budget, leaving room for
small status traffic. This is a buffer budget, not a claim about the browser's total heap:
strings, cryptographic buffers, decoded pixels and displayed images need additional memory.
Overflow closes the affected carrier and leaves pending reads to the existing fallback policy.
The daemon's capacity page describes these browser-owned limits without claiming it can measure
another device's live queue.

The three image-read slots and eight daemon content-read slots remain concurrency limits,
not limits on how many pictures a conversation can display. Removing them for direct connections
would still allow unbounded image decoding, encryption and endpoint memory use. Thumbnail CPU
measurements alone cannot establish a safe larger admission limit for a phone, a smaller executor,
or several independent viewers.

The relay answer contains Base64 image data inside a sealed envelope whose ciphertext is also
Base64 on the wire: the leading-order expansion is 16/9 of the stored bytes, before JSON and
cryptographic overhead. Reducing preview bytes saves work on both direct and relay paths.
The current 4 MiB per-channel daemon spool budget also means that several individually permitted
originals may exceed the channel's aggregate outstanding budget. A count alone cannot guarantee
admission; typed capacity refusals must remain visible.

## Retention is a separate limit

The conversation artifact store currently keeps at most 64 live pictures and 64 MiB across the
machine, with a 24-hour expiry. This is independent of the three/eight concurrent reads.
Large image batches can displace pictures still referenced by older conversations. Thumbnail
support does not change retention, reconstruct evicted originals, or make the hosted console an
archive. Retention needs its own product policy and disk-budget review rather than being raised
as a substitute for fixing transfer failures.

## Relay follow-up

Keeping relay fallback is an availability choice, not evidence that arbitrary image load is safe.
A bounded last-envelope cache prevents that cache from growing without limit; it does not bound
all signature verification, active frames, runtime socket buffers, repeated requests or fan-out.
The image reply path needs a separate traffic and backpressure review. A local emulator or one
successful account is not a production capacity result.

The follow-up must cover:

- Per-account and per-device byte and work admission, including encrypted/Base64 expansion,
  with an explicit shared budget for concurrent image replies.
- Bounded outstanding bytes for slow viewers, acknowledgement semantics, typed refusals and
  reserved capacity for small control traffic.
- Retry backoff, request coalescing, cancellation and the amplification when a direct carrier
  fails and several reads simultaneously fall back to relay.
- Tests that vary image size, viewer count, recipient fan-out, network speed and stalled viewers.
  Measure accepted, delivered, decoded and displayed results separately, along with queue bytes,
  memory, CPU and tail latency.
- Isolation between accounts and continued operation of small commands under image pressure.
  Publish the tested deployment, workload and failure boundary before claiming a supported
  production capacity.

Relevant implementation: `internal/transport/http/images.go`,
`internal/adapters/artifacts/thumbnail.go`, `internal/app/cloudops/answer.go`,
`internal/transport/cloud/link.go`, `web/console/src/cloud/status-client.js`, and
`web/console/src/legacy/js/view/transcript-images.js`.
