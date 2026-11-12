// The vocabulary table (internal/domain/cloudvocab/vocabulary.json) against
// what this console carries: `node --test web/console/src/cloud/*.test.ts`.
//
// The Go half (internal/app/cloudops/vocabulary_test.go) drives every row
// through the machine's decode and route. This half holds the same rows to
// `writeRoute`: the seam a carried row names parses its console route as the
// word. `carry.ts`'s lists are generated from these rows (`carry-generated.ts`,
// `go run ./tools/contract-gen -check`), so they are not compared here again.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { writeRoute } from "./relay-writer.ts"
// @ts-expect-error -- Node executes source extensions.
import { assistantRelayRequest } from "./project-assistants.ts"

const here = dirname(fileURLToPath(import.meta.url))
const tablePath = resolve(here, "../../../../internal/domain/cloudvocab/vocabulary.json")

interface Row {
  op: string
  console: "carried" | "deferred" | "no_machine_route"
  console_route?: string
  console_seam?: "writer" | "reader" | "client"
  console_parsed_as?: string
  example: Record<string,unknown>
  shapes: {path:string;query?:string[]}[]
  local?: {method:string}
}

function rows(): Row[] {
  const table = JSON.parse(readFileSync(tablePath, "utf8")) as { version: number; operations: Row[] }
  assert.equal(table.version, 1)
  assert.ok(table.operations.length > 100, "the table is far shorter than the machine's vocabulary")
  return table.operations
}

/**
 * One concrete request for a carried row's console route, or null
 * for an entry that names no route (the copied client asks on its own).
 * Optional parts in `[…]` and the query are dropped; `{a|b}` takes `a`; every
 * other placeholder is "1", which every parser here takes as an id or index.
 */
function sample(route: string): { method: string; path: string } | null {
  const space = route.indexOf(" ")
  const method = route.slice(0, space)
  let path = route.slice(space + 1)
  if (!path.startsWith("/")) return null
  path = path.split(" ")[0].split("?")[0]
  path = path.replace(/\[[^\]]*\]/g, "").replace(/[[\]]/g, "")
  path = path.replace(/\{([^}|]+)\|[^}]*\}/g, "$1").replace(/\{[^}]+\}/g, "1")
  return { method, path }
}

test("every carried row's console route is parsed as its word", () => {
  for (const row of rows()) {
    if (row.console !== "carried") {
      assert.equal(row.console_route, undefined, `${row.op} is not carried and names a console route`)
      continue
    }
    const asked = sample(row.console_route!)
    if (row.console_seam === "client") {
      assert.equal(asked, null, `${row.op} says the copied client asks it, and names a route`)
      if (row.op.startsWith("project-assistants.")) {
        const example = row.example
        const lookup = (path:string) => path.split(".").reduce<unknown>((value,key) => (value as Record<string,unknown>)[key],example)
        let path = row.shapes[0].path.replace(/\{([^}]+)\}/g,(_match,field:string) => encodeURIComponent(String(lookup(field))))
        const command = example.command as {key:{machine_id:string};idempotency_key:string} | undefined
        const key = example.key as {machine_id:string} | undefined
        const query = new URLSearchParams((row.shapes[0].query ?? []).map(field => [field,String(field === "machine_id" ? key?.machine_id ?? example.machine_id : example[field])]))
        if (query.size) path += "?"+query
        const {session:_session,request:_request,...fields} = example
        const method = row.local!.method
        const parsed = assistantRelayRequest(path,{method,...(command ? {body:JSON.stringify(fields),headers:{"Idempotency-Key":command.idempotency_key}} : {})})
        assert.equal(parsed.word,row.op,`${row.op}: captured source parser`)
        assert.deepEqual(parsed.fields,fields,`${row.op}: complete request fields`)
        assert.equal(parsed.id,command?.idempotency_key ?? "",`${row.op}: stable command ref`)
      }
      continue
    }
    assert.ok(asked, `${row.op}: no route for a ${row.console_seam} word`)
    const parsed = writeRoute(asked.method, asked.path)
    if (row.console_seam === "writer") {
      assert.ok(parsed, `${row.op}: writeRoute does not parse ${asked.method} ${asked.path}`)
      assert.equal(parsed.word, row.console_parsed_as ?? row.op, `${row.op}: writeRoute's word for ${asked.method} ${asked.path}`)
    } else {
      assert.equal(row.console_seam, "reader", `${row.op}: console seam`)
      assert.equal(parsed, null, `${row.op} is the reader's and writeRoute parses ${asked.method} ${asked.path} as ${parsed?.word}`)
    }
  }
})
