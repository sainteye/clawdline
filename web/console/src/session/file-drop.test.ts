import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's type-strip runner uses the source extension.
import { droppedFiles, droppedReferences, quoteDroppedPath } from "./file-drop.ts"

function drag(files: File[], uris = "", directories: string[] = []): DataTransfer {
  return { files, items: directories.map(name => ({ webkitGetAsEntry: () => ({ name, isDirectory: true }) })),
    getData: (type: string) => type === "text/uri-list" ? uris : "" } as unknown as DataTransfer
}

test("documents and directories insert their native paths while mixed pictures stay attachments", () => {
  const picture = new File(["png"], "shot.png", { type: "image/png" })
  const document = new File(["notes"], "notes.txt")
  const drop = droppedFiles(drag([document, picture], "", ["project.png"]))
  assert.deepEqual(drop.pictures, [picture])
  assert.deepEqual(droppedReferences(drop, ["/tmp/notes.txt", "/tmp/shot.png", "/tmp/project.png"]),
    { text: "/tmp/notes.txt /tmp/project.png", namesOnly: false })
})

test("directories named like pictures are references, even when omitted from FileList", () => {
  const drop = droppedFiles(drag([new File([], "folder.png")], "", ["folder.png", "empty"]))
  assert.deepEqual(drop.pictures, [])
  assert.deepEqual(droppedReferences(drop), { text: "folder.png empty", namesOnly: true })
})

test("browser file URIs decode paths without treating web links as local files", () => {
  const drop = droppedFiles(drag([new File([], "notes.txt")], "# comment\r\nfile:///tmp/my%20notes.txt\r\nhttps://example.com/notes\nfile://other-host/no\nfile:///bad%ZZ"))
  assert.deepEqual(droppedReferences(drop), { text: "'/tmp/my notes.txt'", namesOnly: false })
})

test("browser names are identified as names and never prefixed with a guessed project path", () => {
  const drop = droppedFiles(drag([new File([], "notes.txt")]))
  assert.deepEqual(droppedReferences(drop), { text: "notes.txt", namesOnly: true })
  Object.defineProperty(drop.references[0], "webkitRelativePath", { value: "project/notes.txt" })
  assert.deepEqual(droppedReferences(drop), { text: "project/notes.txt", namesOnly: false })
})

test("spaces, quotes, newlines and shell substitutions remain literal paths", () => {
  assert.equal(quoteDroppedPath("/tmp/it's $(danger); a\nfile"), "'/tmp/it'\\''s $(danger); a\nfile'")
  assert.equal(quoteDroppedPath("/tmp/正常檔案.txt"), "'/tmp/正常檔案.txt'")
  assert.equal(quoteDroppedPath("/tmp/plain.txt"), "/tmp/plain.txt")
})
