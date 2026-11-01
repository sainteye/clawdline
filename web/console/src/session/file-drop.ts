/** Snapshot browser-owned drag data synchronously; it expires after the drop event. */
export function droppedFiles(data: DataTransfer) {
  const files = Array.from(data.files)
  const directories = new Set(Array.from(data.items ?? []).flatMap(item => {
    const entry = item.webkitGetAsEntry?.()
    return entry?.isDirectory ? [entry.name] : []
  }))
  const pictures = files.filter(file => !directories.has(file.name) &&
    (/^image\//.test(file.type) || /\.(hei[cf]|jpe?g|png|gif|webp)$/i.test(file.name)))
  const references = files.filter(file => !pictures.includes(file))
  for (const name of directories) {
    if (!references.some(file => file.name === name)) references.push(new File([], name))
  }
  const uris = data.getData("text/uri-list").split(/\r?\n/).filter(line => line && !line.startsWith("#")).flatMap(line => {
    try {
      const url = new URL(line)
      return url.protocol === "file:" && (!url.host || url.host === "localhost") ? [decodeURIComponent(url.pathname)] : []
    } catch { return [] }
  })
  return { pictures, references, uris }
}

export type DroppedFiles = ReturnType<typeof droppedFiles>

/** Preserve spaces and metacharacters as a single shell path, without running it. */
export function quoteDroppedPath(path: string): string {
  return /^[\w/.:\\-]+$/u.test(path) ? path : "'" + path.replace(/'/g, "'\\''") + "'"
}

export function droppedReferences(drop: DroppedFiles, nativePaths: readonly string[] = []) {
  const available = nativePaths.length ? nativePaths : drop.uris
  // Native URLs may include directories which WebKit omitted from FileList.
  const pictureNames = new Set(drop.pictures.map(file => file.name))
  const paths = available.filter(path => !pictureNames.has(path.split(/[\\/]/).pop() ?? ""))
  let namesOnly = false
  if (!available.length) {
    for (const file of drop.references) {
      const path = (file as File & { path?: string }).path || file.webkitRelativePath || file.name
      paths.push(path)
      if (path === file.name) namesOnly = true
    }
  }
  return { text: paths.map(quoteDroppedPath).join(" "), namesOnly }
}

export async function nativeDroppedPaths(): Promise<string[]> {
  const bridge = (window as unknown as { webkit?: { messageHandlers?: {
    clawdlineDroppedPaths?: { postMessage(body: object): Promise<unknown> }
  } } }).webkit?.messageHandlers?.clawdlineDroppedPaths
  if (!bridge) return []
  try {
    const reply = await bridge.postMessage({})
    return Array.isArray(reply) && reply.every(path => typeof path === "string") ? reply : []
  } catch { return [] }
}
