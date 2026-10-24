import { test } from "node:test"
import assert from "node:assert/strict"
import { registerHooks } from "node:module"

// Run the actual core client under Node, resolving its browser build suffixes.
registerHooks({ resolve(specifier, context, next) {
  if (context.parentURL?.includes("/core/src/") && specifier.startsWith("./") && specifier.endsWith(".js")) {
    return next(specifier.slice(0, -3) + ".ts", context)
  }
  return next(specifier, context)
} })
// @ts-expect-error -- Node executes the TypeScript source.
const { ClawdlineClient } = await import("./client.ts")

test("native transcript cursors carry their held revision on both read directions", async () => {
  const urls: URL[] = []
  const client = new ClawdlineClient({ baseUrl: "http://fixture", fetch: async (input: string | URL | Request) => {
    urls.push(new URL(String(input)))
    return Response.json({ entries: [], signature: "new", evidence: "transcript" })
  } })
  const revision = "agy1:synthetic:100:abc&def"
  await client.transcript("conversation-a", 20, 50, revision)
  await client.transcriptAfter("conversation-a", 20, 100, revision)
  await client.transcript("conversation-b")
  assert.deepEqual(urls.map((url) => [url.searchParams.get("session"), url.searchParams.get("before"),
    url.searchParams.get("after"), url.searchParams.get("revision")]), [
    ["conversation-a", "50", null, revision], ["conversation-a", null, "100", revision],
    ["conversation-b", null, null, null],
  ])
})
