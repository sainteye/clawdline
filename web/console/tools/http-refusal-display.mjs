// These producer examples use angle-bracket placeholders in commands and JSON.
// The browser displays text only, but the catalog rejects HTML-looking values.
const commandExampleKeys = new Set([
  "http.32a3b0c1ae85abed",
  "http.3ad9765394f45b04",
  "http.b83800283d88b391",
  "http.bb96b476b96bba73",
  "http.dd9496ebcab9dfee",
  "http.df99d615a86cecb5",
  "http.9775bdba5ed93c48",
  "http.e4624297f324376b",
])

export const displayRefusal = (key, value) => {
  if (typeof value !== "string") throw new Error(`${key}: expected a string`)
  if (!commandExampleKeys.has(key)) return value
  const result = value.replace(/<([^<>]+)>/gu, "‹$1›")
  if (/[<>]/u.test(result)) throw new Error(`${key}: unpaired angle bracket`)
  return result
}
