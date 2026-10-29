// This subclass accepts the new content-free ss/ channel without changing the
// copied Cloud client or weakening its validation for any existing channel.
import { imageAnswerBytes } from "../legacy/js/net/cloud-client.js"
import { CatalogCloudClient } from "./refusal-client.js"
import { SESSION_INVENTORY_ID, SESSION_INVENTORY_LIMIT, channelSegment, decodedChannelSegment } from "../legacy/js/net/client.js"
import { base64Bytes, bytesBase64, envelopeSigningBytes, importMasterSecret, openEnvelope, sealEnvelope, validateEnvelope } from "../legacy/js/net/cloud-crypto.js"
import { classifyViewerError, viewerHealthBegin, viewerHealthFailed } from "../legacy/js/net/viewer-health-hook.js"
import { statusInventoryIDs } from "./status-projection.ts"

const decoder = new TextDecoder()
const STATUS = /^ss\/([^/]+)\/([^/]+)$/u
const GENERATION = /^[0-9a-f]{32}$/u
/**
 * The read word a carrier offer is published and answered as.
 *
 * It is not one of the machine's routed words: the offer leaves as a signed `termi` request,
 * which the machine answers from its terminal lane rather than its read queue, and the only
 * reason it has a read word at all is that the answer is an ordinary read answer and the whole
 * waiter, timeout and settle machinery then works unchanged. `_machineImplements` below is what
 * decides whether a machine has it, from the one flag its descriptor carries.
 */
const CARRIER_WORD = "carrier-offer"
const CARRIER_SESSION = "__clawdline_machine__"
/** The machine's whole-list channel, which the carrier never carries. */
/** A carrier either negotiates in a few seconds or is not available on this network. */
const CARRIER_TIMEOUT_MS = 15_000
/** A cold list must yield promptly when the relay or direct carrier stops answering. */
const PINNED_LIST_READ_TIMEOUT_MS = 10_000
/** A retained marker normally replays at once; silence must not block the direct-list fallback. */
const STATUS_MARKER_RECOVERY_TIMEOUT_MS = 5_000
/** Whisper may load a model before it can answer the first recording. */
const VOICE_TIMEOUT_MS = 6 * 60 * 1000
// Total encoded envelopes held before authentication, across this viewer's machines.
// Byte and row budgets are registered as console.carrier_receive_*.
export const CARRIER_RECEIVE_BYTES = 33554432
export const CARRIER_RECEIVE_ROWS = 32

function refused(code, message) {
  return Object.assign(new Error(message), { code })
}

function pinnedRead(type, body) {
  return (type === "sessions.list" && typeof body?.request === "string" ||
    type === "info" && ["full", "remote-permission"].includes(body?.parts) ||
    ["transcript", "skills", "image", "git", "git-diff", "outputs", "output-file", "screen", "agent", "shell", "documents", "document", "video", "peer-inbox"].includes(type)) &&
    (body?.expected_generation !== undefined || type === "sessions.list")
}

function pinnedReplyKey(machine, session, read) {
  return machine + "\u0000" + session + "\u0000" + read
}

const COMPRESSED_READ_WORDS = new Set(["work.v2.cards", "work.v2.items", "work.v2.item", "work.v2.search", "work.v2.proposals", "work.v2.session-todos", "work.v2.human-interventions", "personas", "projects", "project-folders", "schedules", "snippets", "provider-quotas", "capacity", "skills", "info", "sessions.list"])
const READ_COMPRESSION_SUFFIX = ".encoding.gzip-v1"
function pinnedReadName(type, body) {
 const name = basePinnedReadName(type,body)
 return name && body?.accept_encoding === "gzip-v1" ? name + READ_COMPRESSION_SUFFIX : name
}

function basePinnedReadName(type, body) {
  if (type === "sessions.list" && typeof body?.request === "string" && body.request) return "read:" + body.request
  if (type === "info" && ["full", "remote-permission"].includes(body?.parts)) return "info." + body.parts
  if (type === "skills") return "skills"
  if (type === "git" || type === "screen" || type === "documents" || type === "outputs") return type
  if ((type === "output-file" || type === "git-diff" || type === "document" || type === "video" || type === "peer-inbox") &&
    typeof body?.request === "string" && body.request) return "read:" + body.request
  if (type === "agent" && typeof body?.agent === "string" && body.agent) {
    if (body.before === undefined) return "agent:" + body.agent
    return Number.isSafeInteger(body.before) && body.before > 0
      ? "agent:" + body.agent + ".before." + body.before : null
  }
  if (type === "shell" && typeof body?.shell === "string" && body.shell) return "shell:" + body.shell
  if (type === "image" && typeof body?.id === "string" && body.id) return "image." + body.id + (body.size === "thumb" ? ".thumb" : "")
  if (type === "transcript") {
    if (body?.before === undefined) return "transcript" + (body.view ? ".view." + body.view : "")
    return Number.isSafeInteger(body.before) && body.before > 0
      ? "transcript.before." + body.before + (body.revision ? ".revision." + body.revision : "") + (body.view ? ".view." + body.view : "") : null
  }
  if (type === "peer-inbox" && typeof body?.request === "string" && body.request) return "read:" + body.request
  return null
}

/** Apply one authenticated update to its exact held page before publishing any rows. */
export function applyTranscriptDelta(held, answer, session) {
  const changes = answer?.delta, page = answer?.page
  const stale = () => refused("transcript_cursor_stale", "The transcript update does not match its held page")
  if (!held || !Array.isArray(held.entries) || held.id !== session || answer?.baseVersion !== held.version ||
    !page || page.id !== session || !/^[0-9a-f]{64}$/u.test(page.version) ||
    typeof page.signature !== "string" || !["transcript", "structured"].includes(page.evidence) ||
    !Array.isArray(page.entries) || page.entries.length !== 0 || !Array.isArray(changes) ||
    !Number.isSafeInteger(answer.entryCount) || answer.entryCount < 0) throw stale()
  const entries = []
  let cursor = 0
  for (const change of changes) {
    if (!change || !Number.isSafeInteger(change.start) || change.start < cursor || change.start > held.entries.length ||
      !Number.isSafeInteger(change.deleteCount) || change.deleteCount < 0 || change.deleteCount > held.entries.length-change.start ||
      !Array.isArray(change.entries) || change.entries.some(row => !row || typeof row.role !== "string" || typeof row.text !== "string") ||
      entries.length+change.start-cursor+change.entries.length > answer.entryCount) throw stale()
    entries.push(...held.entries.slice(cursor,change.start),...change.entries)
    cursor = change.start+change.deleteCount
  }
  if (entries.length+held.entries.length-cursor !== answer.entryCount) throw stale()
  entries.push(...held.entries.slice(cursor))
  return { ...page, entries }
}

/** A retained or delayed t/ row must name the exact request before it can settle a pinned read. */
export function pinnedReplyMatches(payload, expected) {
  return !!payload && typeof payload === "object" && !Array.isArray(payload) &&
    payload.read === expected.read && payload.machine_id === expected.machineID &&
    payload.session_id === expected.sessionID &&
    (expected.generation === null ? payload.expected_generation === undefined :
      payload.expected_generation === expected.generation) &&
    Number.isSafeInteger(payload.seq) && payload.seq === expected.seq
}

/** Validate every envelope field with the frozen validator, then verify the original signed ss/ bytes. */
export function statusChannel(envelope) {
  const match = typeof envelope?.ch === "string" ? STATUS.exec(envelope.ch) : null
  if (!match) return null
  validateEnvelope({ ...envelope, ch: `s/${match[1]}/${match[2]}` })
  return { machine: decodedChannelSegment(match[1]), session: decodedChannelSegment(match[2]) }
}

export class StatusCloudClient extends CatalogCloudClient {
  // The archived client chooses the voice host. Record that choice when the
  // receiving machine says Whisper is missing, so a phone-only failure leaves
  // evidence on the paired Mac without retaining audio or recognized words.
  async voice(audio, rate) {
    const host = this._voiceHost()
    try {
      return await super.voice(audio, rate)
    } catch (error) {
      this._recordMissingWhisper(error, host)
      throw error
    }
  }

  /** Dictates on this Session's machine unless a person selected another host. */
  async voiceFor(machine, audio, rate) {
    let missing = null
    for (;;) {
      const host = this._sessionVoiceHost(machine)
      if (!host.machine) throw missing || host.error
      try {
        return await this._machineRequest(host.machine, "voice",
          { audio, rate }, "action", VOICE_TIMEOUT_MS)
      } catch (error) {
        this._recordMissingWhisper(error, host)
        if (error?.code !== "no_whisper") throw error
        // A command in the descriptor says that the daemon understands the
        // route, not that whisper-cli and its model are installed. Remember
        // this machine's authoritative refusal for this page and retry the
        // same recording on the next eligible host.
        if (!(this.voiceUnavailable instanceof Set)) this.voiceUnavailable = new Set()
        this.voiceUnavailable.add(host.machine)
        missing = error
      }
    }
  }

  _recordMissingWhisper(error, host) {
    if (error?.code !== "no_whisper" || !host.machine) return
    try {
      const reason = error.detail?.reason || error.reason || null
      this.viewerEvents.record("cloud.voice.whisper_missing", {
        target_machine: host.machine,
        chosen: host.chosen,
        candidates: host.candidates,
        reason,
        web_build: this.webBuild || null,
      }, ["cloud.voice.whisper_missing", host.machine, reason].join("|"))
      this._scheduleViewerEvents()
    } catch { /* diagnostics cannot change the voice result */ }
  }

  constructor(options) {
    super(options)
    // Retained rows from a previous connection may say "current" after that
    // connection has gone away. Wait for a fresh complete pass on this socket.
    this.statusSnapshots = new Map()
    this.statusSequences = new Map()
    this.statusRecoveries = new Map()
    this.readContentCapabilities = new Map()
    this.openedDetails = new Set()
    this.detailSnapshots = new Map()
    this.pinnedInfoFlights = new Map()
    this.pinnedTranscriptFlights = new Map()
    this.pinnedReadProofs = new Map()
    // The direct carrier: what each machine said it can do, the carrier's own
    // inbound envelope sequence, and the reads waiting on one right now. A
    // page whose typed layer gave it no provider has no carrier and every
    // read of it takes the relay, which is also what happens on a network the
    // carrier cannot cross.
    this.carrierCapabilities = new Map()
    this.carrierSequences = new Map()
    // A Session row copied onto the carrier keeps the relay's own envelope number, because that
    // is the number the store compares it against. It therefore cannot share the floor above,
    // which counts the carrier's answers from one for every channel: one row would put the floor
    // thousands ahead and every answer after it would read as a replay. One floor per carried
    // channel, in the relay's number space.
    this.carrierRowSequences = new Map()
    this.carrierReads = new Map()
    this.directCarriers = null
    this.carrierWatched = new WeakSet()
    this.carrierMachines = new Set()
    this.classicSessionMachine = null
    this.classicSessionReads = new Map()
    this.classicSessionAttempted = new Map()
    this.classicSessionPass = null
    const priorUnavailable = options?.resumeFrom?.voiceUnavailable
    this.voiceUnavailable = priorUnavailable instanceof Set ? new Set(priorUnavailable) : new Set()
  }

  // The archived client keeps only the first 64 advertised command words in
  // its display cache. A current daemon advertises more than that, so words
  // near the end (including session-receipt) disappear from machineDescriptor
  // even while the authenticated orch/ snapshot still holds the complete list.
  // Use that live snapshot for capability checks; the cache remains a fallback
  // until this connection receives its own descriptor.
  machineDescriptor(machine) {
    const remembered = super.machineDescriptor(machine)
    const live = this.orchestratorSnapshots?.get(machine)
    if (!live?.machine || typeof live.machine !== "object" || Array.isArray(live.machine)) return remembered
    return { ...remembered, machine: { ...live.machine } }
  }

  // The frozen client's display cache clips command words at 64. Preserve the
  // signed complete list in the same bounded cache so a reopened phone can
  // verify end and session-receipt before the next orch/ snapshot arrives.
  _rememberDescriptor(machine, payload) {
    super._rememberDescriptor(machine, payload)
    const commands = payload?.machine?.commands
    const row = this.machineDescriptors?.get(machine)
    if (!row || !Array.isArray(commands) || commands.length > 512 ||
      !commands.every((word) => typeof word === "string" && word.length > 0 && word.length <= 64)) return
    row.machine = { ...row.machine, commands: commands.slice() }
    this._persistMachineDescriptors()
  }

  _loadMachineDescriptors() {
    const remembered = super._loadMachineDescriptors()
    try {
      const key = this._descriptorStorageKey()
      const parsed = JSON.parse(this.descriptorStorage?.getItem(key) || "null")
      if (parsed?.v !== 1 || !Array.isArray(parsed.machines)) return remembered
      for (const row of parsed.machines.slice(-16)) {
        const commands = row?.machine?.commands
        const known = remembered.get(row?.id)
        if (known && Array.isArray(commands) && commands.length <= 512 &&
          commands.every((word) => typeof word === "string" && word.length > 0 && word.length <= 64)) {
          known.machine.commands = commands.slice()
        }
      }
    } catch { /* optional display cache */ }
    return remembered
  }

  // The copied client sends a row about no machine it can reach to the one
  // paired machine that takes viewer events, and refuses two as ambiguous.
  // Every current daemon takes them, so an account with two machines would
  // deliver those rows nowhere. A row about a machine goes to that machine
  // first (`_viewerEventRoute`); this is only the account-wide fallback: the
  // machine this page is reading if it is one of them, else the first by id.
  _viewerEventTarget(remember) {
    try {
      return super._viewerEventTarget(remember)
    } catch (error) {
      if (error?.code !== "cloud_machine_ambiguous") throw error
      const candidates = this._viewerEventCandidates()
      // A fleet page has no single classicSessionMachine. Prefer the paired
      // Mac's durable diagnostic log over an incidental Linux test machine
      // whose id sorts first; a Linux-only account still has a recipient.
      const chosen = candidates.find((candidate) => this._evidentMac(candidate.machine))
        ?? candidates.find((candidate) => candidate.machine === this.classicSessionMachine)
        ?? candidates[0]
      if (!chosen) throw error
      if (remember) this.viewerEvents.rememberTarget(chosen)
      return chosen
    }
  }

  // A status list must not cause the old, content-bearing sessions.snapshot
  // recovery. The ss/ inventory and row recovery is owned by its publisher.
  _recoverSessions() {}

  /** Feed the original one-machine Session page from exact, authorized s/ rows. */
  enableClassicSessionView(machineID, sessionID = null) {
    if (this.classicSessionMachine === machineID && this.classicSessionTarget === sessionID) return
    this.disableClassicSessionView()
    this.classicSessionMachine = machineID
    this.classicSessionTarget = sessionID
    this._recoverClassicSessionRows()
  }

  disableClassicSessionView() {
    const channel = this.classicSessionChannel
    this.classicSessionChannel = null
    if (channel && this.subscriptionHolds) {
      const holds = (this.subscriptionHolds.get(channel) || 0) - 1
      if (holds > 0) this.subscriptionHolds.set(channel, holds)
      else this.subscriptionHolds.delete(channel)
      this._releaseClassicRead(channel)
    }
    for (const [channel, timer] of this.classicSessionReads) {
      this.clearTimeout(timer)
      this._releaseClassicRead(channel)
    }
    this.classicSessionReads.clear()
    this.classicSessionAttempted.clear()
    this.classicSessionPass = null
    this.classicSessionMachine = null
    this.classicSessionTarget = null
  }

  /**
   * Let go of a channel the list's row recovery subscribed, unless something
   * else holds it. The recovery and an opened detail ask for the same s/
   * channel when the Session open is one the list has not read yet; letting go
   * of it unconditionally dropped the opened detail's subscription the moment
   * its row arrived. On 2026-10-16 the machine's log showed that Session's s/
   * row going out with `fanout=0` and `fanout=1` by turns while the page stayed
   * open on it, and a "read again" that landed in a gap never reached the card.
   */
  _releaseClassicRead(channel) {
    if (this.openedDetails?.has(channel) || this.subscriptionHolds?.has(channel)) return
    this.unsubscribe([channel])
  }

  _recoverClassicSessionRows() {
    const machine = this.classicSessionMachine
    if (!machine || !this.ready) return
    const marker = this.statusSnapshots.get(JSON.stringify([machine, SESSION_INVENTORY_ID]))?.payload
    const ids = marker?.complete === true && marker?.inventory?.version === 1 &&
      Array.isArray(marker.inventory.sessions) ? marker.inventory.sessions : null
    if (!ids || ids.length > SESSION_INVENTORY_LIMIT || !GENERATION.test(marker.snapshot_generation)) return
    if (this.classicSessionPass !== marker.snapshot_generation) {
      this.classicSessionAttempted.clear()
      this.classicSessionPass = marker.snapshot_generation
    }
    const selected = this.classicSessionTarget ? ids.filter((id) => id === this.classicSessionTarget) : ids
    const expected = new Set(selected)
    for (const [channel, timer] of this.classicSessionReads) {
      const id = decodedChannelSegment(channel.split("/")[2])
      if (expected.has(id)) continue
      this.clearTimeout(timer)
      this.classicSessionReads.delete(channel)
      this._releaseClassicRead(channel)
    }
    // The copied client already owns the eight-channel budget and releases idle
    // channels before subscribing. Counting its occupied slots here stranded the
    // original list when another pane had used all eight. Recover in a small
    // rolling window instead; each signed row frees its slot for the next one.
    const available = Math.max(0, 2 - this.classicSessionReads.size)
    let opened = 0
    for (const id of selected) {
      if (typeof id !== "string" || !id || id === SESSION_INVENTORY_ID) return
      const status = this.statusSnapshots.get(JSON.stringify([machine, id]))?.payload
      if (!status || status.snapshot_generation !== marker.snapshot_generation ||
        !GENERATION.test(status.execution_generation)) continue
      const channel = "s/" + channelSegment(machine) + "/" + channelSegment(id)
      // The selected Session is a live view, not a one-shot list recovery.
      // Keep its channel after the first row, including when the row was
      // already held before the detail opened. Otherwise the model picker
      // and its Effort successor can be published to an unsubscribed page.
      if (this.classicSessionTarget === id && this.subscriptionHolds && this.classicSessionChannel !== channel) {
        this.subscriptionHolds.set(channel, (this.subscriptionHolds.get(channel) || 0) + 1)
        this.classicSessionChannel = channel
        this.subscribe([channel])
      }
      const held = this.sessionSnapshots.get(machine + "\u0000" + id)
      if (held?.execution_generation === status.execution_generation) continue
      if (this.classicSessionReads.has(channel) ||
        this.classicSessionAttempted.get(channel) === status.execution_generation) continue
      if (opened >= available) break
      try {
        this.subscribe([channel])
        if (!this.socketSubscriptions.has(channel)) continue
        this.classicSessionAttempted.set(channel, status.execution_generation)
        const timer = this.setTimeout(() => {
          this.classicSessionReads.delete(channel)
          this._releaseClassicRead(channel)
          this._recoverClassicSessionRows()
        }, this.readTimeoutMs)
        this.classicSessionReads.set(channel, timer)
        opened += 1
      } catch {
        this.pendingSubscriptions.delete(channel)
        this.socketSubscriptions.delete(channel)
      }
    }
  }

  /** The copied ACK handler knows ctl/; a pinned r/ ACK has the same request receipt semantics. */
  _relayAnswered(frame, refusal) {
    const seq = frame?.seq
    const pending = Number.isSafeInteger(seq) ? this.pendingBySequence?.get(seq) : null
    const machine = pending?.machine
    const readChannel = machine && "r/" + channelSegment(machine)
    const isPinnedRead = !!machine && frame?.ch === readChannel
    if (isPinnedRead) {
      const proof = pending?.key && this.pinnedReadProofs?.get(pending.key)
      if (!proof || proof.seq !== seq || proof.machineID !== machine) return
    }
    // A carrier offer leaves on `termi/<machine>/<viewer>`. The copied handler returns
    // early on a channel it does not know, and an offer to a machine that is not
    // connected would then wait out its whole timeout instead of failing at once.
    const isCarrierOffer = !!machine && !!this.deviceID &&
      frame?.ch === "termi/" + channelSegment(machine) + "/" + channelSegment(this.deviceID)
    const ctlMachine = typeof frame?.ch === "string" ? /^ctl\/([^/]+)$/u.exec(frame.ch) : null
    const target = machine || (ctlMachine ? decodedChannelSegment(ctlMachine[1]) : null)
    const before = target ? this.machineOffline?.get(target) : null
    super._relayAnswered(isPinnedRead || isCarrierOffer
      ? { ...frame, ch: "ctl/" + channelSegment(machine) } : frame, refusal)
    if (target && (frame?.status === "machine_offline" || frame?.status === "delivered") &&
      this.machineOffline?.get(target) !== before) {
      this._emit({ type: "machine_reachability", machine: target })
    }
  }

  // ----- The direct carrier -----
  //
  // A read normally travels to the machine through relay.clawdline.com in San
  // Jose and back: about 290 ms of network for a phone on the same desk as the
  // machine, and one of this connection's eight subscription channels while it
  // waits. When the two ends can reach each other the same request goes
  // straight over a WebRTC data channel: the same signed, end-to-end encrypted
  // envelope, the same machine-side authorization one read at a time, and no
  // subscription at all, because the answer comes back on the channel it was
  // asked on (docs/cloud-terminal-wire.md).
  //
  // The relay stays the authority and the fallback. Nothing here can make a
  // read succeed that the relay would have refused, and every way the carrier
  // can fail ends with the same read asked again on the relay.

  /** Given the carrier provider by the typed layer; without it this client has no carrier. */
  useDirectCarriers(provider) {
    this.directCarriers = provider && typeof provider.for === "function" ? provider : null
  }

  /** Whether this machine's own descriptor says it opens carriers. */
  carrierSupported(machine) {
    if (this.carrierCapabilities?.get(machine)?.supported === true) return true
    return this.machineDescriptor(machine)?.machine?.session_carrier_v1 === true
  }

  /**
   * What this page's carrier to that machine is doing, for the dot the machine's
   * row draws. It opens nothing and asks nothing: both facts are already here,
   * and `known` is false until this browser has read that machine's descriptor,
   * because "not supported" and "not read yet" are different sentences.
   */
  carrierFacts(machine) {
    const known = this.carrierCapabilities?.has(machine) === true || !!this.machineDescriptor(machine)
    return {
      known,
      supported: this.carrierSupported(machine),
      open: typeof this.directCarriers?.open === "function" && this.directCarriers.open(machine) === true,
    }
  }

  /**
   * One direct attempt the person asked for (`DirectCarrier.tryNow`), or
   * `{ kind: "unsupported" }` when this page has no carrier to offer at all.
   */
  async tryDirect(machine) {
    const carrier = this._carrierFor(machine)
    if (!carrier || typeof carrier.tryNow !== "function") return { kind: "unsupported" }
    return carrier.tryNow()
  }

  sameNetworkAvailable(machine) {
    const descriptor = this.machineDescriptor(machine)?.machine
    return this.allowWrites === true && descriptor?.commands?.includes("direct-local-network-enable") === true &&
      descriptor.operation_allowed === true && this._carrierFor(machine)?.canEnableSameNetwork === true
  }

  async enableSameNetwork(machine) {
    const result = await this._machineRequestAs(crypto.randomUUID(), machine,
      "direct-local-network-enable", {}, "action")
    if (result?.direct_local_network !== true) throw refused("bad_response", "The machine did not confirm the setting.")
    this._carrierFor(machine)?.sameNetworkEnabled()
  }

  /** The carrier for this machine, or null when this page has none to offer. */
  _carrierFor(machine) {
    if (!this.directCarriers || typeof machine !== "string" || !machine || this.carrierPairingRefusals?.has(machine)) return null
    let carrier = null
    try { carrier = this.directCarriers.for(machine) } catch { return null }
    if (carrier && !this.carrierWatched.has(carrier)) {
      this.carrierWatched.add(carrier)
      this.carrierMachines.add(machine)
      carrier.whenDown((code) => this._carrierDown(machine, code))
    }
    return carrier
  }

  /**
   * The carrier word is answered from the machine's terminal lane, so it is not in the routed
   * vocabulary a descriptor lists. Answer for it from the one flag that does say, which is also
   * how an older daemon is never sent an offer at all: it publishes no flag, so this says no and
   * the copied reader refuses the offer before a sequence is spent.
   */
  _machineImplements(machine, type, options) {
    if (type === CARRIER_WORD) return this.carrierSupported(machine) ? "yes" : "no"
    // The archived client persists only the first 64 command words. Absence
    // from that prefix is not a machine refusal, even for a Linux machine.
    // A named read may ask the machine; its signed answer is authoritative.
    const live = this.orchestratorSnapshots?.get(machine)?.machine
    const remembered = this.machineDescriptors?.get(machine)?.machine?.commands
    const lacks = this.machineLacks?.get(machine)
    if ((!live || typeof live !== "object" || Array.isArray(live)) &&
      Array.isArray(remembered) && remembered.length === 64 && !remembered.includes(type) &&
      !lacks?.has(type)) return "unknown"
    return super._machineImplements(machine, type, options)
  }

  /** Restore local capability without waiting for a relay publication or sending a request. */
  async restoreMachinePairing(machine) {
    if (typeof machine !== "string" || !machine) return
    // A foreground return can find storage ready after an earlier absent or
    // incomplete answer. Keep usable keys; retry only the unavailable reading.
    if (!this.machinePairings.has(machine)) {
      // Retrying an unavailable read is not a new pairing generation. The
      // global invalidation barrier would cancel another machine's parallel
      // restoration; explicit handovers still use that barrier unchanged.
      this.pairingAbsent.delete(machine)
      this.pairingIncomplete.delete(machine)
    }
    try {
      await this._machinePairing(machine)
    } catch (error) {
      // The archived lookup records a tuple before validating it. A rejected
      // tuple is not restoration proof and must not create a paired roster row.
      const held = this.machinePairings.get(machine)
      if (held && (held.machineID !== machine || !held.senderID || !held.keyID ||
        held.masterKey?.extractable !== false || held.senderKey?.extractable !== false)) {
        this.machinePairings.delete(machine)
        this.pairingSeen.delete(machine)
        this.pairingLookups.delete(machine)
      }
      throw error
    }
  }

  _knownMachines() {
    // Local lookups can prove saved keys or their absence, independently of
    // account registration. Store failures do not leave a lookup answer.
    return [...new Set([...super._knownMachines(), ...this.machinePairings.keys(), ...this.pairingLookups.keys()])].sort()
  }

  /** Retry backoff expiring permits another request; only live evidence restores presence. */
  _machineRows() {
    return super._machineRows().map((row) => {
      // Rich s/ rows are opened detail, not the machine's Session inventory.
      // The signed, fresh ss/ marker proves a count even before its rows replay.
      // Missing or partial evidence remains unknown; only a complete empty set is zero.
      const inventory = statusInventoryIDs(this, row.id)
      return Object.freeze({ ...row, sessions: inventory?.length,
        ...(this.machineOffline?.has(row.id) ? { freshness: "stale", autoSelectable: false } : {}) })
    })
  }

  _machinesFor(type) {
    let found = super._machinesFor(type)
    if (type !== "voice") return found
    if (!found.capable.length && found.evidentMac) {
      // The archived picker drops every unknown route as soon as it sees a Mac.
      // Its 64-word descriptor cache makes voice unknown on that very Mac while
      // its live descriptor is between snapshots. Keep only paired, evident Macs
      // as candidates; a named request gets the machine's authoritative answer.
      const possible = found.unconfirmed.filter((row) => this._evidentMac(row.id))
      if (possible.length) found = { ...found, capable: possible,
        unconfirmed: found.unconfirmed.filter((row) => !possible.includes(row)) }
    }
    if (this.voiceUnavailable instanceof Set && this.voiceUnavailable.size) {
      found = { ...found, capable: found.capable.filter((row) => !this.voiceUnavailable.has(row.id)) }
    }
    // Capability is not availability. This also gates saved choices, sole
    // candidates and the Session-owner path before any recording is sent.
    return { ...found, capable: found.capable.filter((row) => row.freshness === "current") }
  }

  /** A saved choice wins; otherwise one current Mac is the fleet fallback. */
  _voiceHost() {
    const choice = this._voiceHostChoice()
    const found = this._machinesFor("voice")
    if (found.capable.some((row) => row.id === choice)) return super._voiceHost()
    const macs = found.capable.filter((row) => row.freshness === "current" &&
      ["darwin", "macos"].includes(String(this.machineDescriptor(row.id)?.machine?.platform || "").toLowerCase()))
    if (macs.length === 1) {
      return { machine: macs[0].id, chosen: false,
        candidates: Object.freeze(found.capable.map((row) => row.id)), error: null }
    }
    return super._voiceHost()
  }

  /**
   * The Session owner is the first choice, including over an old saved host.
   * It is never persisted: moving to another Session must move dictation with
   * it. If that machine is stale or cannot transcribe, the person's saved host
   * and then the ordinary macOS-first fleet fallback apply.
   */
  _sessionVoiceHost(machine) {
    const found = this._machinesFor("voice")
    const current = found.capable.find((row) => row.id === machine && row.freshness === "current")
    if (current) {
      return { machine: current.id, chosen: false,
        candidates: Object.freeze(found.capable.map((row) => row.id)), error: null }
    }
    return this._voiceHost()
  }

  voiceHostFor(machine) {
    try {
      const host = this._sessionVoiceHost(machine)
      if (!host.machine) throw host.error
      return Promise.resolve({ machine: host.machine, chosen: host.chosen,
        candidates: host.candidates })
    } catch (error) { return Promise.reject(error) }
  }

  /**
   * Publishes one `carrier_offer` and resolves with the machine's answer.
   *
   * The offer is a signed `termi` request because that lane is the machine's own, off its read
   * queue: gathering ICE candidates takes a round trip to a STUN server and a transcript waiting
   * in the read queue must not wait behind it. The answer is an ordinary read answer on the
   * machine reply channel, which this page already reads `sessions.list` from.
   */
  async publishCarrierOffer(machine, offer) {
    const body = { request: offer.request, connection: offer.connection,
      key_id: offer.keyID, key: bytesBase64(offer.key), sdp: offer.sdp }
    const answer = await this._read({ machine, session: CARRIER_SESSION }, CARRIER_WORD, body,
      "read:" + offer.request, CARRIER_TIMEOUT_MS, { probe: false })
    if (!answer || typeof answer !== "object" || typeof answer.sdp_sealed !== "string" ||
      !answer.sdp_sealed || answer.carrier !== offer.connection) {
      throw refused("carrier_bad_answer", "this machine's carrier answer is not usable")
    }
    return { sdpSealed: answer.sdp_sealed, directReceipts: answer.direct_receipts === true,
      directLocalNetwork: typeof answer.direct_local_network === "boolean" ? answer.direct_local_network : undefined }
  }

  /** Seals and signs the `termi` offer. The frozen sealer validates only ctl/, so sign the renamed bytes. */
  async _publishCarrierOffer(machine, body, pending) {
    if (!this.ready) throw this.closedFailure || refused("offline", "the Cloud connection is not ready")
    if (!this.devicePrivateKey || !this.deviceID) throw refused("missing_device_key", "the viewer key is unavailable")
    const offline = this._offlineRefusal(machine)
    if (offline) throw offline
    const pairing = await this._outboundMachinePairing(machine)
    const sequence = await this.nextSequence(this.deviceID)
    if (!Number.isSafeInteger(sequence) || sequence < 0) throw refused("bad_sequence", "invalid envelope sequence")
    const request = { v: 1, type: "terminal_request", request_id: body.request, connection: body.connection,
      operation: "carrier_offer", key_id: body.key_id, key: body.key, body: { sdp: body.sdp } }
    const envelope = await sealEnvelope({
      ch: "ctl/" + channelSegment(machine), seq: sequence, ts: Date.now(), class: "ctl",
      key_id: pairing.keyID, sender: this.deviceID,
    }, JSON.stringify(request), pairing.masterKey, this.devicePrivateKey)
    envelope.ch = "termi/" + channelSegment(machine) + "/" + channelSegment(this.deviceID)
    envelope.sig = bytesBase64(await crypto.subtle.sign({ name: "Ed25519" }, this.devicePrivateKey,
      envelopeSigningBytes(envelope)))
    const ref = { sender: this.deviceID, seq: sequence, request: body.request }
    if (pending?.waiters) {
      if (this.readWaiters.get(pending.key) !== pending.waiters) {
        throw refused("cloud_read_settled", "the carrier offer settled before it was sent")
      }
      pending.waiters.ref = ref
      pending.registered = { ref, machine, key: pending.key || null, ack: null }
      this.pendingBySequence.set(sequence, pending.registered)
    }
    try { this._send({ type: "publish", envelope }) }
    catch (error) {
      this.pendingBySequence.delete(sequence)
      if (pending?.waiters) pending.waiters.ref = null
      throw this.closedFailure || error
    }
    return envelope
  }

  /**
   * One envelope the carrier delivered.
   *
   * Everything the relay path checks is checked here — the ten envelope fields, the channel, the
   * machine's pairing and key id, its signature, the account content key — except the sequence,
   * which is the carrier's own. A sender's sequence is read as strictly increasing, and the two
   * carriers cannot share one counter: the answer that took the short path would arrive in front
   * of a relay envelope with a lower number and that envelope would be read as a replay. The
   * machine counts separately for the same reason (`directPeer.carrierSeq`).
   *
   * It counts per peer, so the floor is kept per channel and not per machine: a carrier that
   * replaced another starts at one again, and one number is only comparable with another from
   * the same channel (`DirectCarrier.channelGeneration`).
   */
  receiveCarrierEnvelope(machine, envelope, channelGeneration = 0) {
    this.carrierReceiveQueues ??= new Map()
    this.carrierReceiveBytes ??= 0
    this.carrierReceiveRows ??= 0
    const bytes = JSON.stringify(envelope).length
    if (this.carrierReceiveRows >= CARRIER_RECEIVE_ROWS ||
      bytes > CARRIER_RECEIVE_BYTES - this.carrierReceiveBytes) {
      const error = refused("carrier_receive_full", "the viewer's authenticated receive queue is full")
      this._recordReceiveFailure(error, envelope, false, { stage: "receive_queue" })
      this._carrierFor(machine)?.failed(channelGeneration, "carrier_receive_full")
      return Promise.resolve()
    }
    const key = JSON.stringify([machine, channelGeneration])
    const previous = this.carrierReceiveQueues.get(key) ?? Promise.resolve()
    this.carrierReceiveBytes += bytes
    this.carrierReceiveRows++
    // The ordered DataChannel is not an ordered WebCrypto completion queue. Keep
    // pairing lookup, authentication, replay admission and application in wire order.
    const next = previous.catch(() => {}).then(() => {
      if (!this.retired) return this._receiveCarrierEnvelope(machine, envelope, channelGeneration)
    }).finally(() => {
      this.carrierReceiveBytes -= bytes
      this.carrierReceiveRows--
      if (this.carrierReceiveQueues.get(key) === next) this.carrierReceiveQueues.delete(key)
    })
    this.carrierReceiveQueues.set(key, next)
    return next
  }

  async _receiveCarrierEnvelope(machine, envelope, channelGeneration) {
    const probe = { stage: "channel_parse", original: null, cause: null, senderKeyFound: null,
      senderKeySource: null, senderKeyLookupMs: null, routedMachine: machine,
      pairing: undefined, pairingSource: null, pairingLookupMs: null, pairingFoundBefore: null }
    let row = false
    try {
      const channel = validateEnvelope(envelope)
      row = channel.kind === "session"
      if ((channel.kind !== "transcript" && !row) || decodedChannelSegment(channel.machine) !== machine) {
        throw refused("bad_channel", "the carrier delivered a channel it may not carry")
      }
      // The machine's whole-list inventory stays on the relay. Applying one here would fill
      // `sessionInventoryByMachine` and raise the authoritative `sessions` event, which drives
      // the older whole-list recovery this list deliberately does not use — and an inventory
      // this page then believed would delete every row it does not name.
      if (row && decodedChannelSegment(channel.session) === SESSION_INVENTORY_ID) {
        throw refused("bad_channel", "the carrier may not carry a machine's Session inventory")
      }
      probe.stage = "machine_pairing_lookup"
      const pairing = await this._machinePairing(machine, probe)
      if (!pairing) throw refused("machine_not_paired", "this browser is not paired with the carrier's machine")
      probe.stage = "pairing_key_id"
      this._compareKeyID(envelope, machine, pairing.keyID)
      if (pairing.keyID !== envelope.key_id || pairing.senderID !== envelope.sender) {
        throw refused("unknown_sender", "the carrier's machine does not match its pairing")
      }
      probe.stage = "signature_verify"
      const clear = await openEnvelope(envelope, pairing.masterKey, pairing.senderKey, probe)
      probe.stage = "sequence"
      if (row) {
        const floor = this.carrierRowSequences.get(envelope.ch)
        if (floor !== undefined && envelope.seq <= floor) {
          throw refused("replay", "the carried row sequence did not advance")
        }
        this.carrierRowSequences.set(envelope.ch, envelope.seq)
      } else {
        const previous = this.carrierSequences.get(machine)
        if (previous !== undefined && (previous.channel > channelGeneration ||
          (previous.channel === channelGeneration && envelope.seq <= previous.seq))) {
          throw refused("replay", "the carrier sequence did not advance")
        }
        this.carrierSequences.set(machine, { channel: channelGeneration, seq: envelope.seq })
      }
      probe.stage = "payload"
      const payload = clear.length ? JSON.parse(decoder.decode(clear)) : null
      this.machineOffline?.delete(machine)
      this._sawAuthenticatedEnvelope(envelope, channel, machine)
      probe.stage = "apply"
      // `sequenceBySender` is not raised here, and must not be: it is one floor for everything a
      // sender publishes, and the relay will publish this very envelope again on its own road.
      // A row arrives because the machine published it just now, so it is not a realignment.
      this._applySnapshot(channel, payload, envelope, false)
    } catch (error) {
      this._recordReceiveFailure(error, envelope, false, probe)
      // The same words the relay's receive path records (`cloud-client.js` `_receiveEnvelope`): an
      // envelope that arrived on the carrier and did not open or apply is this page's failure,
      // whatever the relay socket is doing.
      viewerHealthFailed("viewer_decrypt_apply", error && error.code === "replay" ? "replay_rejected"
        : probe.stage === "apply" ? "apply_failed" : "decrypt_failed", { class: "read" })
      // This channel delivered something this page cannot use, and it will deliver the next
      // answer the same way. Nothing has failed from the read's point of view — the carrier is
      // open and its request was written to it — so it is this that has to end the channel, and
      // closing it is what asks every read it holds again on the relay.
      //
      // A row is the exception: nothing waits for it, the relay is still publishing it, and the
      // page still has its own way of asking for one. If the channel is broken rather than the
      // row, the next answer fails here too and closes it then.
      if (!row) {
        try { this._carrierFor(machine)?.failed(channelGeneration, "carrier_unusable") }
        catch { /* a carrier this page no longer has is already gone */ }
      }
    }
  }

  /**
   * A read on the carrier: the copied reader's waiter, timeout and abort machinery, with no
   * subscription and the request written to the data channel.
   *
   * This is the one thing that could not be reused from the copied `_read`, which always
   * subscribes to the answer channel before publishing. On the carrier there is nothing to
   * subscribe to — and not spending one of the eight relay subscription channels is half of why
   * the carrier is here.
   */
  _readOnCarrier(identity, carrier, type, extra, answer, timeoutMs, readOptions) {
    const key = identity.machine + "\u0000" + identity.session + "\u0000" + answer
    const self = this
    return new Promise(function (resolve, reject) {
      const signal = readOptions && readOptions.signal
      if (signal && signal.aborted) { reject(self._abortedRead()); return }
      const waiter = { resolve, reject, signal, aborted: null }
      const existing = self.readWaiters.get(key)
      if (existing) {
        existing.waiting.push(waiter)
        self._watchReadAbort(key, existing, waiter)
        return
      }
      const waiters = { waiting: [waiter], timer: null, ref: null, machine: identity.machine, type,
        request: extra && typeof extra.request === "string" ? extra.request : null,
        action: false, channel: null, retireUncertain: !!(readOptions && readOptions.retireUncertain),
        // The read's connection-health trace, as a relay read's (`_read`): issued now, settled by
        // `_settleRead` under the same correlation, so a read the carrier holds and never answers
        // is a timed-out read, not a silence behind an open relay socket.
        health: viewerHealthBegin("viewer_request_issued", "read") }
      self.readWaiters.set(key, waiters)
      self._watchReadAbort(key, waiters, waiter)
      if (self.readWaiters.get(key) !== waiters) return
      waiters.timer = self.setTimeout(function () {
        if (self.readWaiters.get(key) !== waiters) return
        waiters.timer = null
        self.carrierReads.delete(key)
        self._readTimedOut(key, waiters, !(readOptions && readOptions.probe === false))
      }, timeoutMs || self.readTimeoutMs)
      self.carrierReads.set(key, { identity, type, extra, waiters })
      Promise.resolve()
        .then(function () {
          return self._publishCommand(identity.machine, type,
            Object.assign({ session: identity.session }, extra), "ctl", { key, waiters, carrier })
        })
        .catch(function (error) {
          if (self.readWaiters.get(key) !== waiters) return
          // Nothing was written, so nothing is replayed: the request is simply
          // asked on the relay instead, and the page never learns there was a
          // carrier to fail.
          self._relayAfterCarrier(key, error)
        })
    })
  }

  /**
   * Asks a carrier read again on the relay, keeping the waiter the caller already holds.
   *
   * A read has no effect, which is what makes this different from terminal input: re-sending one
   * cannot do anything twice. So a request the carrier refused — or one it took and then died
   * holding — is published again on the relay rather than left to time out, and the page shows an
   * answer instead of "could not read". `_publishCommand` re-pins the proof to the new sequence.
   */
  _relayAfterCarrier(key, cause) {
    const flight = this.carrierReads.get(key)
    if (!flight) return
    this.carrierReads.delete(key)
    const waiters = flight.waiters
    if (this.readWaiters.get(key) !== waiters) return
    // The read's carrier gave up and the relay takes it over: recorded under the read's own
    // correlation, before the answer the relay brings settles it.
    if (waiters.health) {
      waiters.health.mark("viewer_carrier_replace", "viewer_stage_failed",
        { error: cause ? classifyViewerError(cause, "read") : "carrier_closed", outcome: "error" })
    }
    // The sequence the carrier was given is spent. Nothing on the relay will ever
    // acknowledge it, so stop waiting for an ACK under that number before the
    // resend pins a new one.
    if (waiters.ref) { this.pendingBySequence?.delete(waiters.ref.seq); waiters.ref = null }
    const settle = (error) => { if (this.readWaiters.get(key) === waiters) this._settleRead(key, null, error) }
    const channel = "t/" + channelSegment(flight.identity.machine) + "/" + channelSegment(flight.identity.session)
    let full = null
    try {
      full = this._subscriptionRoom(channel)
      if (!full) {
        this.subscribe([channel])
        waiters.channel = channel
        this.subscriptionHolds.set(channel, (this.subscriptionHolds.get(channel) || 0) + 1)
      }
    } catch (error) {
      settle(cause || refused("malformed_read", error.message))
      return
    }
    if (full) { settle(cause || full); return }
    Promise.resolve()
      .then(() => this._publishCommand(flight.identity.machine, flight.type,
        Object.assign({ session: flight.identity.session }, flight.extra), "ctl", { key, waiters }))
      .catch((error) => settle(error))
  }

  /** The carrier went: every read it was holding is asked again on the relay. */
  _carrierDown(machine, code) {
    if (code === "machine_pairing_required") {
      // Older daemons require a strong local pin only on the direct path.
      // A transport refusal does not prove the authenticated relay is unusable.
      // Keep this client on the relay until pairing is refreshed or it renews.
      this.carrierPairingRefusals ??= new Set()
      this.carrierPairingRefusals.add(machine)
      this.viewerEvents?.record?.("cloud.carrier.fallback", { machine, code, stage: "carrier_refused" })
    }
    for (const [key, flight] of [...this.carrierReads]) {
      if (flight.identity.machine === machine) this._relayAfterCarrier(key, null)
    }
  }

  /** Only explicitly capable peers and bounded decoders negotiate a new representation. */
  _read(value, type, extra, answer, timeoutMs, readOptions) {
    let identity
    try { identity = this._sessionIdentity(value) } catch (error) { return Promise.reject(error) }
    if (typeof DecompressionStream !== "function" || typeof answer !== "string" || !COMPRESSED_READ_WORDS.has(type) || extra?.accept_encoding !== undefined ||
      this.machineLacks?.get(identity.machine)?.has("read-compression-v1") ||
      this.machineDescriptor?.(identity.machine)?.machine?.read_compression_v1 !== true) {
      return this._readRepresentation(value,type,extra,answer,timeoutMs,readOptions)
    }
    return this._readRepresentation(value,type,{...extra,accept_encoding:"gzip-v1"},answer+READ_COMPRESSION_SUFFIX,timeoutMs,readOptions).catch(error=>{
      if (readOptions?.signal?.aborted || !["malformed_read","unknown_command","cloud_read_timeout","read_compression_unavailable","read_payload_corrupt","read_encoding_unsupported"].includes(error?.code)) throw error
      this.machineLacks ??= new Map()
      const lacks = this.machineLacks.get(identity.machine) ?? new Set()
      lacks.add("read-compression-v1")
      this.machineLacks.set(identity.machine,lacks)
      return this._readRepresentation(value,type,extra,answer,timeoutMs,readOptions)
    })
  }

  /** The copied reader's local write flag predates the read-only r/ channel. */
  _readRepresentation(value, type, extra, answer, timeoutMs, readOptions) {
    if (!pinnedRead(type, extra)) return super._read(value, type, extra, answer, timeoutMs, readOptions)
    const machineList = type === "sessions.list" && value.session === "__clawdline_machine__"
    if ((!machineList && !GENERATION.test(extra.expected_generation)) ||
      (machineList && extra.expected_generation !== undefined) ||
      typeof extra.machine_id !== "string" || !extra.machine_id ||
      answer !== pinnedReadName(type, extra)) {
      return Promise.reject(refused("execution_target_required", "an exact Session execution is required"))
    }
    const key = pinnedReplyKey(extra.machine_id, value.session, answer)
    if (this.readWaiters.has(key)) {
      return Promise.reject(refused("cloud_read_busy", "another read owns this Session reply channel"))
    }
    const proof = { machineID: extra.machine_id, sessionID: value.session,
      generation: machineList ? null : extra.expected_generation, read: answer, seq: null, waiters: null }
    this.pinnedReadProofs.set(key, proof)
    // The copied _read checks allowWrites synchronously before registering its
    // t/ waiter. It is a local guard for ctl/; this one narrow call publishes
    // on r/ and is authorized separately by the relay and machine.
    const previous = this.allowWrites
    this.allowWrites = true
    try {
      // The carrier is taken only when it is already open. A cold foreground
      // read goes over the relay before this page negotiates another path: a
      // failed STUN/data-channel attempt on a small executor must not contend
      // with the first conversation the person is waiting to see.
      const carrier = this._carrierFor(extra.machine_id)
      const direct = carrier?.open ? carrier : null
      const promise = direct
        ? this._readOnCarrier({ machine: extra.machine_id, session: value.session },
          direct, type, extra, answer, timeoutMs, readOptions)
        : super._read(value, type, extra, answer, timeoutMs, readOptions)
      if (carrier && !direct) {
        // Only a successful read warms the optional path. The timer yields to
        // the caller's promise continuation, so the transcript can be drawn
        // before WebRTC work begins. DirectCarrier.ensure coalesces any other
        // reads that completed in the same turn and applies its retry bound.
        void promise.then(() => {
          setTimeout(() => { void carrier.ensure().catch(() => {}) }, 0)
        }, () => {})
      }
      proof.waiters = this.readWaiters.get(key) || null
      if (!proof.waiters) this.pinnedReadProofs.delete(key)
      return promise
    }
    catch (error) {
      if (this.pinnedReadProofs.get(key) === proof) this.pinnedReadProofs.delete(key)
      throw error
    }
    finally { this.allowWrites = previous }
  }

  /** Only pinned content reads may leave on the read_transcript-authorized r/ channel. */
  async _publishCommand(machine, type, body, envelopeClass, pending) {
    if (type === CARRIER_WORD) return this._publishCarrierOffer(machine, body, pending)
    if (type === "peer-inbox" && !GENERATION.test(body?.expected_generation)) {
      throw refused("execution_target_required", "an exact Session execution is required")
    }
    if (!pinnedRead(type, body)) return super._publishCommand(machine, type, body, envelopeClass, pending)
    const machineList = type === "sessions.list" && body.session === "__clawdline_machine__" &&
      body.expected_generation === undefined
    if (envelopeClass !== "ctl" || body.machine_id !== machine || typeof body.session !== "string" || !body.session ||
      (!machineList && !GENERATION.test(body.expected_generation)) || !pinnedReadName(type, body)) {
      throw refused("execution_target_required", "an exact Session execution is required")
    }
    const unsupported = this._unsupportedRefusal(machine, type)
    if (unsupported) throw unsupported
    // `machineOffline` is the relay's word that this machine is not connected to
    // it. An open carrier is this page's own evidence to the contrary, so a read
    // about to travel on one is not refused by it.
    if (!pending?.carrier) {
      const offline = this._offlineRefusal(machine)
      if (offline) throw offline
    }
    // The same reasoning as `machineOffline` above, for this page's own socket: a read on an open
    // carrier does not touch the relay, so a relay socket between connections is not its problem.
    // Measured on 2026-10-11 at 08:39, while this page renewed its credentials: three reads in a
    // row were refused `offline` inside a second, which is the residual failure the Cloud
    // read-failure observation keeps recording as `connection=not_ready`. A retired or stopped
    // client closes its carriers, so an open carrier here means the socket is reconnecting --
    // exactly when a second road is worth having. The machine still authorizes every carrier read
    // by itself.
    if (!this.ready && !pending?.carrier) {
      throw this.closedFailure || refused("offline", "the Cloud connection is not ready")
    }
    if (!this.devicePrivateKey || !this.deviceID) throw refused("missing_device_key", "the viewer key is unavailable")
    const pairing = await this._outboundMachinePairing(machine)
    const sequence = await this.nextSequence(this.deviceID)
    if (!Number.isSafeInteger(sequence) || sequence < 0) throw refused("bad_sequence", "invalid envelope sequence")
    if (pending?.key) {
      const proof = this.pinnedReadProofs.get(pending.key)
      if (!proof || proof.waiters !== pending.waiters || proof.machineID !== machine ||
        proof.sessionID !== body.session || proof.generation !== (machineList ? null : body.expected_generation) ||
        proof.read !== pinnedReadName(type, body)) {
        throw refused("read_reply_mismatch", "the pinned read no longer owns its reply")
      }
      proof.seq = sequence
    }
    const envelope = await sealEnvelope({
      ch: "ctl/" + channelSegment(machine), seq: sequence, ts: Date.now(), class: "ctl",
      key_id: pairing.keyID, sender: this.deviceID,
    }, JSON.stringify({ type, ...body }), pairing.masterKey, this.devicePrivateKey)
    // The byte-frozen sealer validates only the older ctl/ vocabulary. Its
    // ciphertext has no channel AAD, so name r/ and sign those exact wire bytes.
    envelope.ch = "r/" + channelSegment(machine)
    envelope.sig = bytesBase64(await crypto.subtle.sign({ name: "Ed25519" }, this.devicePrivateKey,
      envelopeSigningBytes(envelope)))
    const ref = { sender: this.deviceID, seq: sequence,
      request: typeof body.request === "string" ? body.request : null }
    if (pending) {
      if (pending.waiters) {
        if (this.readWaiters.get(pending.key) !== pending.waiters) {
          throw refused("cloud_read_settled", "the read settled before it was sent")
        }
        pending.waiters.ref = ref
      }
      pending.registered = { ref, machine, key: pending.key || null, ack: pending.ack || null }
      this.pendingBySequence.set(sequence, pending.registered)
    }
    this.trail.sealed({ sender: ref.sender, seq: sequence, request: ref.request, type, machine })
    try {
      if (pending?.carrier) pending.carrier.send(envelope)
      else this._send({ type: "publish", envelope })
    }
    catch (error) {
      this.pendingBySequence.delete(sequence)
      if (pending?.waiters) pending.waiters.ref = null
      throw pending?.carrier ? error : (this.closedFailure || error)
    }
    return envelope
  }

  _applySnapshot(channel, payload, envelope, realign) {
    if (channel?.kind === "transcript" && typeof payload?.read === "string") {
      const machine = decodedChannelSegment(channel.machine)
      const session = decodedChannelSegment(channel.session)
      const key = pinnedReplyKey(machine, session, payload.read)
      const proof = this.pinnedReadProofs?.get(key)
      if (proof) {
        // Exact subscriptions realign with the relay's last t/ row. It is not
        // an answer to a request this page has just sent.
        if (realign) return
        if (proof.waiters !== this.readWaiters.get(key) || !pinnedReplyMatches(payload, proof)) {
          this._settleRead(key, null, refused("read_reply_mismatch", "the Session content reply did not match its request"))
          return
        }
      } else if (this.readWaiters?.get(key)?.type === "peer-inbox") {
        this._settleRead(key, null, refused("read_reply_mismatch", "the peer inbox reply has no pinned request"))
        return
      }
    }
    return super._applySnapshot(channel, payload, envelope, realign)
  }

  _settleRead(key, body, error) {
    const waiters = this.readWaiters.get(key)
    if (waiters && !waiters.action && error?.code === "cloud_read_timeout") {
      this._emit({ type: "error", machine: waiters.machine, error: this._withRef(error, waiters.ref) })
    }
    this.pinnedReadProofs?.delete(key)
    this.carrierReads?.delete(key)
    return super._settleRead(key, body, error)
  }

  /**
   * A renewal hands the page to a replacement client, and this one's carrier goes with it.
   *
   * The machine keeps one carrier per viewer and gives it no idle bound, so a channel a retired
   * client leaves open is a channel the machine still believes in: measured on 2026-10-11, the
   * replacement's offer was refused as busy and that page read over the relay for the rest of
   * its life. The machine supersedes such a carrier now, and this is the other half of it — the
   * page says so itself rather than leaving the machine to work it out.
   */
  retire() {
    this._closeCarriers("carrier_client_retired")
    return super.retire()
  }

  /** The cloud connection was stopped on purpose: no read is waiting for the short path. */
  stop() {
    this._closeCarriers("carrier_client_stopped")
    return super.stop()
  }

  _closeCarriers(code) {
    for (const machine of [...this.carrierMachines]) {
      try { this._carrierFor(machine)?.close(code) } catch { /* already gone */ }
    }
    this.carrierMachines.clear()
  }

  /**
   * An opened detail holds its rich row's channel for as long as it is open.
   *
   * Unheld, the channel was trimmed as idle by the next read: on 2026-10-12 the
   * machine's log recorded `ch=t/<machine>/<session> fanout=1` every minute and
   * `ch=s/<machine>/<session> fanout=0` throughout, for a Session the page had
   * open. A Session stopped on a question is exactly the one whose row stops
   * moving, so the row carrying the question never arrived, and the card said
   * the machine could not read a menu it had read whole.
   */
  openDetail(destination) {
    const channel = "s/" + channelSegment(destination.machineID) + "/" + channelSegment(destination.sessionID)
    if (!this.openedDetails.has(channel) && this.subscriptionHolds) {
      this.subscriptionHolds.set(channel, (this.subscriptionHolds.get(channel) || 0) + 1)
    }
    this.openedDetails.add(channel)
    this.detailSnapshots.delete(destination.machineID + "\u0000" + destination.sessionID)
  }

  closeDetail(destination) {
    const channel = "s/" + channelSegment(destination.machineID) + "/" + channelSegment(destination.sessionID)
    if (this.openedDetails.delete(channel) && this.subscriptionHolds) {
      const holds = (this.subscriptionHolds.get(channel) || 0) - 1
      if (holds > 0) this.subscriptionHolds.set(channel, holds)
      else this.subscriptionHolds.delete(channel)
    }
    this.detailSnapshots.delete(destination.machineID + "\u0000" + destination.sessionID)
  }

  _emit(event) {
    if (event?.type === "connection" && event.state === "live") this._recoverClassicSessionRows()
    if (event?.type === "session_status" && event.identity?.machine === this.classicSessionMachine &&
      event.identity.session === SESSION_INVENTORY_ID) this._recoverClassicSessionRows()
    if (event?.type === "sessions" && event.identity?.machine === this.classicSessionMachine &&
      event.identity.session) {
      const channel = "s/" + channelSegment(event.identity.machine) + "/" + channelSegment(event.identity.session)
      const timer = this.classicSessionReads.get(channel)
      if (timer !== undefined) {
        this.clearTimeout(timer)
        this.classicSessionReads.delete(channel)
        this._releaseClassicRead(channel)
        this._recoverClassicSessionRows()
      }
    }
    if (event?.type === "orchestrator" && !event.statusOnly && event.machine) {
      const payload = event.data
      this.readContentCapabilities?.set(event.machine, {
        at: payload?.at, supported: payload?.machine?.read_content_v1 === true,
      })
      this.carrierCapabilities?.set(event.machine, {
        at: payload?.at, supported: payload?.machine?.session_carrier_v1 === true,
      })
    }
    if (event?.type === "sessions" && event.identity?.machine && event.identity?.session) {
      const channel = "s/" + channelSegment(event.identity.machine) + "/" + channelSegment(event.identity.session)
      if (this.openedDetails?.has(channel)) {
        const key = event.identity.machine + "\u0000" + event.identity.session
        const row = this.sessionSnapshots.get(key)
        if (row) this.detailSnapshots.set(key, row)
        else this.detailSnapshots.delete(key)
      }
    }
    return super._emit(event)
  }

  async _receiveEnvelope(envelope, realign) {
    if (typeof envelope?.ch !== "string" || !envelope.ch.startsWith("ss/")) {
      return super._receiveEnvelope(envelope, realign)
    }
    const probe = { stage: "channel_parse", original: null, cause: null, senderKeyFound: null,
      senderKeySource: null, senderKeyLookupMs: null, routedMachine: null,
      pairing: undefined, pairingSource: null, pairingLookupMs: null, pairingFoundBefore: null }
    try {
      const identity = statusChannel(envelope)
      if (!identity?.machine || !identity.session) throw refused("bad_payload", "the status channel has no identity")
      probe.routedMachine = identity.machine
      probe.stage = "machine_pairing_lookup"
      const pairing = await this._machinePairing(identity.machine, probe)
      if (!pairing) throw refused("machine_not_paired", "this browser is not paired with the status source")
      probe.stage = "pairing_key_id"
      this._compareKeyID(envelope, identity.machine, pairing.keyID)
      if (pairing.keyID !== envelope.key_id || pairing.senderID !== envelope.sender) {
        throw refused("unknown_sender", "status source does not match its pairing")
      }
      probe.stage = "signature_verify"
      const valid = await crypto.subtle.verify({ name: "Ed25519" }, pairing.senderKey,
        base64Bytes(envelope.sig, "sig"), envelopeSigningBytes(envelope))
      if (!valid) throw refused("unreadable_envelope", "status signature did not verify")
      probe.stage = "decrypt"
      const master = typeof CryptoKey !== "undefined" && pairing.masterKey instanceof CryptoKey ? pairing.masterKey
        : await importMasterSecret(pairing.masterKey)
      const clear = await crypto.subtle.decrypt({ name: "AES-GCM", iv: base64Bytes(envelope.nonce, "nonce"), tagLength: 128 },
        master, base64Bytes(envelope.ct, "ct"))
      probe.stage = "sequence"
      const sequenceKey = envelope.sender + "\n" + envelope.ch
      const previous = realign ? this.realignSequenceByChannel.get(sequenceKey) : this.sequenceBySender.get(envelope.sender)
      if (previous !== undefined && envelope.seq <= previous) throw refused("replay", "status sequence did not advance")
      if (realign) this.realignSequenceByChannel.set(sequenceKey, envelope.seq)
      if ((this.sequenceBySender.get(envelope.sender) ?? -1) < envelope.seq) this.sequenceBySender.set(envelope.sender, envelope.seq)
      probe.stage = "payload"
      const payload = JSON.parse(decoder.decode(clear))
      const key = JSON.stringify([identity.machine, identity.session])
      if ((this.statusSequences.get(key) ?? -1) > envelope.seq) return
      this.statusSequences.set(key, envelope.seq)
      // Use the envelope's signed time, as the copied s/ and orch/ paths do.
      // A retained row can still be recent, but its arrival does not renew it.
      this._observeMachine(identity.machine, envelope.ts)
      if (!realign) this.machineOffline?.delete(identity.machine)
      if (payload === null || payload?.deleted === true) this.statusSnapshots.delete(key)
      else this.statusSnapshots.set(key, { identity, payload, observedAt: envelope.ts, sequence: envelope.seq })
      this._sawAuthenticatedEnvelope(envelope, { kind: "session_status" }, identity.machine)
      this._emit({ type: "session_status", identity, envelope, realign })
    } catch (error) {
      this._recordReceiveFailure(error, envelope, realign, probe)
      throw error
    }
  }

  /** Explicitly release opened detail channels, including on a tab switch. */
  unsubscribe(channels) {
    if (!Array.isArray(channels) || !channels.length) return
    const drop = channels.filter((channel) => this.socketSubscriptions.has(channel) || this.pendingSubscriptions.has(channel))
    for (const channel of drop) {
      this.pendingSubscriptions.delete(channel)
      this.socketSubscriptions.delete(channel)
      this.resubscribes.delete(channel)
    }
    if (this.ready && drop.length) this._sendSubscriptionFrame("unsubscribe", drop)
  }

  /** Ask the relay to replay one retained status row, never a rich s/ row. */
  recoverStatusRow(machineID, sessionID, snapshotGeneration) {
    if (!machineID || !sessionID || !/^[0-9a-f]{32}$/u.test(snapshotGeneration) || !this.ready) {
      return Promise.resolve(false)
    }
    // A default ss/ row can arrive after the caller observed the gap but
    // before this exact recovery installs its listener. In that case the
    // cache already resolves the gap; waiting for a second event costs the
    // entire read timeout while the list stays empty.
    if (this.statusSnapshots.get(JSON.stringify([machineID, sessionID]))?.payload?.snapshot_generation === snapshotGeneration) {
      return Promise.resolve(true)
    }
    const channel = "ss/" + channelSegment(machineID) + "/" + channelSegment(sessionID)
    const previous = this.statusRecoveries.get(channel)
    if (previous?.generation === snapshotGeneration) return previous.promise
    previous?.finish(false)
    let resolve
    const promise = new Promise((answer) => { resolve = answer })
    let done = false
    let timer = null
    let stop = () => {}
    const finish = (received) => {
      if (done) return
      done = true
      if (timer !== null) this.clearTimeout(timer)
      stop()
      this.unsubscribe([channel])
      if (this.statusRecoveries.get(channel)?.promise === promise) this.statusRecoveries.delete(channel)
      resolve(received)
    }
    // Installing the listener first prevents a fast cached realign from
    // passing between the subscribe frame and the wait.
    stop = this.events((event) => {
      if (event.type !== "session_status" || event.identity?.machine !== machineID ||
        event.identity?.session !== sessionID) return
      const payload = this.statusSnapshots.get(JSON.stringify([machineID, sessionID]))?.payload
      if (payload?.snapshot_generation === snapshotGeneration) finish(true)
    })
    this.statusRecoveries.set(channel, { generation: snapshotGeneration, promise, finish })
    try {
      // Rich rows and transcripts often occupy every relay slot when a page
      // opens. Free idle subscriptions the same way an ordinary read does.
      this._trimSubscriptions(1, [channel])
      if (this.socketSubscriptions.size >= this.subscriptionLimit) finish(false)
      else {
        this.pendingSubscriptions.add(channel)
        this._sendSubscriptionFrame("subscribe", [channel])
        this.socketSubscriptions.set(channel, this.now())
        timer = this.setTimeout(() => finish(false), this.readTimeoutMs)
      }
    } catch { finish(false) }
    return promise
  }

  /** A cold browser may know a machine from orch/ before its default ss/ marker arrives. */
  recoverStatusMarker(machineID) {
    const sessionID = SESSION_INVENTORY_ID
    if (!machineID || !this.ready) return Promise.resolve(false)
    const key = JSON.stringify([machineID, sessionID])
    if (this.statusSnapshots.get(key)?.payload?.inventory) return Promise.resolve(true)
    const channel = "ss/" + channelSegment(machineID) + "/" + sessionID
    const previous = this.statusRecoveries.get(channel)
    if (previous) return previous.promise
    let resolve
    const promise = new Promise((answer) => { resolve = answer })
    let done = false
    let timer = null
    let stop = () => {}
    const finish = (received) => {
      if (done) return
      done = true
      if (timer !== null) this.clearTimeout(timer)
      stop()
      this.unsubscribe([channel])
      if (this.statusRecoveries.get(channel)?.promise === promise) this.statusRecoveries.delete(channel)
      resolve(received)
    }
    stop = this.events((event) => {
      if (event.type === "session_status" && event.identity?.machine === machineID &&
        event.identity?.session === sessionID && this.statusSnapshots.get(key)?.payload?.inventory) finish(true)
    })
    this.statusRecoveries.set(channel, { generation: "marker", promise, finish })
    try {
      this._trimSubscriptions(1, [channel])
      if (this.socketSubscriptions.size >= this.subscriptionLimit) finish(false)
      else {
        this.pendingSubscriptions.add(channel)
        this._sendSubscriptionFrame("subscribe", [channel])
        this.socketSubscriptions.set(channel, this.now())
        timer = this.setTimeout(() => finish(false), STATUS_MARKER_RECOVERY_TIMEOUT_MS)
      }
    } catch { finish(false) }
    return promise
  }

  _subscriptionRefused(code) {
    const channels = this.subscriptionFrames[0]?.channels ?? []
    const result = super._subscriptionRefused(code)
    for (const channel of channels) this.statusRecoveries?.get(channel)?.finish(false)
    return result
  }

  cancelStatusRecoveries() {
    for (const recovery of [...this.statusRecoveries.values()]) recovery.finish(false)
  }

  /** The list gets only a pinned title; full detail stays on the opened Session. */
  listPresentationsForMachine(machineID, signal, options = {}) {
    if (!machineID || signal?.aborted) {
      return Promise.reject(refused("execution_target_required", "a machine is required"))
    }
    const request = crypto.randomUUID()
    return this._read({ machine: machineID, session: "__clawdline_machine__" }, "sessions.list", {
      machine_id: machineID, request, ...(options.fresh ? { fresh: true } : {}),
    }, "read:" + request, PINNED_LIST_READ_TIMEOUT_MS, { signal })
  }

  /** Ask the machine to observe and publish one Session now. Its ss/ event updates the list. */
  refreshSessionStatus(machineID, sessionID, signal) {
    if (!machineID || !sessionID || signal?.aborted) {
      return Promise.reject(refused("execution_target_required", "a Session is required"))
    }
    return this._machineRequest(machineID, "sessions.row", { row: sessionID }, "read",
      PINNED_LIST_READ_TIMEOUT_MS, { signal })
  }

  /** Let each schedule machine answer independently of its slower peers. */
  schedules(options) {
    if (!options?.fresh || !options.machine) return super.schedules(options)
    if (this.retired) return this._viaSuccessor((next) => next.schedules(options))
    const machine = options.machine
    const found = this._machinesFor("schedules")
    if (found.pairable.some((row) => row.id === machine)) {
      return Promise.reject(refused("machine_pairing_required", "this browser is not paired with this machine"))
    }
    if (!found.rows.some((row) => row.id === machine)) {
      return Promise.reject(refused("cloud_read_unavailable", "this machine is not in the viewer's roster"))
    }
    if (!found.capable.some((row) => row.id === machine)) {
      if (found.rows.some((row) => row.id === machine) &&
        this._machineImplements(machine, "schedules", { learned: false }) === "no") {
        return Promise.resolve({ schedules: [], at: Math.floor(this.now() / 1000) })
      }
      // The roster can precede orch/. A named, paired machine may answer one
      // bounded authenticated read before its capability descriptor arrives.
      if (this._machineImplements(machine, "schedules", { learned: false }) === "no") {
        return Promise.reject(refused("cloud_read_unavailable", "this machine's schedule reader is not confirmed"))
      }
    }
    return this._machineRequest(machine, "schedules", {}, "read").then((answer) => {
      const rows = Array.isArray(answer?.schedules) ? answer.schedules : []
      const at = typeof answer?.at === "number" ? answer.at : 0
      const previous = this.orchestratorSnapshots.get(machine) || {}
      this.orchestratorSnapshots.set(machine,
        Object.assign({}, previous, { schedules: rows, at: at || previous.at || 0 }))
      return { schedules: rows.map((row) => Object.assign({}, row, { machine })),
        at: at || Math.floor(this.now() / 1000) }
    }).catch((error) => {
      if (["unknown_command", "cloud_machine_unsupported"].includes(error?.code)) {
        return { schedules: [], at: Math.floor(this.now() / 1000) }
      }
      throw error
    })
  }

  /** Keep distinct generations from joining the copied client's per-session read waiter. */
  infoForGeneration(destination, signal) {
    const { machineID, sessionID, executionGeneration } = destination
    if (!machineID || !sessionID || !/^[0-9a-f]{32}$/u.test(executionGeneration)) {
      return Promise.reject(refused("execution_target_required", "an exact Session execution is required"))
    }
    const key = JSON.stringify([machineID, sessionID])
    const previous = this.pinnedInfoFlights.get(key)
    if (previous?.generation === executionGeneration) return previous.promise
    const run = async () => {
      if (previous) await previous.promise.catch(() => undefined)
      if (signal?.aborted) throw refused("read_aborted", "the Session detail was closed")
      if (this.readWaiters?.has(machineID + "\u0000" + sessionID + "\u0000info.full")) {
        throw refused("cloud_read_busy", "an unpinned Session info read is already in flight")
      }
      return this._read({ machine: machineID, session: sessionID }, "info", {
        parts: "full", machine_id: machineID, expected_generation: executionGeneration,
      }, "info.full", undefined, { signal })
    }
    const promise = run().finally(() => {
      if (this.pinnedInfoFlights.get(key)?.promise === promise) this.pinnedInfoFlights.delete(key)
    })
    this.pinnedInfoFlights.set(key, { generation: executionGeneration, promise })
    return promise
  }

  /** Slash-menu metadata follows the same exact execution and reply proof as content. */
  skillsForGeneration(destination, signal) {
    const { machineID, sessionID, executionGeneration } = destination
    if (!machineID || !sessionID || !GENERATION.test(executionGeneration) || signal?.aborted) {
      return Promise.reject(refused("execution_target_required", "an exact Session execution is required"))
    }
    return this._read({ machine: machineID, session: sessionID }, "skills", {
      machine_id: machineID, expected_generation: executionGeneration,
    }, "skills", undefined, { signal })
  }

  /** The original one-machine page can ask for the same bounded thumbnail. */
  image(value, id, size) {
    const identity = this._sessionIdentity(value)
    if (size !== "thumb" || this.machineDescriptor?.(identity.machine)?.machine?.image_thumbnails_v1 !== true) {
      return super.image(value, id)
    }
    const key = identity.machine + "\u0000thumb\u0000" + id
    const cached = this._cachedImage(key)
    if (cached) return Promise.resolve(cached)
    return this._whenImageSlotFree(async () => {
      const filled = this._cachedImage(key)
      if (filled) return filled
      const body = await this._read(identity, "image", { id, size: "thumb" }, "image." + id + ".thumb")
      const answer = imageAnswerBytes(id, body)
      this._rememberImage(key, answer)
      return answer
    })
  }

  /** Transcript image bytes use the selected execution and the same r/ reply proof. */
  imageForGeneration(destination, id, signal, size) {
    const { machineID, sessionID, executionGeneration } = destination
    if (!machineID || !sessionID || !GENERATION.test(executionGeneration) ||
      typeof id !== "string" || !id || signal?.aborted) {
      return Promise.reject(refused("execution_target_required", "an exact Session image is required"))
    }
    const thumb = size === "thumb" && this.machineDescriptor?.(machineID)?.machine?.image_thumbnails_v1 === true
    const fields = { id, machine_id: machineID, expected_generation: executionGeneration, ...(thumb ? { size: "thumb" } : {}) }
    return this._whenImageSlotFree(() => this._read(
      { machine: machineID, session: sessionID }, "image", fields,
      pinnedReadName("image", fields), undefined, { signal })).catch((error) => {
      try {
        this.viewerEvents?.record?.("cloud.image.failed", { machine: machineID,
          code: error?.code ?? "read_failed", size: thumb ? "thumb" : "full" })
        this._scheduleViewerEvents?.()
      } catch { /* diagnostics cannot change the image failure */ }
      throw error
    })
  }

  /** The original detail panels share this exact read adapter. */
  readForGeneration(destination, type, fields = {}, signal) {
    const { machineID, sessionID, executionGeneration } = destination
    if (!machineID || !sessionID || !GENERATION.test(executionGeneration) || signal?.aborted ||
      typeof fields !== "object" || !fields || Array.isArray(fields)) {
      return Promise.reject(refused("execution_target_required", "an exact Session execution is required"))
    }
    const body = { ...fields, machine_id: machineID, expected_generation: executionGeneration }
    const answer = pinnedReadName(type, body)
    if (!pinnedRead(type, body) || !answer) {
      return Promise.reject(refused("read_only_channel", "the selected read is not supported on the pinned channel"))
    }
    return this._read({ machine: machineID, session: sessionID }, type, body, answer, undefined, { signal })
  }

  /** Keep distinct generations from joining the copied client's per-session read waiter. */
  transcriptForGeneration(destination, signal, held) {
    const { machineID, sessionID, executionGeneration } = destination
    if (!machineID || !sessionID || !/^[0-9a-f]{32}$/u.test(executionGeneration)) {
      return Promise.reject(refused("execution_target_required", "an exact Session execution is required"))
    }
    const key = JSON.stringify([machineID, sessionID])
    const previous = this.pinnedTranscriptFlights.get(key)
    if (previous?.generation === executionGeneration) return previous.promise
    const run = async () => {
      if (previous) await previous.promise.catch(() => undefined)
      if (signal?.aborted) throw refused("read_aborted", "the Session detail was closed")
      if (this.readWaiters?.has(machineID + "\u0000" + sessionID + "\u0000transcript")) {
        throw refused("cloud_read_busy", "an unpinned Session transcript read is already in flight")
      }
      const version = this.machineDescriptor?.(machineID)?.machine?.transcript_tools_v2 === true ? "tools-v2" : "tools-v1"
      const known = held?.id === sessionID && /^[0-9a-f]{64}$/u.test(held?.version) ? held.version : ""
      const delta = version === "tools-v2" && !!known && this.machineDescriptor?.(machineID)?.machine?.transcript_delta_v1 === true
      const fullView = version + (typeof DecompressionStream === "function" ? "z" : "")
      const view = (delta ? "delta-v1" + (typeof DecompressionStream === "function" ? "z" : "") : fullView) + (known ? ":" + known : "")
      const fields = { limit: 200, priority: "foreground", machine_id: machineID, expected_generation: executionGeneration }
      if (this.machineDescriptor?.(machineID)?.machine?.transcript_tools_v1 !== true) {
        return this._read({ machine: machineID, session: sessionID }, "transcript", fields, "transcript", undefined, { signal })
      }
      try {
        const page = await this._read({ machine: machineID, session: sessionID }, "transcript",
          { ...fields, view }, "transcript.view." + view, undefined, { signal })
        if (page?.unchanged === true) {
          if (!known || page.version !== known) throw refused("transcript_cursor_stale", "The held page no longer matches")
          return held
        }
        return page?.delta !== undefined || page?.baseVersion !== undefined
          ? applyTranscriptDelta(held, page, sessionID) : page
      } catch (error) {
        if (delta && ["transcript_cursor_stale", "invalid_transcript_view", "bad_payload"].includes(error?.code)) {
          return this._read({ machine: machineID, session: sessionID }, "transcript", { ...fields, view: fullView }, "transcript.view." + fullView, undefined, { signal })
        }
        if (version === "tools-v2" && error?.code === "invalid_transcript_view") {
          const fallback = "tools-v1" + (typeof DecompressionStream === "function" ? "z" : "")
          return this._read({ machine: machineID, session: sessionID }, "transcript", { ...fields, view: fallback }, "transcript.view." + fallback, undefined, { signal })
        }
        if (!["malformed_read", "cloud_read_timeout"].includes(error?.code)) throw error
        return this._read({ machine: machineID, session: sessionID }, "transcript", fields, "transcript", undefined, { signal })
      }
    }
    const promise = run().finally(() => {
      if (this.pinnedTranscriptFlights.get(key)?.promise === promise) this.pinnedTranscriptFlights.delete(key)
    })
    this.pinnedTranscriptFlights.set(key, { generation: executionGeneration, promise })
    return promise
  }

  /** Older pages keep their own read name, so they cannot settle the newest page's waiter. */
  transcriptPageForGeneration(destination, before, signal, revision) {
    const { machineID, sessionID, executionGeneration } = destination
    if (!machineID || !sessionID || !GENERATION.test(executionGeneration) ||
      !Number.isSafeInteger(before) || before < 1) {
      return Promise.reject(refused("execution_target_required", "an exact older Session page is required"))
    }
    if (signal?.aborted) return Promise.reject(refused("read_aborted", "the Session detail was closed"))
    const answer = "transcript.before." + before + (revision ? ".revision." + revision : "")
    const fields = { limit: 200, before, ...(revision ? { revision } : {}), priority: "foreground", machine_id: machineID,
      expected_generation: executionGeneration }
    if (this.machineDescriptor?.(machineID)?.machine?.transcript_tools_v1 !== true) {
      return this._read({ machine: machineID, session: sessionID }, "transcript", fields, answer, undefined, { signal })
    }
    const version = this.machineDescriptor?.(machineID)?.machine?.transcript_tools_v2 === true ? "tools-v2" : "tools-v1"
    const view = version + (typeof DecompressionStream === "function" ? "z" : "")
    return this._read({ machine: machineID, session: sessionID }, "transcript", { ...fields, view },
      answer + ".view." + view, undefined, { signal }).catch(error => {
        if (version === "tools-v2" && error?.code === "invalid_transcript_view") {
          const fallback = "tools-v1" + (typeof DecompressionStream === "function" ? "z" : "")
          return this._read({ machine: machineID, session: sessionID }, "transcript", { ...fields, view: fallback }, answer + ".view." + fallback, undefined, { signal })
        }
        if (!["malformed_read", "cloud_read_timeout"].includes(error?.code)) throw error
        return this._read({ machine: machineID, session: sessionID }, "transcript", fields, answer, undefined, { signal })
      })
  }
}
