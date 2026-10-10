// The guard that keeps console tests from asserting on the shape of the code
// has to be able to go red. It is given its own tiny console: one test file
// that transcribes an implementation, and the same file with the transcription
// replaced by something a person would notice.
import { test } from "node:test"
import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const guard = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..", "tools", "check-mirror-tests.mjs")

function consoleWith(body) {
  const root = mkdtempSync(join(tmpdir(), "mirror-guard-"))
  const src = join(root, "web", "console", "src")
  mkdirSync(src, { recursive: true })
  writeFileSync(join(root, "web", "package.json"), JSON.stringify({ scripts: { check: "" } }))
  writeFileSync(join(src, "sample.test.ts"),
    `import { readFileSync } from "node:fs"\nconst source = readFileSync(new URL("./Sample.tsx", import.meta.url), "utf8")\ntest("sample", () => {\n${body}\n})\n`)
  return root
}
const run = (root) => spawnSync(process.execPath, [guard, "--web", join(root, "web")], { encoding: "utf8" })

test("a transcription of the implementation is refused, naming what it read", () => {
  const root = consoleWith('  assert.match(source, /const \\[open, setOpen\\] = useState\\(false\\)/)')
  try {
    const result = run(root)
    assert.equal(result.status, 1, result.stdout + result.stderr)
    assert.match(result.stderr, /asserts on a declaration/)
  } finally { rmSync(root, { recursive: true, force: true }) }
})

test("an absence with no stated reason is refused; one with a reason is not", () => {
  const bare = consoleWith('  assert.doesNotMatch(source, /work-persona-ai/)')
  const explained = consoleWith('  assert.doesNotMatch(source, /work-persona-ai/, "an older daemon refuses the field")')
  try {
    assert.equal(run(bare).status, 1)
    assert.equal(run(explained).status, 0)
  } finally {
    rmSync(bare, { recursive: true, force: true })
    rmSync(explained, { recursive: true, force: true })
  }
})

test("a catalog lookup, an ARIA attribute and a said() sentence are left alone", () => {
  const root = consoleWith([
    '  assert.match(source, /workWord\\("reviewRequiredHint"\\)/)',
    '  assert.match(source, /aria-haspopup="dialog"/)',
    '  assert.match(source, pattern`${said("這個設定只影響這台機器")}`)',
  ].join("\n"))
  try {
    const result = run(root)
    assert.equal(result.status, 0, result.stdout + result.stderr)
  } finally { rmSync(root, { recursive: true, force: true }) }
})

test("a bracket inside a regular expression does not run the call's end into the next test", () => {
  // The first version of this guard counted `\(` as structure, so the span it
  // reported ended inside the following declaration.
  const root = consoleWith([
    '  assert.match(source, /\\{featureLike\\(\\{ kind \\}\\) && <ReviewRequiredField id="work-new-review-required"/)',
    '  assert.match(source, /workWord\\("reviewRequiredLabel"\\)/)',
  ].join("\n"))
  try {
    const result = run(root)
    assert.equal(result.status, 1, result.stdout + result.stderr)
    assert.match(result.stderr, /sample\.test\.ts:4/)      // the first assertion, not a later line
    assert.doesNotMatch(result.stderr, /reviewRequiredLabel/)
  } finally { rmSync(root, { recursive: true, force: true }) }
})
