import { documentLocatorFromHash } from "../legacy/js/net/document-links.js"

/** A valid document address names the machine to open before a remembered choice. */
export function machineForAddress(hash: string, remembered: string | null): string | null {
  return documentLocatorFromHash(hash)?.machine ?? remembered
}
