// The composer's pictures: `js/input/shots.js`, ported.
//
// The copy beside this file is the Swift app's module byte for byte, kept under
// the guard as the text this one follows. It cannot be imported: it binds its
// listeners through `core/dom.js`, which looks every id up once at import —
// before React has drawn `#shots`, `#attach` or `#pick` — and it redraws the
// composer by calling the page's own `renderComposer`. So the list, its bounds,
// the shrinking and the markup are here, function for function, and
// `session/Composer.tsx` binds the listeners the original binds at the bottom
// of that file.
//
// What is different, on purpose:
// - `draw()` does not write `#shots`; it tells the composer, which renders the
//   same markup (`shotsHTML`). The picture URL is escaped there, which changes
//   nothing for a `data:` URL a canvas or a FileReader wrote.
// - `toast` is passed in, because the toast lives in `overlays/`, which
//   imports this directory.
// - The send is `net/live.js`'s `send` with pictures, spelled against this
//   daemon; a send with no pictures still goes through the shared client.
import { RefusalError, TransportError } from "@clawdline/core"
import { T, fill } from "./js/core/i18n.js"
import { esc } from "./js/core/esc.js"

const LONG_EDGE = 1600
const QUALITY = 0.82
const MAX_COUNT = 6
// Measured on the wire, which is what the limit is about: the lengths of the
// `data:` strings, base64 and all. The server refuses a body over 20 MB.
const MAX_EACH = 5 << 20
const MAX_TOTAL = 15 << 20

interface Shot {
  id: number
  url: string
  name: string
}

let list: Shot[] = []
let seq = 0
let busy = 0
let version = 0
const listeners = new Set<() => void>()

/** `draw()`: the composer is told, and renders `shotsHTML` and its buttons again. */
function draw(): void {
  version += 1
  for (const listener of listeners) listener()
}

export function subscribeShots(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** A number that changes whenever the list or its busy count does. */
export function shotsVersion(): number {
  return version
}

function total(): number {
  return list.reduce((sum, shot) => sum + shot.url.length, 0)
}

/** A bitmap from a file, however this browser is willing to give one. */
function decode(file: File): Promise<ImageBitmap | HTMLImageElement> {
  if (typeof window.createImageBitmap === "function") {
    // `from-image`, so a photograph taken sideways is not drawn sideways.
    return createImageBitmap(file, { imageOrientation: "from-image" }).catch(() => createImageBitmap(file))
  }
  return new Promise((done, fail) => {
    const url = URL.createObjectURL(file)
    const img = new Image()
    img.onload = () => {
      URL.revokeObjectURL(url)
      done(img)
    }
    img.onerror = () => {
      URL.revokeObjectURL(url)
      fail(new Error("not a picture"))
    }
    img.src = url
  })
}

function asDataURL(file: File): Promise<string> {
  return new Promise((done, fail) => {
    const reader = new FileReader()
    reader.onload = () => done(String(reader.result))
    reader.onerror = () => fail(new Error("could not be read"))
    reader.readAsDataURL(file)
  })
}

/**
 * Shrunk to a long edge of 1600px: a phone photograph is megabytes, and that is
 * more than Claude Code can use. A screenshot stays PNG unless that is too big,
 * because a screenshot is text and JPEG is the wrong thing to do to text.
 */
function shrink(file: File): Promise<{ url: string; w: number; h: number }> {
  return decode(file)
    .then((bitmap) => {
      const scale = Math.min(1, LONG_EDGE / Math.max(bitmap.width, bitmap.height))
      const canvas = document.createElement("canvas")
      canvas.width = Math.max(1, Math.round(bitmap.width * scale))
      canvas.height = Math.max(1, Math.round(bitmap.height * scale))
      canvas.getContext("2d")!.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
      if ("close" in bitmap && typeof bitmap.close === "function") bitmap.close()
      let url = file.type === "image/png" ? canvas.toDataURL("image/png") : ""
      if (!url || url.length > MAX_EACH) url = canvas.toDataURL("image/jpeg", QUALITY)
      return { url, w: canvas.width, h: canvas.height }
    })
    .catch(() =>
      // The browser cannot decode it — a HEIC, in everything but Safari. The
      // bytes still go: the server re-encodes whatever it is given.
      asDataURL(file).then((url) => ({ url, w: 0, h: 0 })),
    )
}

/** The markup `draw()` writes into `#shots`. */
export function shotsHTML(): string {
  return list
    .map(
      (shot) =>
        '<div class="shot"><img src="' +
        esc(shot.url) +
        '" alt="' +
        esc(shot.name) +
        '">' +
        '<button type="button" class="drop" data-shot="' +
        shot.id +
        '" aria-label="' +
        esc(fill(T.webRemoveShot, { name: shot.name })) +
        '">×</button></div>',
    )
    .join("")
}

export type Say = (text: string, bad: boolean) => void

export const Shots = {
  count: (): number => list.length,
  busy: (): boolean => busy > 0,
  urls: (): string[] => list.map((shot) => shot.url),
  clear(): void {
    if (!list.length) return
    list = []
    draw()
  },
  /** The `#shots` click handler's half: the picture with this `data-shot` goes. */
  remove(id: string): void {
    list = list.filter((shot) => String(shot.id) !== id)
    draw()
  },

  add(files: ArrayLike<File> | null | undefined, toast: Say): void {
    let wanted = Array.prototype.filter.call(files || [], isPicture) as File[]
    if (!wanted.length) {
      toast(T.webShotsOnlyPictures, true)
      return
    }
    if (list.length + wanted.length > MAX_COUNT) {
      toast(fill(T.webShotsTooMany, { n: MAX_COUNT }), true)
      wanted = wanted.slice(0, Math.max(0, MAX_COUNT - list.length))
      if (!wanted.length) return
    }
    busy += wanted.length
    draw()
    for (const file of wanted) {
      shrink(file)
        .then((shot) => {
          if (shot.url.length > MAX_EACH) {
            toast(T.webShotTooBig, true)
            return
          }
          if (total() + shot.url.length > MAX_TOTAL) {
            toast(T.webShotsTooBig, true)
            return
          }
          list.push({ id: ++seq, url: shot.url, name: file.name || "picture" })
        })
        .catch(() => {
          toast(T.webShotUnreadable, true)
        })
        .then(() => {
          busy -= 1
          draw()
        })
    }
  },
}

/**
 * A file the attachment list would take. A HEIC sometimes arrives with an
 * empty type, so the extension gets a say.
 */
export function isPicture(file: File): boolean {
  return /^image\//.test(file.type) || /\.(hei[cf]|jpe?g|png|gif|webp)$/i.test(file.name || "")
}

export function carriesPicture(data: DataTransfer | null | undefined): boolean {
  const files = data && data.files
  return !!files && Array.prototype.some.call(files, isPicture)
}

/** Whether a drag is carrying files at all (`dropPictures`'s `carriesFiles`). */
export function carriesFiles(dt: DataTransfer | null | undefined): boolean {
  if (!dt) return false
  if (dt.types && Array.prototype.indexOf.call(dt.types, "Files") >= 0) return true
  return !!(dt.files && dt.files.length)
}

/**
 * `api.send` with pictures: `{text?, images}` to `/v1/sessions/:id/send`. A
 * refusal comes back as the shared client's RefusalError, whichever of this
 * daemon's two refusal shapes it arrived in, so the composer reads one kind.
 */
export async function sendWithPictures(id: string, text: string, images: string[]): Promise<void> {
  const body: { text?: string; images?: string[] } = {}
  if (text) body.text = text
  if (images.length) body.images = images
  const path = "/v1/sessions/" + encodeURIComponent(id) + "/send"
  let res: Response
  try {
    res = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
  } catch (cause) {
    throw new TransportError(`POST ${path} did not complete`, cause)
  }
  if (res.ok) return
  let parsed: unknown = null
  try {
    parsed = await res.json()
  } catch {
    /* no body worth reading */
  }
  const error = (parsed as { error?: unknown } | null)?.error
  if (typeof error === "string") {
    throw new RefusalError(res.status, { error, detail: String((parsed as { detail?: unknown }).detail ?? "") }, path)
  }
  if (error && typeof error === "object" && typeof (error as { code?: unknown }).code === "string") {
    const e = error as { code: string; message?: unknown }
    throw new RefusalError(res.status, { error: e.code, detail: String(e.message ?? "") }, path)
  }
  throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
}
