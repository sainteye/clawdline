import Foundation

/// **What a handoff request must contain, and who it must be from.**
///
/// Split out of `Sources/Orchestrator.swift` when the sender contract landed: that file is under a
/// zero-headroom ratchet, and this is the cohesive half the feature needed. Everything here is a
/// pure decision over values — no lock, no store, no terminal — so the whole contract can be
/// tested without a machine, and `Orchestrator` keeps the side effects.
///
/// The two halves are one subject on purpose. `handoffDraft` says whether a request is *shaped*
/// like a handoff; `handoffSenderVerdict` says whether it is *from* somebody. The 2026-09-04
/// incident is what happens when only the first question is asked: a handoff whose sender was the
/// machine's coordinator named nobody, nothing could notice, and the succession sequence that
/// exists to move that role was skipped entirely.
extension Orchestrator {

    // MARK: - What a request must contain

    struct HandoffDraft: Equatable {
        let id: String
        let projectDir: String
        let assistant: Assistant
        let model: String?
        let title: String?
        let fromSession: String?
        let coordinatorPlainHandoff: Bool
    }

    enum HandoffDraftOutcome: Equatable {
        case ok(HandoffDraft)
        case bad(String)

        var isBad: Bool {
            if case .bad = self { return true }
            return false
        }
    }

    static func handoffDraft(from obj: [String: Any],
                             isDirectory: (String) -> Bool = StartPoints.isDirectory,
                             packageIsReady: ((String) -> Bool)? = nil)
        -> HandoffDraftOutcome {
        guard let id = obj["handoff_id"] as? String, OrchestratorDraft.isTaskID(id) else {
            return .bad("handoff_id must be a lowercase UUID")
        }
        guard let projectDir = obj["project_dir"] as? String,
              StartPoints.usable(projectDir), isDirectory(projectDir) else {
            return .bad("project_dir must be an absolute path to a directory")
        }
        let assistant: Assistant
        if let raw = obj["assistant"] {
            guard let name = raw as? String, let selected = Assistant(rawValue: name) else {
                return .bad("assistant must be claude or codex")
            }
            assistant = selected
        } else {
            assistant = .claude
        }
        var model: String?
        if let raw = obj["model"] {
            guard let name = raw as? String, StartPoints.modelName(name) == name else {
                return .bad("model must be a model name: lower-case letters, digits, . _ -, "
                          + "at most 64 characters")
            }
            model = name
        }
        func optionalString(_ key: String) -> String?? {
            guard let raw = obj[key] else { return .some(nil) }
            guard let value = raw as? String, value.count <= 200 else { return nil }
            return .some(.some(value))
        }
        guard let title = optionalString("title") else {
            return .bad("title must be a string of at most 200 characters")
        }
        guard let fromSession = optionalString("from_session") else {
            return .bad("from_session must be a string of at most 200 characters")
        }
        // A field whose whole meaning is *I decided this* has no useful false, so `false` is
        // refused rather than quietly treated as absent: a caller that typed it meant something,
        // and the two spellings of "no" must not be a place where intent goes missing.
        var coordinatorPlainHandoff = false
        if obj["coordinator_plain_handoff"] != nil {
            guard handoffWaiverAsserted(obj["coordinator_plain_handoff"]) else {
                return .bad("coordinator_plain_handoff is set to true or left out entirely")
            }
            coordinatorPlainHandoff = true
        }
        let ready = packageIsReady?(id) ?? handoffPackageReady(id: id)
        guard ready else {
            return .bad("No non-empty regular handoff.md under "
                      + "/tmp/.clawdline/handoffs/<handoff_id>/")
        }
        return .ok(HandoffDraft(id: id, projectDir: projectDir, assistant: assistant,
                                model: model, title: title, fromSession: fromSession,
                                coordinatorPlainHandoff: coordinatorPlainHandoff))
    }

    /// Whether `coordinator_plain_handoff` is the JSON boolean `true` itself, and not a value that
    /// merely casts to it.
    ///
    /// `as? Bool` is not that question. `JSONSerialization` returns `NSNumber` for both `true` and
    /// `1`, and `NSNumber(1) as? Bool` is `true` in Swift — so the plain cast reads
    /// `"coordinator_plain_handoff": 1` as the waiver, and a client that serialises booleans as
    /// 0 and 1 could waive the one refusal protecting the coordinator binding by accident. Three
    /// surfaces promise this field must be exactly `true`, and the whole reason it exists is that
    /// somebody *decided* something; a decision inferred from a number is not one. `CFGetTypeID`
    /// is how the rest of this tree separates a JSON boolean from a JSON number
    /// (`Sources/Settings.swift`, `Sources/SessionImageArtifact.swift`), asked here in the
    /// direction that keeps a caller building the dictionary in-process — with a native `Bool`
    /// rather than a bridged one — reading as the assertion it is.
    static func handoffWaiverAsserted(_ raw: Any?) -> Bool {
        guard let raw else { return false }
        if let number = raw as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID() {
            return false
        }
        return raw as? Bool == true
    }

    // MARK: - Who sent a handoff

    /// Everything the sender contract on `POST /v1/orchestrator/handoffs` is decided from, taken
    /// from **one** observation so the two halves cannot describe two different moments: which
    /// assistant sessions exist right now, and which of them the coordinator record is bound to.
    ///
    /// `coordinatorInspection` is the whole body of `Coordinator.inspection(liveSessions:bearings:)`
    /// — the reader `GET /v1/orchestrator/coordinator` already serves — rather than a second
    /// reader of the store or a cached projection of it. `nil` means no reading at all, which is
    /// one of the three answers and not the same as "no coordinator".
    struct HandoffSenderEvidence {
        /// The assistant sessions of the current inventory, with their process-bound identities.
        let identities: [SessionWorkIdentity]
        /// A complete scan that actually happened. Both halves matter: an inventory nobody has
        /// read yet is "complete" in the freshness sense and still names no sessions, so trusting
        /// it would answer *no such session* to a question nobody has looked at.
        let inventoryCurrent: Bool
        let coordinatorInspection: [String: Any]?
    }

    /// What `from_session` on `POST /v1/orchestrator/handoffs` resolved to. Every case is one
    /// answer with one next move; the refusals deliberately do not share a code, because a
    /// caller that sent the wrong *kind* of id has a different thing to do next from one that
    /// sent none.
    ///
    /// The three "cannot tell" cases — `inventoryIncomplete`, `coordinatorStoreUnreadable`,
    /// `coordinatorLivenessUnknown` — are refusals, and that asymmetry is the point of the type.
    /// A handoff refused because the machine could not see costs a retry a few seconds later; a
    /// handoff *allowed* because the machine could not see is the crown moving with no succession
    /// behind it, which is the defect this contract exists to remove.
    enum HandoffSenderVerdict: Equatable {
        /// A single current assistant session answers to that spelling, and it does not hold the
        /// coordinator binding.
        case accepted(sessionID: String)
        /// It does hold the binding, and the request said so on purpose.
        case waived(sessionID: String, coordinatorID: String, generation: Int)
        case missing
        case malformed
        /// A well-formed id from a namespace this machine does not index — an Anthropic cloud
        /// session id, `session_01…`, which names a conversation on claude.ai and nothing here.
        case wrongNamespace
        case notFound
        case ambiguous(matches: Int)
        case inventoryIncomplete
        case coordinatorStoreUnreadable
        case coordinatorLivenessUnknown
        case successionRequired(sessionID: String, coordinatorID: String, generation: Int)

        /// The route to use instead of a plain handoff when the sender wears the crown.
        static let successionRoute = "POST /v1/orchestrator/coordinator/successions"

        /// `nil` when the handoff may proceed. The refusal bodies carry the caller's next move,
        /// and `successionRequired` carries the compare-and-swap fields under the exact key
        /// names ``successionRoute`` reads, so nothing has to be looked up a second time.
        var refusal: Reply? {
            switch self {
            case .accepted, .waived:
                return nil
            case .missing:
                return .refused(400, "from_session_required",
                                "from_session names the session this handoff is sent from and is "
                                  + "required. Resolve this session's own id through "
                                  + "GET /v1/orchestrator/whoami.")
            case .malformed:
                return .refused(400, "from_session_invalid",
                                "from_session must be one session id as a string of at most 200 "
                                  + "characters — the id itself, not a label or an object.")
            case .wrongNamespace:
                return .refused(404, "from_session_wrong_namespace",
                                "A session_01… id names a conversation on claude.ai, not a "
                                  + "session on this Mac. Send this session's terminal-neutral "
                                  + "id or its process-bound conversation id; "
                                  + "GET /v1/orchestrator/whoami resolves both.")
            case .notFound:
                return .refused(404, "sender_not_found",
                                "No current assistant session answers to that id, in either the "
                                  + "terminal-neutral or the conversation namespace.")
            case .ambiguous(let matches):
                return .refused(status: 409, code: "sender_ambiguous",
                                message: "More than one current session answers to that id; "
                                  + "name the terminal-neutral id, which is unique.",
                                extra: ["matches": matches])
            case .inventoryIncomplete:
                return .refused(409, "sender_unverifiable",
                                "This Mac has no complete current reading of its sessions, so "
                                  + "the sender cannot be proved. Retry after the next scan.")
            case .coordinatorStoreUnreadable:
                return .refused(409, "coordinator_store_unreadable",
                                "The coordinator record cannot be read, so whether this sender "
                                  + "is the coordinator is unknown. GET /v1/orchestrator/"
                                  + "coordinator says what state the store is in.")
            case .coordinatorLivenessUnknown:
                return .refused(409, "coordinator_liveness_unknown",
                                "A coordinator is registered and this reading cannot say which "
                                  + "process it is bound to. Retry after a complete scan.")
            case .successionRequired(let sessionID, let coordinatorID, let generation):
                return .refused(
                    status: 409, code: "succession_required",
                    message: "This session holds the machine coordinator binding, and moving that "
                      + "role is \(HandoffSenderVerdict.successionRoute). Send a plain handoff "
                      + "from the coordinator only with coordinator_plain_handoff:true, which "
                      + "says the crown stays where it is.",
                    extra: ["route": HandoffSenderVerdict.successionRoute,
                            "coordinator_id": coordinatorID,
                            "expected_generation": generation,
                            "sender_session_id": sessionID])
            }
        }
    }

    /// Whether an unresolved spelling is an Anthropic cloud session id — the `session_01…` in a
    /// `https://claude.ai/code/session_01…` link, and the value one of this machine's two stored
    /// envelopes actually carried.
    ///
    /// **It is asked only after resolution has already failed**, so it can never turn an id that
    /// resolves into a refusal. All it does is replace *nothing here answers to that* with *that
    /// is an id from another system*, which are different things to do next.
    static func handoffSenderIsCloudSessionID(_ value: String) -> Bool {
        guard value.hasPrefix("session_") else { return false }
        let rest = value.dropFirst("session_".count)
        guard !rest.isEmpty else { return false }
        return rest.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber) }
    }

    /// The sender contract of `POST /v1/orchestrator/handoffs`, as one pure decision.
    ///
    /// Resolution reuses ``handoffSource(_:matches:)`` — the comparison the receipt path already
    /// makes — rather than adding a third matcher: exactly the watched terminal-neutral id or the
    /// process-bound conversation id, compared whole, with no prefix, title or tty fallback.
    /// Anything but exactly one match fails closed, the way
    /// ``RemoteServer/sessionMessageSource(withID:among:conversationID:)`` does for a relay.
    ///
    /// The coordinator half compares the resolved sender's terminal id against
    /// `coordinator.session.id`. That is an exact comparison rather than a name match:
    /// `Coordinator` publishes `status:"online"` only when a live session agrees with the stored
    /// record on assistant, terminal, tty, pid, process start and conversation, and that
    /// comparison requires the terminal ids to be equal — so an online binding's published id
    /// names one proved process.
    ///
    /// An **offline** coordinator is accepted, deliberately, but only where this reading can say
    /// that word means what it sounds like. The refusal exists to stop the crown moving by
    /// accident, and a binding whose process is gone is not a crown this letter can move: the
    /// receiver's route back is `rebind`, and the succession route named by the refusal would
    /// itself refuse a sender that is not currently online. Pointing a caller at a route that
    /// cannot take it would be a worse answer than allowing an ordinary handoff.
    ///
    /// **`status:"offline"` is not that fact on its own.** `Coordinator` publishes it whenever a
    /// current observation holds no row agreeing with the record on assistant, terminal, tty, pid,
    /// process start *and* conversation — which covers both *the process is gone* and *this
    /// reading could not prove the row it can see is the one*. A live Claude session whose
    /// transcript could not be located this round has no conversation id, so it stops agreeing
    /// while it is still very much alive, and reading that as offline would accept exactly the
    /// plain handoff of 2026-09-04 from the code written to refuse it. So the word is believed
    /// only with positive evidence of absence: the bound terminal id missing from this reading
    /// altogether. Present but unmatched is the third answer — `coordinatorLivenessUnknown`,
    /// refuse and retry — because *cannot tell* has never been *allow*. Teaching `Coordinator` to
    /// publish a fourth word for the middle case belongs to that projection, not to this decision.
    static func handoffSenderVerdict(_ obj: [String: Any],
                                     evidence: HandoffSenderEvidence) -> HandoffSenderVerdict {
        guard let raw = obj["from_session"] else { return .missing }
        guard let source = raw as? String, source.count <= 200 else { return .malformed }
        guard !source.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return .missing
        }
        // Resolution is only as good as the reading under it, so an incomplete inventory is
        // answered before the search rather than after: "nothing matched" and "I could not look"
        // are the two states a match count cannot tell apart.
        guard evidence.inventoryCurrent else { return .inventoryIncomplete }
        let matches = evidence.identities.filter {
            $0.assistant != nil && handoffSource(source, matches: $0)
        }
        guard let sender = matches.first else {
            return handoffSenderIsCloudSessionID(source) ? .wrongNamespace : .notFound
        }
        guard matches.count == 1 else { return .ambiguous(matches: matches.count) }

        guard let inspection = evidence.coordinatorInspection else {
            return .coordinatorStoreUnreadable
        }
        // `coordinator.configured:false` renders an absent store and a corrupt one identically,
        // and here those are opposite answers — nothing to move, versus cannot see whether there
        // is anything to move. `registration.state` is the projection that separates them.
        switch (inspection["registration"] as? [String: Any])?["state"] as? String {
        case "available": return .accepted(sessionID: sender.terminalID)
        case "configured": break
        default: return .coordinatorStoreUnreadable
        }
        guard let coordinator = inspection["coordinator"] as? [String: Any],
              coordinator["configured"] as? Bool == true,
              let coordinatorID = coordinator["id"] as? String,
              let generation = coordinator["generation"] as? Int,
              let status = coordinator["status"] as? String,
              let bound = (coordinator["session"] as? [String: Any])?["id"] as? String else {
            return .coordinatorStoreUnreadable
        }
        switch status {
        case "offline":
            // Only an absence this reading can actually show. A row still bearing the bound
            // terminal id means the machine has something there it could not match, not that the
            // binding's process ended.
            guard !evidence.identities.contains(where: { $0.terminalID == bound }) else {
                return .coordinatorLivenessUnknown
            }
            return .accepted(sessionID: sender.terminalID)
        case "online": break
        // "unknown", and any word a later build adds that this one has not been taught.
        default: return .coordinatorLivenessUnknown
        }
        guard bound == sender.terminalID else { return .accepted(sessionID: sender.terminalID) }
        guard handoffWaiverAsserted(obj["coordinator_plain_handoff"]) else {
            return .successionRequired(sessionID: sender.terminalID,
                                       coordinatorID: coordinatorID, generation: generation)
        }
        return .waived(sessionID: sender.terminalID, coordinatorID: coordinatorID,
                       generation: generation)
    }

}

/// The machine-reading half, kept beside the decision it feeds rather than inside `RemoteServer`,
/// which is under a ceiling of its own. `CoordinatorSuccession.swift` puts its own
/// `coordinatorInspection` here for the same reason.
extension RemoteServer {
    /// The two halves of the handoff sender contract, read from one observation.
    ///
    /// `coordinatorObservation()` is the reading `GET /v1/orchestrator/coordinator` is built from,
    /// so the sessions the sender is resolved among and the binding it is compared against are the
    /// same moment. Taking them separately would make the refusal a statement about two machines,
    /// either of which could have moved in between.
    ///
    /// Freshness is `sessionsFresh` **and** an observation time. `coordinatorSessionsFresh` calls
    /// a reading that never happened fresh, which is right for the coordinator projection — it
    /// falls through to `status:"unknown"` — and wrong for a match count, where an inventory
    /// nobody has read yet would answer *no such session* to a question nobody has looked at.
    func handoffSenderEvidence() -> Orchestrator.HandoffSenderEvidence {
        let observation = coordinatorObservation()
        return Orchestrator.HandoffSenderEvidence(
            identities: observation.sessions.map(\.identity),
            inventoryCurrent: observation.sessionsFresh && observation.sessionsObservedAt != nil,
            coordinatorInspection: coordinatorInspection(observation))
    }
}
