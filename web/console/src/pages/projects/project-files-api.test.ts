import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node strips source TypeScript in the browser-free test.
import { ProjectFileError, saveProjectFile } from "./project-files-api.ts"

test("an unanswered Cloud save stays uncertain so the editor rereads before confirming it", async () => {
  const previous = globalThis.fetch
  globalThis.fetch = async () => new Response(JSON.stringify({ error: "cloud_unanswered", outcome: "unknown" }), {
    status: 504, headers: { "Content-Type": "application/json" },
  })
  try {
    await assert.rejects(saveProjectFile("project", "file", "old", "new text", "save-1"), (error: unknown) =>
      error instanceof ProjectFileError && error.uncertain)
  } finally { globalThis.fetch = previous }
})
