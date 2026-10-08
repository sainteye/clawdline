// The archived Cloud client is pinned byte for byte. This subclass accepts the
// new content-free ss/ channel without changing that archive or weakening its
// validation for any existing channel.
import { CatalogCloudClient } from "./refusal-client.js"
import { channelSegment, decodedChannelSegment } from "../legacy/js/net/client.js"
import { base64Bytes, envelopeSigningBytes, importMasterSecret, validateEnvelope } from "../legacy/js/net/cloud-crypto.js"

const decoder = new TextDecoder()
const STATUS = /^ss\/([^/]+)\/([^/]+)$/u

function refused(code, message) {
  return Object.assign(new Error(message), { code })
}

/** Validate every envelope field with the frozen validator, then verify the original signed ss/ bytes. */
export function statusChannel(envelope) {
  const match = typeof envelope?.ch === "string" ? STATUS.exec(envelope.ch) : null
  if (!match) return null
  validateEnvelope({ ...envelope, ch: `s/${match[1]}/${match[2]}` })
  return { machine: decodedChannelSegment(match[1]), session: decodedChannelSegment(match[2]) }
}

export class StatusCloudClient extends CatalogCloudClient {
  constructor(options) {
    super(options)
    // Retained rows from a previous connection may say "current" after that
    // connection has gone away. Wait for a fresh complete pass on this socket.
    this.statusSnapshots = new Map()
    this.statusSequences = new Map()
    this.statusRecoveries = new Map()
    this.openedDetails = new Set()
    this.detailSnapshots = new Map()
    this.pinnedInfoFlights = new Map()
    this.pinnedTranscriptFlights = new Map()
  }

  // A status list must not cause the old, content-bearing sessions.snapshot
  // recovery. The ss/ inventory and row recovery is owned by its publisher.
  _recoverSessions() {}

  openDetail(destination) {
    const channel = "s/" + channelSegment(destination.machineID) + "/" + channelSegment(destination.sessionID)
    this.openedDetails.add(channel)
    this.detailSnapshots.delete(destination.machineID + "\u0000" + destination.sessionID)
  }

  closeDetail(destination) {
    const channel = "s/" + channelSegment(destination.machineID) + "/" + channelSegment(destination.sessionID)
    this.openedDetails.delete(channel)
    this.detailSnapshots.delete(destination.machineID + "\u0000" + destination.sessionID)
  }

  _emit(event) {
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

  _subscriptionRefused(code) {
    const channels = this.subscriptionFrames[0]?.channels ?? []
    const result = super._subscriptionRefused(code)
    for (const channel of channels) this.statusRecoveries?.get(channel)?.finish(false)
    return result
  }

  cancelStatusRecoveries() {
    for (const recovery of [...this.statusRecoveries.values()]) recovery.finish(false)
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

  /** Keep distinct generations from joining the copied client's per-session read waiter. */
  transcriptForGeneration(destination, signal) {
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
      return this._read({ machine: machineID, session: sessionID }, "transcript", {
        limit: 200, priority: "foreground", machine_id: machineID,
        expected_generation: executionGeneration,
      }, "transcript", undefined, { signal })
    }
    const promise = run().finally(() => {
      if (this.pinnedTranscriptFlights.get(key)?.promise === promise) this.pinnedTranscriptFlights.delete(key)
    })
    this.pinnedTranscriptFlights.set(key, { generation: executionGeneration, promise })
    return promise
  }
}
