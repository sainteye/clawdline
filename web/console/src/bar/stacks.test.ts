import { test } from "node:test"
import assert from "node:assert/strict"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

async function stacks() {
  const { outputFiles } = await build({ stdin: { contents: 'export {stackStateSaid,stackTip} from "./stacks"', resolveDir: fileURLToPath(new URL(".", import.meta.url)), loader: "ts" },
    bundle: true, platform: "node", format: "cjs", packages: "external", write: false })
  const compiled = { exports: {} as typeof import("./stacks") }
  new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
  return compiled.exports
}

test("a process with only an address is not part of the up/total count", async () => {
  const { stackStateSaid } = await stacks()
  const stack = { name: "app", root: "/p", state: "running", commands: [], processes: [
    { name: "api", port: 8002, state: "running" },
    { name: "tunnel", url: "https://example.dev", state: "stopped" },
  ] }
  assert.equal(stackStateSaid(stack as never).text, "▮ 1/1")
})
