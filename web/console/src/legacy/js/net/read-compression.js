// This framing is inside authenticated ciphertext. Old plaintext remains JSON.
export const READ_PLAINTEXT_BYTES_LIMIT = 8 * 1024 * 1024

export async function unpackReadPayload(value) {
  const bytes = value instanceof Uint8Array ? value : new Uint8Array(value)
  if (bytes[0] !== 103 || bytes[1] !== 122) return bytes
  if (bytes[2] !== 49 || bytes[3] !== 0) throw Object.assign(new Error("Unknown compressed read format"), { code: "read_encoding_unsupported" })
  if (typeof DecompressionStream !== "function") throw Object.assign(new Error("Compressed reads are unavailable"), { code: "read_compression_unavailable" })
  const reader = new Blob([bytes.slice(4)]).stream().pipeThrough(new DecompressionStream("gzip")).getReader()
  const chunks = []
  let length = 0
  try {
    for (;;) {
      const row = await reader.read()
      if (row.done) break
      length += row.value.length
      if (length > READ_PLAINTEXT_BYTES_LIMIT) throw Object.assign(new Error("Compressed read exceeds its byte budget"), { code: "read_payload_too_large" })
      chunks.push(row.value)
    }
  } catch (error) {
    await reader.cancel().catch(() => undefined)
    if (error?.code === "read_payload_too_large") throw error
    throw Object.assign(new Error("Compressed read is corrupt", { cause: error }), { code: "read_payload_corrupt" })
  }
  const result = new Uint8Array(length)
  let at = 0
  for (const chunk of chunks) { result.set(chunk, at); at += chunk.length }
  return result
}
