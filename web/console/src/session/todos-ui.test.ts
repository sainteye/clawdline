import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const source = readFileSync(new URL("./Todos.tsx", import.meta.url), "utf8")

test("fleet refreshes do not clear and reload the same Session todos", () => {
  assert.match(source, /const rowID = row\?\.id \?\? ""/)
  assert.match(source, /const load = useCallback[\s\S]*?readSessionWorkV2\(rowID\)[\s\S]*?}, \[rowID\]\)/)
  assert.doesNotMatch(source, /}, \[row\]\)/)
  assert.doesNotMatch(source, /useEffect\(\(\) => \{ if \(open\) void load\(\) \}/)
})

test("opening an answered todo fold explicitly refreshes it once", () => {
  assert.match(source, /if \(next && page !== null\) void load\(\)/)
})
