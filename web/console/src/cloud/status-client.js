// The archived Cloud client is pinned byte for byte. This subclass accepts the
// new content-free ss/ channel without changing that archive or weakening its
// validation for any existing channel.
import { CatalogCloudClient } from "./refusal-client.js"
import { channelSegment, decodedChannelSegment } from "../legacy/js/net/client.js"
import { base64Bytes, bytesBase64, envelopeSigningBytes, importMasterSecret, sealEnvelope, validateEnvelope } from "../legacy/js/net/cloud-crypto.js"

const decoder = new TextDecoder()
const STATUS = /^ss\/([^/]+)\/([^/]+)$/u
const GENERATION = /^[0-9a-f]{32}$/u

function refused(code, message) {
  return Object.assign(new Error(message), { code })
}

function pinnedRead(type, body) {
  return (type === "info" && body?.parts === "full" || type === "transcript" || type === "skills" || type === "image" || type === "peer-inbox") &&
    body?.expected_generation !== undefined
}

function pinnedReplyKey(machine, session, read) {
  return machine + "\u0000" + session + "\u0000" + read
}

function pinnedReadName(type, body) {
  if (type === "info" && body?.parts === "full") return "info.full"
  if (type === "skills") return "skills"
  if (type === "image" && typeof body?.id === "string" && body.id) return "image." + body.id
  if (type === "transcript") {
    if (body?.before === undefined) return "transcript"
    return Number.isSafeInteger(body.before) && body.before > 0 ? "transcript.before." + body.before : null
  }
  if (type === "peer-inbox" && typeof body?.request === "string" && body.request) return "read:" + body.request
  return null
}

/** A retained or delayed t/ row must name the exact request before it can settle a pinned read. */
export function pinnedReplyMatches(payload, expected) {
  return !!payload && typeof payload === "object" && !Array.isArray(payload) &&
    payload.read === expected.read && payload.machine_id === expected.machineID &&
    payload.session_id === expected.sessionID && payload.expected_generation === expected.generation &&
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
  }

  // A status list must not cause the old, content-bearing sessions.snapshot
  // recovery. The ss/ inventory and row recovery is owned by its publisher.
  _recoverSessions() {}

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
    const ctlMachine = typeof frame?.ch === "string" ? /^ctl\/([^/]+)$/u.exec(frame.ch) : null
    const target = machine || (ctlMachine ? decodedChannelSegment(ctlMachine[1]) : null)
    const before = target ? this.machineOffline?.get(target) : null
    super._relayAnswered(isPinnedRead ? { ...frame, ch: "ctl/" + channelSegment(machine) } : frame, refusal)
    if (target && (frame?.status === "machine_offline" || frame?.status === "delivered") &&
      this.machineOffline?.get(target) !== before) {
      this._emit({ type: "machine_reachability", machine: target })
    }
  }

  /** The copied reader's local write flag predates the read-only r/ channel. */
  _read(value, type, extra, answer, timeoutMs, readOptions) {
    if (!pinnedRead(type, extra)) return super._read(value, type, extra, answer, timeoutMs, readOptions)
    if (!GENERATION.test(extra.expected_generation) || typeof extra.machine_id !== "string" || !extra.machine_id ||
      answer !== pinnedReadName(type, extra)) {
      return Promise.reject(refused("execution_target_required", "an exact Session execution is required"))
    }
    const key = pinnedReplyKey(extra.machine_id, value.session, answer)
    if (this.readWaiters.has(key)) {
      return Promise.reject(refused("cloud_read_busy", "another read owns this Session reply channel"))
    }
    const proof = { machineID: extra.machine_id, sessionID: value.session,
      generation: extra.expected_generation, read: answer, seq: null, waiters: null }
    this.pinnedReadProofs.set(key, proof)
    // The copied _read checks allowWrites synchronously before registering its
    // t/ waiter. It is a local guard for ctl/; this one narrow call publishes
    // on r/ and is authorized separately by the relay and machine.
    const previous = this.allowWrites
    this.allowWrites = true
    try {
      const promise = super._read(value, type, extra, answer, timeoutMs, readOptions)
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
    if (type === "peer-inbox" && !GENERATION.test(body?.expected_generation)) {
      throw refused("execution_target_required", "an exact Session execution is required")
    }
    if (!pinnedRead(type, body)) return super._publishCommand(machine, type, body, envelopeClass, pending)
    if (envelopeClass !== "ctl" || body.machine_id !== machine || typeof body.session !== "string" || !body.session ||
      !GENERATION.test(body.expected_generation) || !pinnedReadName(type, body)) {
      throw refused("execution_target_required", "an exact Session execution is required")
    }
    const unsupported = this._unsupportedRefusal(machine, type)
    if (unsupported) throw unsupported
    const offline = this._offlineRefusal(machine)
    if (offline) throw offline
    if (!this.ready) throw this.closedFailure || refused("offline", "the Cloud connection is not ready")
    if (!this.devicePrivateKey || !this.deviceID) throw refused("missing_device_key", "the viewer key is unavailable")
    const pairing = await this._outboundMachinePairing(machine)
    const sequence = await this.nextSequence(this.deviceID)
    if (!Number.isSafeInteger(sequence) || sequence < 0) throw refused("bad_sequence", "invalid envelope sequence")
    if (pending?.key) {
      const proof = this.pinnedReadProofs.get(pending.key)
      if (!proof || proof.waiters !== pending.waiters || proof.machineID !== machine ||
        proof.sessionID !== body.session || proof.generation !== body.expected_generation ||
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
    try { this._send({ type: "publish", envelope }) }
    catch (error) {
      this.pendingBySequence.delete(sequence)
      if (pending?.waiters) pending.waiters.ref = null
      throw this.closedFailure || error
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
    this.pinnedReadProofs?.delete(key)
    return super._settleRead(key, body, error)
  }

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
    if (event?.type === "orchestrator" && !event.statusOnly && event.machine) {
      const payload = event.data
      this.readContentCapabilities?.set(event.machine, {
        at: payload?.at, supported: payload?.machine?.read_content_v1 === true,
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

  /** Transcript image bytes use the selected execution and the same r/ reply proof. */
  imageForGeneration(destination, id, signal) {
    const { machineID, sessionID, executionGeneration } = destination
    if (!machineID || !sessionID || !GENERATION.test(executionGeneration) ||
      typeof id !== "string" || !id || signal?.aborted) {
      return Promise.reject(refused("execution_target_required", "an exact Session image is required"))
    }
    return this._read({ machine: machineID, session: sessionID }, "image", {
      id, machine_id: machineID, expected_generation: executionGeneration,
    }, "image." + id, undefined, { signal })
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

  /** Older pages keep their own read name, so they cannot settle the newest page's waiter. */
  transcriptPageForGeneration(destination, before, signal) {
    const { machineID, sessionID, executionGeneration } = destination
    if (!machineID || !sessionID || !GENERATION.test(executionGeneration) ||
      !Number.isSafeInteger(before) || before < 1) {
      return Promise.reject(refused("execution_target_required", "an exact older Session page is required"))
    }
    if (signal?.aborted) return Promise.reject(refused("read_aborted", "the Session detail was closed"))
    const answer = "transcript.before." + before
    return this._read({ machine: machineID, session: sessionID }, "transcript", {
      limit: 200, before, priority: "foreground", machine_id: machineID,
      expected_generation: executionGeneration,
    }, answer, undefined, { signal })
  }
}
