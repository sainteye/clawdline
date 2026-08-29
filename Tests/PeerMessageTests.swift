import AppKit
import Carbon.HIToolbox
import Foundation
import SQLite3

// MARK: - Claude peer transcript messages

func peerTestLine(_ value: [String: Any]) -> String {
    let data = try! JSONSerialization.data(withJSONObject: value, options: [.sortedKeys])
    return String(decoding: data, as: UTF8.self)
}

func peerEnvelope(source: String = "payments-ops-12", mode: String = "prompting",
                  body: String) -> String {
    "<cross-session-message from=\"uds:/tmp/private.sock\" from-name=\"\(source)\" "
        + "from-mode=\"\(mode)\">\n\(body)\n</cross-session-message>"
}

func queuedPeer(_ body: String, at: String, source: String = "payments-ops-12",
                mode: String = "prompting") -> String {
    peerTestLine([
        "type": "queue-operation", "operation": "enqueue", "timestamp": at,
        "content": peerEnvelope(source: source, mode: mode, body: body),
    ])
}

func deliveredPeer(_ body: String, at: String, id: String,
                   source: String = "payments-ops-12", mode: String = "prompting",
                   sidechain: Bool = false) -> String {
    peerTestLine([
        "type": "user", "isMeta": true, "isSidechain": sidechain, "timestamp": at,
        "origin": [
            "kind": "peer", "name": source, "fromMode": mode, "body": body, "msg_id": id,
        ],
    ])
}























// MARK: - Peer messages and Clawdline notices in one transcript

// The two features were built apart and meet here for the first time. `.peer` is what another
// assistant session said to this one; `.notice` is what the app said to it about a task.
// Neither may claim the other's rows, in the parser, in the de-duplicator, or on the wire.

func noticeUserRow(_ wire: String, at: String) -> String {
    peerTestLine([
        "type": "user", "timestamp": at,
        "message": ["role": "user", "content": [["type": "text", "text": wire]]],
    ])
}

func noticeQueuedRow(_ wire: String, at: String) -> String {
    peerTestLine([
        "type": "queue-operation", "operation": "enqueue", "timestamp": at, "content": wire,
    ])
}

let coexistenceNotice = ClawdlineMessage.Notice(
    event: .taskFinished(
        task: .init(id: "9f1c0a3e-2b44-4d61-8f0e-71aa2c6d5b93", title: "Project portrait"),
        state: .success, audience: .root,
        resultPath: "/tmp/.clawdline/9f1c0a3e-2b44-4d61-8f0e-71aa2c6d5b93/result.json",
        outstanding: 0, claimsReleased: false, childMayStillWrite: false),
    body: "[clawdline] task 9f1c0a3e (Project portrait) finished: success"
        + " — read /tmp/.clawdline/9f1c0a3e-2b44-4d61-8f0e-71aa2c6d5b93/result.json")
let coexistenceWire = ClawdlineMessage.encode(coexistenceNotice)

func runPeerMessageTests() {
group("T1 queued peer envelopes become attributed peer entries") {
    let entries = Transcript.parse(
        queuedPeer("  please inspect the deploy  ", at: "2026-08-26T10:00:00.000Z"),
        assistant: .claude)
    let entry = entries.first
    check("T1 enqueue envelope fields",
          entries.count == 1 && entry?.kind == .peer
            && entry?.source == "payments-ops-12" && entry?.sourceMode == "prompting"
            && entry?.text == "please inspect the deploy")
}

group("T2 delivered meta peer rows use their structured origin") {
    let entries = Transcript.parse(
        deliveredPeer("  delivered body  ", at: "2026-08-26T10:00:01.000Z", id: "m-2",
                      source: "release-room", mode: "bypass"),
        assistant: .claude)
    let sidechainEntries = Transcript.parse(
        deliveredPeer("agent-only", at: "2026-08-26T10:00:02.000Z", id: "m-2-side",
                      sidechain: true),
        assistant: .claude)
    let entry = entries.first
    check("T2 delivered origin fields",
          entries.count == 1 && entry?.kind == .peer && entry?.source == "release-room"
            && entry?.sourceMode == "bypass" && entry?.text == "delivered body"
            && sidechainEntries.isEmpty)
}

group("T3 one enqueue and delivery receipt keeps the delivery") {
    let jsonl = [
        queuedPeer("same message", at: "2026-08-26T10:00:00.000Z", mode: "prompting"),
        deliveredPeer("same message", at: "2026-08-26T10:00:00.020Z", id: "m-3",
                      mode: "bypass"),
    ].joined(separator: "\n")
    let entries = Transcript.parse(jsonl, assistant: .claude)
    check("T3 nearby duplicate receipts",
          entries.count == 1 && entries.first?.kind == .peer
            && entries.first?.sourceMode == "bypass")
}

group("T4 delayed delivery still replaces its enqueue receipt") {
    let jsonl = [
        queuedPeer("busy turn message", at: "2026-08-26T10:00:00.000Z"),
        deliveredPeer("busy turn message", at: "2026-08-26T10:00:30.000Z", id: "m-4"),
    ].joined(separator: "\n")
    let entries = Transcript.parse(jsonl, assistant: .claude)
    check("T4 thirty-second duplicate receipts", entries.count == 1)
}

group("T5 distinct deliveries of the same text remain distinct") {
    let jsonl = [
        queuedPeer("please retry", at: "2026-08-26T10:00:00.000Z"),
        deliveredPeer("please retry", at: "2026-08-26T10:00:01.000Z", id: "m-5-a"),
        queuedPeer("please retry", at: "2026-08-26T10:05:00.000Z"),
        deliveredPeer("please retry", at: "2026-08-26T10:05:01.000Z", id: "m-5-b"),
    ].joined(separator: "\n")
    let entries = Transcript.parse(jsonl, assistant: .claude)
    check("T5 separate message ids", entries.count == 2 && entries.allSatisfy { $0.kind == .peer })
}

group("T6 an enqueue receipt survives without a delivery row") {
    let entries = Transcript.parse(
        queuedPeer("session ended before drain", at: "2026-08-26T10:00:00.000Z"),
        assistant: .claude)
    check("T6 enqueue-only receipt",
          entries.count == 1 && entries.first?.kind == .peer
            && entries.first?.text == "session ended before drain")
}

group("T7 peer envelope boundaries reject lookalikes and preserve Markdown") {
    func entry(_ raw: String) -> Transcript.Entry? {
        Transcript.parse(peerTestLine([
            "type": "queue-operation", "operation": "enqueue", "content": raw,
        ]), assistant: .claude).first
    }
    let lookalike = entry("<cross-session-messageX>no</cross-session-messageX>")
    let unclosed = entry("<cross-session-message from-name=\"x\">no")
    let empty = entry("<cross-session-message from-name=\"x\">  </cross-session-message>")
    let markdown = entry(peerEnvelope(body: "use `a < b && b > c`"))
    check("T7 envelope edge cases",
          lookalike?.kind != .peer && unclosed?.kind != .peer && empty?.kind != .peer
            && markdown?.kind == .peer && markdown?.text == "use `a < b && b > c`")
}

group("T8 peer envelope attributes decode entities in the safe order") {
    let raw = peerEnvelope(source: "a &amp;quot; b", body: "hello")
    let entry = Transcript.parse(peerTestLine([
        "type": "queue-operation", "operation": "enqueue", "content": raw,
    ]), assistant: .claude).first
    check("T8 attribute entities", entry?.source == "a &quot; b")
}

group("T9 a peer turn disproves ownership just like a user turn") {
    let delivered = deliveredPeer("external complete turn", at: "2026-08-26T10:00:00.000Z",
                                  id: "m-9")
    let queued = queuedPeer("external queued turn", at: "2026-08-26T10:00:01.000Z")
    check("T9 delivered peer counts as an external turn",
          Transcript.containsUserTurn(delivered, assistant: .claude))
    check("T9 queued peer counts as an external turn",
          Transcript.containsUserTurn(queued, assistant: .claude))
}

group("T10 peer entries serialize their role and omit an empty source") {
    let rows = RemoteServer.transcriptRows([
        Transcript.Entry(kind: .peer, text: "hello", tool: nil, time: nil,
                         source: "", sourceMode: "bypass"),
    ])
    let row = rows.first
    check("T10 peer wire row",
          rows.count == 1 && row?["role"] as? String == "peer" && row?["source"] == nil
            && row?["sourceMode"] as? String == "bypass")
}

group("T11 a newer repeated enqueue survives an older delivery") {
    let jsonl = [
        queuedPeer("status?", at: "2026-08-26T10:00:00.000Z"),
        deliveredPeer("status?", at: "2026-08-26T10:00:01.000Z", id: "m-11-a"),
        queuedPeer("status?", at: "2026-08-26T10:00:02.000Z"),
    ].joined(separator: "\n")
    let entries = Transcript.parse(jsonl, assistant: .claude)
    check("T11 delivered first send and queued second send both remain",
          entries.count == 2 && entries.first?.isPeerDelivery == true
            && entries.last?.isPeerDelivery == false)
}

group("T12 one transcript carries both a peer message and a Clawdline notice") {
    let jsonl = [
        queuedPeer("please inspect the deploy", at: "2026-08-26T10:00:00.000Z"),
        noticeUserRow(coexistenceWire, at: "2026-08-26T10:00:01.000Z"),
        deliveredPeer("and the rollback plan", at: "2026-08-26T10:00:02.000Z", id: "m-12",
                      source: "release-room", mode: "bypass"),
        noticeQueuedRow(coexistenceWire, at: "2026-08-26T10:00:03.000Z"),
    ].joined(separator: "\n")
    let entries = Transcript.parse(jsonl, assistant: .claude)
    expect("T12 four rows, four entries", entries.count, 4)
    check("T12 the queued cross-session envelope is a peer message, not a notice",
          entries.count == 4 && entries[0].kind == .peer
            && entries[0].text == "please inspect the deploy"
            && entries[0].source == "payments-ops-12" && entries[0].notice == nil)
    check("T12 the user-turn envelope is a notice, not a peer message",
          entries.count == 4 && entries[1].kind == .notice
            && entries[1].notice == coexistenceNotice
            && entries[1].text == coexistenceNotice.body
            && entries[1].source == nil && entries[1].isPeerDelivery == false)
    check("T12 a delivered peer row keeps its own source beside a notice",
          entries.count == 4 && entries[2].kind == .peer
            && entries[2].text == "and the rollback plan"
            && entries[2].source == "release-room" && entries[2].sourceMode == "bypass"
            && entries[2].isPeerDelivery == true)
    check("T12 a queued envelope that is a notice is a notice, not a peer message",
          entries.count == 4 && entries[3].kind == .notice
            && entries[3].notice == coexistenceNotice && entries[3].source == nil)

    let rows = RemoteServer.transcriptRows(entries)
    check("T12 the HTTP rows carry peer and notice on the right entries",
          rows.count == 4
            && rows[0]["role"] as? String == "peer"
            && rows[0]["source"] as? String == "payments-ops-12"
            && rows[0]["notice"] == nil
            && rows[1]["role"] as? String == "notice"
            && rows[1]["source"] == nil && rows[1]["sourceMode"] == nil
            && (rows[1]["notice"] as? [String: Any])?["kind"] as? String == "task_finished"
            && rows[2]["role"] as? String == "peer"
            && rows[2]["source"] as? String == "release-room"
            && rows[2]["notice"] == nil
            && rows[3]["role"] as? String == "notice")
}

group("T13 peer de-duplication leaves notices alone") {
    // Two identical notices are two things that happened: a task can finish, and the same
    // sentence can be typed again. The enqueue/delivery pairing exists for Claude Code's
    // double-write of one peer message and must not reach any other kind of entry.
    let twice = [
        noticeUserRow(coexistenceWire, at: "2026-08-26T10:00:00.000Z"),
        noticeQueuedRow(coexistenceWire, at: "2026-08-26T10:00:01.000Z"),
        noticeUserRow(coexistenceWire, at: "2026-08-26T10:00:02.000Z"),
    ].joined(separator: "\n")
    let repeated = Transcript.parse(twice, assistant: .claude)
    check("T13 three identical notices stay three entries",
          repeated.count == 3 && repeated.allSatisfy { $0.kind == .notice })

    // The sharpest version of the same rule: a peer message whose prose is exactly the
    // notice's fallback body. One is a person's words arriving from another session, the
    // other is the app speaking; sharing a string does not make them one entry.
    let sameWords = [
        queuedPeer(coexistenceNotice.body, at: "2026-08-26T10:00:03.000Z"),
        noticeUserRow(coexistenceWire, at: "2026-08-26T10:00:04.000Z"),
    ].joined(separator: "\n")
    let both = Transcript.parse(sameWords, assistant: .claude)
    check("T13 a peer message and a notice with identical text are two entries",
          both.count == 2 && both[0].kind == .peer && both[0].notice == nil
            && both[1].kind == .notice && both[1].notice == coexistenceNotice)

    // And the peer pairing still works while notices sit in the same window.
    let interleaved = [
        queuedPeer("status?", at: "2026-08-26T10:00:05.000Z"),
        noticeUserRow(coexistenceWire, at: "2026-08-26T10:00:06.000Z"),
        deliveredPeer("status?", at: "2026-08-26T10:00:07.000Z", id: "m-13"),
    ].joined(separator: "\n")
    let paired = Transcript.parse(interleaved, assistant: .claude)
    check("T13 a notice between an enqueue and its delivery does not save the receipt",
          paired.count == 2 && paired[0].kind == .notice
            && paired[1].kind == .peer && paired[1].isPeerDelivery == true)
}

group("T14 an envelope quoted inside the other envelope stays what it arrived as") {
    // A peer session that pastes a Clawdline envelope is still a peer session talking.
    let quotedInPeer = Transcript.parse(
        queuedPeer("look at this: " + coexistenceWire, at: "2026-08-26T10:00:00.000Z"),
        assistant: .claude)
    check("T14 a queued peer message quoting a notice is still a peer message",
          quotedInPeer.count == 1 && quotedInPeer[0].kind == .peer
            && quotedInPeer[0].text == "look at this: " + coexistenceWire
            && quotedInPeer[0].notice == nil)

    // Even when the quote is the whole message: `origin.kind == "peer"` is Claude Code saying
    // who sent it, which is a stronger fact than what the text happens to look like.
    let wholeQuote = Transcript.parse(
        deliveredPeer(coexistenceWire, at: "2026-08-26T10:00:01.000Z", id: "m-14"),
        assistant: .claude)
    check("T14 a delivered peer message that is nothing but a notice envelope stays a peer",
          wholeQuote.count == 1 && wholeQuote[0].kind == .peer
            && wholeQuote[0].text == coexistenceWire
            && wholeQuote[0].source == "payments-ops-12"
            && wholeQuote[0].notice == nil)

    // And the other way round: a notice whose readable body quotes a cross-session envelope
    // is still the app speaking, because the wrapper around the whole line is Clawdline's.
    let quoting = ClawdlineMessage.Notice(
        event: .taskFinished(
            task: .init(id: "3f9a21bc-0000-4000-8000-000000000000",
                        title: peerEnvelope(body: "not a peer message")),
            state: .failure, audience: .parent,
            resultPath: "/tmp/.clawdline/3f9a21bc-0000-4000-8000-000000000000/result.json",
            outstanding: 1, claimsReleased: false, childMayStillWrite: false),
        body: "[clawdline] a title that quotes <cross-session-message from-name=\"nobody\">")
    let quotingWire = ClawdlineMessage.encode(quoting)
    let asNotice = Transcript.parse(noticeUserRow(quotingWire, at: "2026-08-26T10:00:02.000Z"),
                                    assistant: .claude)
    check("T14 a notice quoting a cross-session envelope stays a notice",
          asNotice.count == 1 && asNotice[0].kind == .notice
            && asNotice[0].notice == quoting && asNotice[0].source == nil)
    let quotingRow = RemoteServer.transcriptRows(asNotice).first
    check("T14 and reaches the wire as a notice with no peer attribution",
          quotingRow?["role"] as? String == "notice" && quotingRow?["source"] == nil)
}

group("T15 a Clawdline notice does not disprove transcript ownership") {
    // The mirror of T9, and the arm that group does not cover. A peer turn is another session
    // talking; a notice is the app talking about a task, and the app types those into tabs it
    // has already identified — including a child's own tab once that child dispatches in turn.
    // So a notice is evidence of nothing, and `transcriptOwnership` must answer `.unavailable`
    // ("says nothing") rather than `.other` ("somebody else's conversation").
    let asUserTurn = noticeUserRow(coexistenceWire, at: "2026-08-26T10:00:00.000Z")
    let asQueued = noticeQueuedRow(coexistenceWire, at: "2026-08-26T10:00:01.000Z")
    check("T15 a delivered notice is not an external turn",
          !Transcript.containsUserTurn(asUserTurn, assistant: .claude))
    check("T15 a queued notice is not an external turn",
          !Transcript.containsUserTurn(asQueued, assistant: .claude))
    check("T15 but a peer turn beside it still is",
          Transcript.containsUserTurn(
              [asUserTurn, queuedPeer("who is on this?", at: "2026-08-26T10:00:02.000Z")]
                  .joined(separator: "\n"), assistant: .claude))
    check("T15 and so does a person's turn beside it",
          Transcript.containsUserTurn(
              [asUserTurn, peerTestLine([
                  "type": "user", "timestamp": "2026-08-26T10:00:03.000Z",
                  "message": ["role": "user", "content": [["type": "text", "text": "hello"]]],
              ])].joined(separator: "\n"), assistant: .claude))

    // Codex reaches the same answer through a different door: `firstUserMessage` looks for
    // `.user`, and a Codex user item that is exactly an envelope is parsed as `.notice`.
    let codexNotice = peerTestLine([
        "type": "event_msg",
        "payload": ["type": "item_completed",
                    "item": ["type": "UserMessage",
                             "content": [["type": "text", "text": coexistenceWire]]]],
    ])
    check("T15 a Codex notice item is not an external turn either",
          !Transcript.containsUserTurn(codexNotice, assistant: .codex))
    check("T15 while an ordinary Codex user item is",
          Transcript.containsUserTurn(peerTestLine([
              "type": "event_msg",
              "payload": ["type": "item_completed",
                          "item": ["type": "UserMessage",
                                   "content": [["type": "text", "text": "hello"]]]],
          ]), assistant: .codex))
}

group("T16 the two envelopes are mutually exclusive, so the order they are tried in is unobservable") {
    // The `queue-operation` branch tries the peer envelope first and the notice envelope second.
    // Nothing pins that order, and nothing can: no string satisfies both tests, so swapping the
    // two lines is invisible. This group pins the property that actually holds. Because the peer
    // test runs first, what `Transcript.parse` returns for a queued row IS the peer recogniser's
    // own answer — a tie would show up here as a row that is both.
    func queuedRaw(_ raw: String) -> String {
        peerTestLine([
            "type": "queue-operation", "operation": "enqueue",
            "timestamp": "2026-08-26T10:00:00.000Z", "content": raw,
        ])
    }
    let candidates: [(String, String)] = [
        ("a plain peer envelope", peerEnvelope(body: "deploy when you can")),
        ("a plain notice envelope", coexistenceWire),
        ("a peer envelope whose whole body is a notice", peerEnvelope(body: coexistenceWire)),
        ("a peer envelope on one line", "<cross-session-message from-name=\"ops\">"
            + coexistenceWire + "</cross-session-message>"),
        ("a notice with whitespace around it", "  " + coexistenceWire + "  "),
        ("a notice with a peer envelope glued after it",
         coexistenceWire + peerEnvelope(body: "and this")),
        ("a peer envelope with a notice glued before it",
         coexistenceWire + "\n" + peerEnvelope(body: "and this")),
    ]
    for (name, raw) in candidates {
        let parsed = Transcript.parse(queuedRaw(raw), assistant: .claude).first
        let peerWins = parsed?.kind == .peer
        let noticeAccepts = ClawdlineMessage.decode(raw) != nil
        check("T16 \(name) is not both a peer message and a notice", !(peerWins && noticeAccepts))
    }
    // And the structural reason, so a change to either wrapper that would make them overlap
    // fails here rather than silently making the order load-bearing.
    check("T16 the two recognisers require different opening tags",
          !ClawdlineMessage.opening.hasPrefix("<cross-session-message")
              && !"<cross-session-message".hasPrefix(ClawdlineMessage.opening))
}
}
