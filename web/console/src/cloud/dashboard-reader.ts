// @ts-expect-error -- Node's strip-types runner needs source extensions.
import { RelayReader, type CloudReadClient } from "./relay-reader.ts"
// @ts-expect-error -- Node's strip-types runner needs source extensions.
import { CARRY_TABLE } from "./carry.ts"
// @ts-expect-error -- Node's strip-types runner needs source extensions.
import { RelayWriter, writeRoute } from "./relay-writer.ts"

/** A pinned read for inspection. Never install it as the console's active reader. */
export function dashboardFetch(machine: string, connected: () => CloudReadClient | null): typeof fetch {
  const reader = new RelayReader(machine, { carry: CARRY_TABLE })
  const words = new RelayWriter(reader.writeHost)
  reader.carryWrites({ route: (method, path) => {
    const route = writeRoute(method, path)
    return route?.op === "machine-usage" ? route : null
  }, answer: (route, method, url, init) => words.answer(route, method, url, init) })
  return (input, init) => {
    const client = connected()
    if (client) reader.attach(client)
    return reader.fetch(input, init)
  }
}
