// The Devices page against a fake account and a fake daemon:
// `node --test web/console/src/legacy/devices-bridge.test.ts`.
//
// Nothing here touches a real account or a real daemon. The page under test is
// the copied module (`js/view/devices.js`), bound exactly as the console binds
// it, so what is being checked is the binding: which list the cards come from,
// and whether the sentence under the heading is true of that list.
//
// The document is a small stand-in rather than a DOM library — the console has
// none, and `bindDevicesPage` touches seven element fields. A stand-in that
// answered more than the page asks would be testing something else.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts. It sits on one line because the directive answers for the line the path is on.
import { DEVICES_ELEMENT_IDS, bindDevices, devicesLede, forgetLocalPlatform, localMachineKind, localMachines, offerPairing, type MachineAnswer, setAccountMachines, setMachinePairing } from "./devices-bridge.ts"
// @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts.
import { nextWord } from "../next-strings.ts"

/** One element, with the fields `view/devices.js` reads and writes. */
class Node {
  textContent = ""
  hidden = false
  className = ""
  title = ""
  type = ""
  dataset: Record<string, string> = {}
  onclick: (() => void) | null = null
  children: Node[] = []
  tag: string
  ownerDocument: Doc
  constructor(tag: string, ownerDocument: Doc) {
    this.tag = tag
    this.ownerDocument = ownerDocument
  }
  get firstChild(): Node | null {
    return this.children[0] ?? null
  }
  appendChild(child: Node): Node {
    this.children.push(child)
    return child
  }
  removeChild(child: Node): Node {
    this.children = this.children.filter((c) => c !== child)
    return child
  }
  /** Every descendant with this class, in document order. */
  all(className: string): Node[] {
    const found: Node[] = this.className === className ? [this] : []
    for (const child of this.children) found.push(...child.all(className))
    return found
  }
}

/** The page's seven elements by id, and nothing else. */
class Doc {
  readonly byId = new Map<string, Node>()
  constructor() {
    for (const id of DEVICES_ELEMENT_IDS) this.byId.set(id, new Node("div", this))
  }
  getElementById(id: string): Node | null {
    return this.byId.get(id) ?? null
  }
  createElement(tag: string): Node {
    return new Node(tag, this)
  }
  node(id: string): Node {
    const found = this.byId.get(id)
    if (!found) throw new Error("no element " + id)
    return found
  }
}

/** Two machines on an account, as `CloudClient.machines()` lists them. */
const ACCOUNT: MachineAnswer = {
  machines: [
    {
      id: "machine-one",
      name: "Studio",
      label: "Mac · Studio",
      kind: "mac",
      observedAt: 1,
      freshness: "current",
      pairing: "paired",
      sessions: 2,
      selectable: true,
      autoSelectable: true,
    },
    {
      id: "machine-two",
      name: "host-in-a-datacentre",
      label: "Linux / AWS · host-in-a-datacentre",
      kind: "linux-aws",
      observedAt: 2,
      freshness: "stale",
      pairing: "paired",
      sessions: 0,
      selectable: true,
      autoSelectable: false,
    },
  ],
  syncing: false,
  retryAfterMs: 0,
}

/** A daemon that says what it runs on, and counts how often it was asked. */
function daemon(os: string | null) {
  const asked: string[] = []
  const read = ((url: string) => {
    asked.push(url)
    if (os === null) return Promise.resolve({ ok: false, status: 403, json: () => Promise.resolve({}) } as Response)
    return Promise.resolve({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ platform: { os, arch: "arm64", capabilities: [] } }),
    } as Response)
  }) as unknown as typeof fetch
  return { asked, read }
}

/** The words the console passes in, read as late as the console reads them. */
const WORDS = {
  thisMachine: () => nextWord("devicesThisMachine"),
  lede: () => nextWord(devicesLede()),
}

function bound(doc: Doc) {
  const started: string[] = []
  const page = bindDevices(doc as unknown as Document, (machine: string) => started.push(machine), WORDS)
  return { page, started }
}

test.afterEach(() => {
  setAccountMachines(null)
  setMachinePairing(null)
  forgetLocalPlatform()
})

test("an account with two machines draws two cards, not one", async () => {
  setAccountMachines(() => Promise.resolve(ACCOUNT))
  const doc = new Doc()
  const { page } = bound(doc)
  page.enter()
  await page.load()

  const cards = doc.node("devices-rows").all("device-card")
  assert.equal(cards.length, 2, "one card per machine on the account")
  const names = cards.map((card) => card.all("device-card-heading")[0].children[0].textContent)
  assert.deepEqual(names, ["Mac · Studio", "Linux / AWS · host-in-a-datacentre"])
  assert.equal(doc.node("devices-empty").hidden, true)
})

test("with an account's list under it, the heading says it is the account's", async () => {
  setAccountMachines(() => Promise.resolve(ACCOUNT))
  const doc = new Doc()
  const { page } = bound(doc)
  page.enter()
  await page.load()

  assert.equal(devicesLede(), "devicesLedeAccount")
  assert.equal(doc.node("devices-lede").textContent, nextWord("devicesLedeAccount"))
})

test("with no account list, the page says it is showing one machine and does not claim the account's", async () => {
  const doc = new Doc()
  const { page } = bound(doc)
  page.enter()
  await page.load()

  assert.equal(devicesLede(), "devicesLedeThisMachine")
  const said = doc.node("devices-lede").textContent
  assert.equal(said, nextWord("devicesLedeThisMachine"))
  assert.ok(!said.includes("account,"), "it does not promise a list of the account's machines")
  assert.equal(doc.node("devices-rows").all("device-card").length, 1)
})

test("a Linux daemon's own machine is not called a Mac", async () => {
  const { asked, read } = daemon("linux")
  const answer = await localMachines(nextWord("devicesThisMachine"), read)

  assert.deepEqual(asked, ["/v1/diagnostics"])
  assert.equal(answer.machines.length, 1)
  const machine = answer.machines[0] as Record<string, unknown>
  assert.equal(machine.platform, "linux")
  assert.equal(machine.kind, "linux")
  assert.equal(machine.label, "Linux · " + nextWord("devicesThisMachine"))
  assert.ok(!String(machine.label).includes("Mac"))
})

test("a Mac is still called one, and the daemon is asked once", async () => {
  const { asked, read } = daemon("darwin")
  const first = await localMachines(nextWord("devicesThisMachine"), read)
  const again = await localMachines(nextWord("devicesThisMachine"), read)

  assert.equal(asked.length, 1, "the platform is read once per page, not once per draw")
  assert.equal((first.machines[0] as Record<string, unknown>).label, "Mac · This Mac")
  assert.equal((again.machines[0] as Record<string, unknown>).kind, "mac")
})

test("a daemon that will not say what it is running on is not guessed at", async () => {
  const { read } = daemon(null)
  const answer = await localMachines(nextWord("devicesThisMachine"), read)

  const machine = answer.machines[0] as Record<string, unknown>
  assert.equal(machine.platform, null)
  assert.equal(machine.kind, "unknown")
  assert.equal(machine.label, nextWord("devicesThisMachine"), "no platform word in front of it")
})

test("the platform words are the ones `machinePresentation` uses", () => {
  assert.equal(localMachineKind("darwin"), "mac")
  assert.equal(localMachineKind("linux"), "linux")
  assert.equal(localMachineKind("windows"), "unknown")
  assert.equal(localMachineKind(null), "unknown")
})

test("a source that cannot answer leaves the page empty rather than showing a machine that is not there", async () => {
  setAccountMachines(() => Promise.reject(new Error("no line")))
  const doc = new Doc()
  const { page } = bound(doc)
  page.enter()
  await page.load()

  assert.equal(doc.node("devices-rows").all("device-card").length, 0)
  assert.equal(doc.node("devices-empty").hidden, false)
})

/** The account with a third machine on it that this browser was never paired with. */
const WITH_UNPAIRED: MachineAnswer = {
  ...ACCOUNT,
  machines: [
    ...ACCOUNT.machines,
    {
      id: "machine-three-51463f04",
      name: "build-box",
      label: "Linux · build-box",
      kind: "linux",
      observedAt: 3,
      freshness: "current",
      pairing: "not_paired",
      // What the gate passes for a machine whose sessions it cannot read: no count at all.
      selectable: false,
      autoSelectable: false,
    },
  ],
}

const PAIR_WORDS = {
  pair: () => nextWord("cloudPair"),
  pairOne: (machine: string) => nextWord("cloudPairOne", { machine }),
  help: () => nextWord("devicesPairHelp"),
}

test("a card for a machine this browser is not paired with gets a Pair button that names it", async () => {
  setAccountMachines(() => Promise.resolve(WITH_UNPAIRED))
  const asked: string[] = []
  setMachinePairing((machine: string) => asked.push(machine))
  const doc = new Doc()
  const { page } = bound(doc)
  page.enter()
  await page.load()

  const rows = doc.node("devices-rows")
  assert.equal(offerPairing(rows, PAIR_WORDS), 1, "one card, the unpaired one")
  const card = rows.children[2]
  const pair = card.all("device-start device-pair")
  assert.equal(pair.length, 1)
  assert.equal(pair[0].textContent, nextWord("cloudPair"))
  assert.equal(pair[0].title, nextWord("cloudPairOne", { machine: "Linux · build-box" }))
  pair[0].onclick?.()
  assert.deepEqual(asked, ["machine-three-51463f04"], "the full id the copied card carries, not its short form")

  // The copied sentence sends a person to "Pair a Browser" on the machine, a
  // control a Linux daemon does not have; with a button on the card, the card
  // says what the button does instead.
  assert.equal(card.all("device-help")[0].textContent, nextWord("devicesPairHelp"))
  for (const paired of rows.children.slice(0, 2)) assert.equal(paired.all("device-start device-pair").length, 0)

  assert.equal(offerPairing(rows, PAIR_WORDS), 0, "a card already carrying the button is left alone")
  assert.equal(card.all("device-start device-pair").length, 1)
})

test("a machine whose sessions cannot be read is not drawn as having none", async () => {
  setAccountMachines(() => Promise.resolve(WITH_UNPAIRED))
  const doc = new Doc()
  const { page } = bound(doc)
  page.enter()
  await page.load()

  const facts = (card: Node) => card.all("device-facts")[0].children.map((fact) => fact.textContent)
  const rows = doc.node("devices-rows")
  assert.ok(facts(rows.children[1]).some((text) => /^0 /.test(text)), "a paired machine with no sessions says 0")
  assert.ok(!facts(rows.children[2]).some((text) => /^\d+ /.test(text)), "the unpaired one says no number")
})

test("with nothing that can pair, the card keeps the copied sentence and gets no button", async () => {
  setAccountMachines(() => Promise.resolve(WITH_UNPAIRED))
  const doc = new Doc()
  const { page } = bound(doc)
  page.enter()
  await page.load()

  const rows = doc.node("devices-rows")
  assert.equal(offerPairing(rows, PAIR_WORDS), 0)
  assert.equal(rows.children[2].all("device-start device-pair").length, 0)
  assert.notEqual(rows.children[2].all("device-help")[0].textContent, nextWord("devicesPairHelp"))
})
