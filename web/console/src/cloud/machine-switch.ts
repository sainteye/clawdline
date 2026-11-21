/**
 * Which machine this page reads, as far as anything that keeps state about
 * "the machine" needs to know: a Cloud console switches machines by pointing
 * its relay reader at another one (`install.ts`), and nothing else changes.
 *
 * On 2026-10-16 the page for a Linux machine on v1.0.7 said "v1.0.9 is
 * ready" — the Mac's own finished update, read before the switch and kept by
 * the update status, which asks again only every ten minutes — and so it
 * offered no way to update the Linux machine. A switch is now announced, and
 * the update status asks the new machine at once.
 *
 * It imports nothing, so node's test runner loads it alone.
 */
let current: (object & { readonly machine?: string }) | null = null
const listeners = new Set<() => void>()

/** Record the reader now in use; a different one from before is a switch. */
export function readingThrough(reader: object & { readonly machine?: string }): void {
  const switched = current !== null && current !== reader
  current = reader
  if (switched) for (const listener of listeners) listener()
}

/** The machine behind the Cloud reader, or null for the local console. */
export function currentCloudMachineID(): string | null {
  return current?.machine ?? null
}

export function onMachineChanged(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
