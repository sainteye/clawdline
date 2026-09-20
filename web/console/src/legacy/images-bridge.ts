// The transcript's pictures.
//
// `js/view/transcript-images.js` is the Swift app's module byte for byte and is
// imported as it is: it takes its nodes as arguments and imports nothing, so
// the tile states, the reconciliation across redraws and the lightbox's zoom
// are the original's own code. What `view/transcript.js` does around it —
// `artifactTilesHTML`, `describeArtifactFailure`, `hydrateArtifactImages` and
// the lightbox's markup from `index.html` — is here, for `session/Transcript.tsx`.
//
// No `source` is given to a tile, as on the Mac's own page: the bytes are asked
// of this origin at `/v1/artifacts/images/:id`. A console reading a machine
// across Clawdline Cloud cannot do that — its origin is a static host, and an
// `<img>` does not go through `fetch` — so it hands in one (`carryPictures`),
// as the Swift app's hosted console hands `CloudClient.image` to the same tile.
import { T, fill } from "./js/core/i18n.js"
import { esc } from "./js/core/esc.js"
import { failureSentence as failureSentenceOriginal } from "./js/core/failure-text.js"
import {
  connectArtifactTile as connectArtifactTileOriginal,
  createImageLightbox as createImageLightboxOriginal,
  reconcileArtifactTiles as reconcileArtifactTilesOriginal,
  releaseArtifactTile as releaseArtifactTileOriginal,
} from "./js/view/transcript-images.js"

/** One picture as a transcript entry carries it (`ImageArtifact`). */
export interface ArtifactRef {
  id: string
  media_type: string
  byte_count: number
  width: number
  height: number
  expires_at: number
  state?: string
}

type Lightbox = { open: (trigger: Element, src: string, failed?: () => void) => void; close: () => void }

const connectArtifactTile = connectArtifactTileOriginal as (
  tile: HTMLElement,
  artifact: ArtifactRef | undefined,
  options: Record<string, unknown>,
) => unknown
const reconcileArtifactTiles = reconcileArtifactTilesOriginal as (
  current: ArrayLike<HTMLElement>,
  next: ArrayLike<HTMLElement>,
  artifacts: (ArtifactRef | undefined)[],
  options?: { activeElement?: Element | null },
) => { fresh: HTMLElement[]; reused: HTMLElement[]; dropped: HTMLElement[]; restoreFocus: () => void }
export const releaseArtifactTile = releaseArtifactTileOriginal as (tile: HTMLElement) => void
const createImageLightbox = createImageLightboxOriginal as (
  dialog: HTMLElement,
  image: HTMLImageElement,
  close: HTMLElement,
  doc: Document,
) => Lightbox

/**
 * `artifactTilesHTML`: static markup only. The artifacts stay in the caller's
 * queue and reach each tile as DOM properties after parsing, so a reference can
 * never add HTML, an action or a URL. `firstSlot` is where this entry's
 * pictures start in that queue.
 */
export function artifactTilesHTML(artifacts: readonly unknown[] | undefined, firstSlot: number): string {
  if (!Array.isArray(artifacts) || !artifacts.length) return ""
  const tiles = artifacts
    .map(
      (_, i) =>
        '<button class="message-image-tile" type="button" disabled ' +
        'data-artifact-slot="' +
        (firstSlot + i) +
        '" aria-label="' +
        esc(T.webImagePreview) +
        '">' +
        '<img class="message-image" alt="" hidden>' +
        '<span class="message-image-state"></span>' +
        "</button>",
    )
    .join("")
  return '<div class="message-images">' + tiles + "</div>"
}

/** `core/failure-text.js`'s `failureSentence`, typed for this module. */
const failureSentence = failureSentenceOriginal as (error: unknown, fallback: string) => string

/**
 * `describeArtifactFailure`: the words a tile shows when the picture did not
 * come.
 *
 * Two codes have words of their own here and one has none on purpose — an
 * expired or missing artifact is the tile's ordinary end and says nothing. The
 * rest used to be `T.webImageUnavailable`, one sentence with no subject for
 * `forbidden`, `rate_limited`, `machine_offline` and `cloud_not_carried`
 * alike; they now go through the catalog, which names the code and puts
 * `code · ref` after it.
 */
function describeArtifactFailure(code: string, artifact: ArtifactRef | undefined): string {
  if (code === "artifact_expired" || code === "artifact_not_found") return ""
  if (code === "image_too_large_for_cloud") {
    const bytes = artifact && artifact.byte_count ? artifact.byte_count : 0
    return fill(T.webImageTooLarge, { mb: (bytes / 1048576).toFixed(1) })
  }
  return failureSentence({ code }, T.webImageUnavailable)
}

let lightbox: Lightbox | null = null

/** A picture's bytes as a URL the tile can show, and the way to let them go. */
export type PictureSource = (artifact: ArtifactRef, session: string) => Promise<{ url: string; release?: () => void }>

let carried: PictureSource | null = null

/**
 * Hand every tile drawn from now on its bytes through `source`, rather than
 * pointing its `<img>` at this origin. Set once, before the console is drawn,
 * by a console that reads its machine across Clawdline Cloud (`cloud/install.ts`).
 */
export function carryPictures(source: PictureSource): void {
  carried = source
}

/**
 * The lightbox, drawn once as `index.html` writes it and bound once as
 * `view/transcript.js` binds it at import. It is the body's own child, after
 * everything else, as it is there; its two words are `view/static.js`'s.
 */
function imageLightbox(): Lightbox {
  if (lightbox) return lightbox
  let dialog = document.getElementById("image-lightbox")
  if (!dialog) {
    dialog = document.createElement("div")
    dialog.className = "image-lightbox"
    dialog.id = "image-lightbox"
    dialog.setAttribute("role", "dialog")
    dialog.setAttribute("aria-modal", "true")
    dialog.setAttribute("aria-labelledby", "image-lightbox-title")
    dialog.hidden = true
    dialog.innerHTML =
      '<div class="image-lightbox-frame">' +
      '<span id="image-lightbox-title" class="sr-only">Image preview</span>' +
      '<button id="image-lightbox-close" class="image-lightbox-close" type="button">Close preview</button>' +
      '<img id="image-lightbox-image" alt="" draggable="false">' +
      "</div>"
    document.body.appendChild(dialog)
  }
  const title = dialog.querySelector<HTMLElement>("#image-lightbox-title")!
  const close = dialog.querySelector<HTMLElement>("#image-lightbox-close")!
  const image = dialog.querySelector<HTMLImageElement>("#image-lightbox-image")!
  title.textContent = T.webImagePreview
  close.textContent = T.webImageClose
  lightbox = createImageLightbox(dialog, image, close, document)
  return lightbox
}

/** `hydrateArtifactImages`: give each fresh tile its state, its listeners and its source. */
function hydrate(tiles: HTMLElement[], queue: (ArtifactRef | undefined)[], session: string | undefined): void {
  const source = carried
  for (const tile of tiles) {
    const artifact = queue[Number(tile.dataset.artifactSlot)]
    connectArtifactTile(tile, artifact, {
      ...(source && session ? { source: (a: ArtifactRef) => source(a, session) } : {}),
      loadingLabel: T.webLoading,
      expiredLabel: T.webImageExpired,
      unknownLabel: T.webImageUnknown,
      describeFailure: describeArtifactFailure,
      open: (trigger: Element, src: string, expire: () => void) => imageLightbox().open(trigger, src, expire),
    })
  }
}

/**
 * The tiles of one transcript, across its redraws.
 *
 * The original stages every redraw and moves unchanged, already-loaded tiles
 * into it (`replaceTranscriptContents`). React leaves an entry's markup alone
 * when it has not changed, so here only the markup it did replace needs that:
 * after each draw, the tiles not yet connected are the fresh placeholders, the
 * connected ones no longer in the page are what the draw replaced, and the
 * original's `reconcileArtifactTiles` moves the second into the first where
 * their keys agree. Whatever is left fresh is hydrated.
 */
export class ArtifactTiles {
  private connected = new Set<HTMLElement>()

  settle(box: HTMLElement | null, queue: (ArtifactRef | undefined)[], session?: string): void {
    if (!box) return
    const now = Array.from(box.querySelectorAll<HTMLElement>("[data-artifact-slot]"))
    const fresh = now.filter((tile) => !this.connected.has(tile))
    const gone = Array.from(this.connected).filter((tile) => !tile.isConnected)
    for (const tile of gone) this.connected.delete(tile)
    if (!fresh.length) {
      for (const tile of gone) releaseArtifactTile(tile)
      return
    }
    const result = reconcileArtifactTiles(gone, fresh, queue, { activeElement: document.activeElement })
    for (const tile of result.reused) this.connected.add(tile)
    hydrate(result.fresh, queue, session)
    for (const tile of result.fresh) this.connected.add(tile)
    result.restoreFocus()
  }

  release(): void {
    for (const tile of this.connected) releaseArtifactTile(tile)
    this.connected.clear()
  }
}

/** What decides whether an entry's tiles can be kept: each picture's identity and state. */
export function artifactsKey(artifacts: readonly ArtifactRef[] | undefined, now = Math.floor(Date.now() / 1000)): string {
  if (!artifacts || !artifacts.length) return ""
  return artifacts
    .map((a) =>
      [a.id, a.state ?? "", a.byte_count, a.width, a.height, a.expires_at, a.expires_at <= now ? "x" : ""].join(":"),
    )
    .join("|")
}
