import assert from "node:assert/strict"
import test from "node:test"
// @ts-expect-error -- Node's strip-types runner reads the source extension.
import { projectFolderPath, validProjectFolderName } from "./project-folder-name.ts"

test("a folder preview uses the selected machine's Unix or Windows root", () => {
  assert.equal(projectFolderPath("/projects", "新專案"), "/projects/新專案")
  assert.equal(projectFolderPath("/", "sample"), "/sample")
  assert.equal(projectFolderPath("C:\\", "sample"), "C:\\sample")
  assert.equal(projectFolderPath("C:\\projects", "sample"), "C:\\projects\\sample")
  assert.equal(projectFolderPath("\\\\host\\share\\", "sample"), "\\\\host\\share\\sample")
})

test("the name identifies one child and preserves legitimate spaces", () => {
  for (const name of ["", "  ", ".", "..", "../escape", "a/b", "a\\b", "/tmp", "bad\0name", "bad\nname", "bad\u0085name"]) {
    assert.equal(validProjectFolderName(name), false, JSON.stringify(name))
  }
  for (const name of ["my-project", "新專案", "two words", " name "]) assert.equal(validProjectFolderName(name), true)
})
