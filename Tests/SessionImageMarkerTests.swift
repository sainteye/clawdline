import AppKit
import Foundation

/// The road a session takes to put a picture on its own card.
///
/// `POST /v1/orchestrator/messages` cannot serve it — a session typing into its own terminal is
/// handing itself a new instruction, which is why that route refuses `same_session` and still
/// does. What replaces it is a marker the session writes into the reply it was already writing,
/// so Clawdline goes on only *reading* transcripts and gains no second source of truth.

/// One live artifact in an isolated store, and the store it lives in.
private struct MarkerFixture {
    let store: SessionImageArtifactStore
    let artifact: SessionImageArtifact
    let now: Date
}

/// A store of its own per group, so an expiry check in one cannot prune another's fixture.
private func markerFixture(_ name: String, ttl: TimeInterval = 3_600) -> MarkerFixture? {
    let root = isolatedTestSessionImagesDirectory
        .appendingPathComponent("marker-\(name)-\(UUID().uuidString)", isDirectory: true)
    guard (try? FileManager.default.createDirectory(at: root,
                                                    withIntermediateDirectories: true)) != nil,
          let png = exactPixelPNG(width: 8, height: 6, rgba: (244, 114, 182, 255))
    else { return nil }
    let source = root.appendingPathComponent("source.png")
    guard (try? png.write(to: source)) != nil else { return nil }
    let policy = SessionImageArtifactStore.Policy(
        ttl: ttl, maxCount: 8, maxTotalBytes: 1 << 20,
        maxInputBytes: 1 << 20, maxEncodedBytes: 1 << 20,
        maxDimension: 1_000, maxPixels: 1_000_000, tombstoneTTL: 60,
        maxMetadataCount: 32, maxImagesPerMessage: 6)
    let store = SessionImageArtifactStore(directory: root, policy: policy)
    let now = Date(timeIntervalSince1970: 1_800_200_000)
    guard let stored = try? store.importPaths([source.path], now: now).first else { return nil }
    return MarkerFixture(store: store, artifact: stored.artifact, now: now)
}

private let markerRowID = "3b1f9d5a-6c2e-4a71-9d84-0f2b7c6e5a10"
private let secondMarkerRowID = "7e4c2a18-93bd-4f60-8a25-1c9d4e7f0b36"

func runSessionImageMarkerTests() {
group("an image marker is one spelling and everything else stays literal text") {
    let id = markerRowID
    expect("the spelling a caller copies is pinned, not merely round-tripped",
           SessionImageMarker.marker(for: id) ?? "",
           "<clawdline-image id=\"\(id)\">")
    check("a marker is refused for anything that is not an opaque artifact id",
          SessionImageMarker.marker(for: "ARTIFACT_ID") == nil
            && SessionImageMarker.marker(for: id.uppercased()) == nil
            && SessionImageMarker.marker(for: "") == nil)

    guard let marker = SessionImageMarker.marker(for: id) else {
        check("the fixture marker exists", false)
        return
    }
    let alone = SessionImageMarker.read(marker)
    expect("a marker on its own is one id", alone.ids, [id])
    expect("and leaves no text behind it", alone.text, "")

    let wrapped = SessionImageMarker.read("Here it is.\n\n\(marker)\n\nAnd that is that.")
    expect("prose around a marker keeps its ids", wrapped.ids, [id])
    check("a marker alone on its line takes the line with it rather than leaving a gap",
          !wrapped.text.contains(marker) && wrapped.text.contains("Here it is.")
            && wrapped.text.contains("And that is that.")
            && !wrapped.text.contains("\n\n\n\n"))
    let inline = SessionImageMarker.read("before \(marker) after")
    expect("a marker with words beside it takes only itself", inline.text, "before  after")

    // Not the exact wrapper: every one of these stays where it was written, because a marker
    // that disappears silently is indistinguishable from one that worked.
    let malformed = "<clawdline-image id=\"ARTIFACT_ID\">"
    let malformedReading = SessionImageMarker.read(malformed)
    expect("a malformed id honours nothing", malformedReading.ids, [])
    expect("and is still visible as ordinary text", malformedReading.text, malformed)
    let uppercase = "<clawdline-image id=\"\(id.uppercased())\">"
    expect("an id that is not lower-case is not an artifact id",
           SessionImageMarker.read(uppercase).text, uppercase)
    let nearMiss = "<clawdline-image id='\(id)'> <clawdline-image ID=\"\(id)\"> " +
        "<clawdline-image id=\"\(id)\""
    expect("a near miss on quoting, case or closing is left alone",
           SessionImageMarker.read(nearMiss).text, nearMiss)
    let mention = "Write `<clawdline-image id=\"ARTIFACT_ID\">` to show a picture."
    expect("prose that mentions the tag is prose", SessionImageMarker.read(mention).text, mention)

    let fenced = "How it looks:\n\n```\n\(marker)\n```\n\nThat is the wire."
    let fencedReading = SessionImageMarker.read(fenced)
    expect("a marker inside a code fence is a quotation, not an attachment",
           fencedReading.ids, [])
    expect("and the fence keeps its contents byte for byte", fencedReading.text, fenced)
    let tildeFence = "~~~text\n\(marker)\n~~~\n\(marker)"
    let tildeReading = SessionImageMarker.read(tildeFence)
    expect("a tilde fence encloses too, and the marker after it is live",
           tildeReading.ids, [id])
    check("the enclosed copy is still there", tildeReading.text.contains(marker))
    let unclosed = "```\n\(marker)"
    expect("an unclosed fence runs to the end of the turn, as the reader sees it",
           SessionImageMarker.read(unclosed).ids, [])

    // The cap is honoured rather than enforced: what is over it stays readable.
    let cap = SessionImageArtifactStore.productionPolicy.maxImagesPerMessage
    let many = Array(repeating: marker, count: cap + 2).joined(separator: "\n")
    let capped = SessionImageMarker.read(many)
    expect("no more markers are honoured than one message may carry images", capped.ids.count, cap)
    expect("every marker past the cap stays as literal text",
           capped.text.components(separatedBy: marker).count - 1, 2)
    expect("a limit of zero honours nothing and hides nothing",
           SessionImageMarker.read(marker, limit: 0).text, marker)

    // Two real markers in one turn, in the order they were written.
    guard let second = SessionImageMarker.marker(for: secondMarkerRowID) else {
        check("the second fixture marker exists", false)
        return
    }
    let pair = SessionImageMarker.read("\(marker)\nbetween\n\(second)")
    expect("several markers keep the order they were written in",
           pair.ids, [id, secondMarkerRowID])
    expect("and the words between them survive", pair.text, "between")

    // A malformed marker must not swallow a real one that follows it.
    let afterBad = SessionImageMarker.read("<clawdline-image id=\"nope\">\n\(marker)")
    expect("scanning resumes past a marker it refused", afterBad.ids, [id])
    check("while the refused one is still readable",
          afterBad.text.contains("<clawdline-image id=\"nope\">"))
}

group("an assistant turn's image markers become that entry's own attachments") {
    guard let fixture = markerFixture("assistant") else {
        check("the assistant-turn fixture imports one artifact", false)
        return
    }
    guard let marker = SessionImageMarker.marker(for: fixture.artifact.id) else {
        check("the assistant-turn fixture has a marker", false)
        return
    }
    func claudeTurn(_ text: String) -> [Transcript.Entry] {
        let row: [String: Any] = [
            "type": "assistant",
            "message": ["role": "assistant", "content": [["type": "text", "text": text]]],
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: row),
              let line = String(data: data, encoding: .utf8) else { return [] }
        return Transcript.parse(line, assistant: .claude,
                                imageStore: fixture.store, now: fixture.now)
    }

    let one = claudeTurn("Here is the shot.\n\n\(marker)")
    expect("one assistant turn stays one entry", one.count, 1)
    expect("and it is the assistant's", one.first?.kind, .assistant)
    expect("the marker is resolved to the stored artifact",
           one.first?.artifacts.map(\.id) ?? [], [fixture.artifact.id])
    expect("with the store's own dimensions rather than anything from the text",
           one.first?.artifacts.first?.width, fixture.artifact.width)
    expect("and the prose keeps only the words", one.first?.text, "Here is the shot.")

    let onlyMarker = claudeTurn(marker)
    expect("a turn that is nothing but a picture is still a turn", onlyMarker.count, 1)
    expect("carrying the picture", onlyMarker.first?.artifacts.count, 1)
    expect("and no text", onlyMarker.first?.text, "")

    guard let second = SessionImageMarker.marker(for: secondMarkerRowID) else {
        check("the second marker exists", false)
        return
    }
    let several = claudeTurn("Two of them:\n\n\(marker)\n\(second)\n\nThat is all.")
    expect("several markers in one turn all attach", several.first?.artifacts.count, 2)
    expect("an id the store has never seen is not a live reference",
           several.first?.artifacts.last?.expiresAt, 1)
    check("and its prose is unharmed",
          (several.first?.text ?? "").contains("Two of them:")
            && (several.first?.text ?? "").contains("That is all."))

    // The person may quote the tag. A user turn is not where a marker is honoured.
    let userRow: [String: Any] = [
        "type": "user",
        "message": ["role": "user", "content": [["type": "text", "text": "look: \(marker)"]]],
    ]
    let userLine = (try? JSONSerialization.data(withJSONObject: userRow))
        .flatMap { String(data: $0, encoding: .utf8) } ?? ""
    let userEntries = Transcript.parse(userLine, assistant: .claude,
                                       imageStore: fixture.store, now: fixture.now)
    expect("a person's own turn attaches nothing", userEntries.first?.artifacts.count, 0)
    expect("and keeps what they typed", userEntries.first?.text, "look: \(marker)")

    // Codex writes its answers as AgentMessage items, and reads the same way.
    let codex = Codex.entries(ofItem: ["type": "AgentMessage",
                                       "content": [["type": "text", "text": marker]]],
                              at: nil, imageStore: fixture.store, now: fixture.now)
    expect("a Codex rollout answers the same way", codex.count, 1)
    expect("with the same resolved artifact",
           codex.first?.artifacts.map(\.id) ?? [], [fixture.artifact.id])

    // Expiry: the reference resolves, and both renderers draw the tile that says so.
    let afterTTL = fixture.now.addingTimeInterval(fixture.store.policy.ttl + 1)
    let expiredTurn = claudeTurn("Gone by now.\n\n\(marker)")
    expect("an expired artifact is still one attachment", expiredTurn.first?.artifacts.count, 1)
    let stale = Transcript.parse(
        (try? JSONSerialization.data(withJSONObject: [
            "type": "assistant",
            "message": ["role": "assistant",
                        "content": [["type": "text", "text": marker]]],
        ])).flatMap { String(data: $0, encoding: .utf8) } ?? "",
        assistant: .claude, imageStore: fixture.store, now: afterTTL)
    expect("past its expiry the reference no longer claims to be live",
           stale.first?.artifacts.first?.expiresAt, 1)

    let mono = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    let rendered = Transcript.render(one, size: 12, mono: mono,
                                     imageStore: fixture.store, now: fixture.now)
    var attachments = 0
    rendered.enumerateAttribute(.attachment,
                                in: NSRange(location: 0, length: rendered.length)) { value, _, _ in
        if value is NSTextAttachment { attachments += 1 }
    }
    expect("the native pane draws one thumbnail under the assistant's words", attachments, 1)
    check("and keeps the words", rendered.string.contains("Here is the shot."))
    check("the native pane never prints the id or the wire",
          !rendered.string.contains(fixture.artifact.id)
            && !rendered.string.contains(SessionImageMarker.opening))
    let expiredRender = Transcript.render(stale, size: 12, mono: mono,
                                          imageStore: fixture.store, now: afterTTL)
    check("an expired assistant attachment degrades to the explicit expired tile",
          expiredRender.string.contains(L.t.imageExpired))
    check("and draws no attachment at all",
          expiredRender.attribute(.attachment, at: 0, effectiveRange: nil) == nil)

    // Cache validation was written against `.message` entries and is asked about the rendered
    // text rather than about a role, which is why an assistant thumbnail is already covered.
    // Pinned rather than assumed: the answer decides whether a pruned image can stay on screen.
    check("a live assistant thumbnail keeps its cached render current",
          SessionImagePresentation.cacheIsCurrent(rendered, store: fixture.store,
                                                  now: fixture.now))
    check("and the same render stops being current once the artifact expires",
          !SessionImagePresentation.cacheIsCurrent(rendered, store: fixture.store,
                                                   now: afterTTL))

    // The Web contract needs no new field: `transcriptRows` already serialises `artifacts` for
    // any entry. Verified rather than assumed, because the whole design rests on it.
    let rows = RemoteServer.transcriptRows(one)
    expect("an assistant row is still an assistant row", rows.first?["role"] as? String,
           "assistant")
    let serialized = rows.first?["artifacts"] as? [[String: Any]] ?? []
    expect("and it carries its artifacts to the page", serialized.count, 1)
    check("as metadata only — no path, no url, no bytes",
          serialized.first?["id"] as? String == fixture.artifact.id
            && serialized.first?["path"] == nil && serialized.first?["url"] == nil
            && serialized.first?["marker"] == nil)
}

group("storing an image answers with its marker and leaves the message route alone") {
    let sourceSession = TargetSession(
        backend: .iterm, id: "MARKER-SOURCE", name: "marker source",
        tty: "/dev/ttys090", windowIndex: 0, tabIndex: 0,
        assistant: .claude, cwd: "/repo")
    RemoteServer.sessionPayloadForTesting = ([sourceSession], [sourceSession.id: .idle])
    var sent: [String] = []
    RemoteServer.terminalSendForTesting = { text, _ in sent.append(text); return nil }
    defer {
        RemoteServer.sessionPayloadForTesting = nil
        RemoteServer.terminalSendForTesting = nil
    }

    let root = isolatedTestSessionImagesDirectory
        .appendingPathComponent("route-\(UUID().uuidString)", isDirectory: true)
    guard (try? FileManager.default.createDirectory(at: root,
                                                    withIntermediateDirectories: true)) != nil,
          let png = exactPixelPNG(width: 5, height: 4, rgba: (16, 185, 129, 255)) else {
        check("the store-route fixture directory and PNG exist", false)
        return
    }
    let input = root.appendingPathComponent("shot.png")
    let unreadable = root.appendingPathComponent("not-an-image.png")
    guard (try? png.write(to: input)) != nil,
          (try? Data("this is not a raster".utf8).write(to: unreadable)) != nil else {
        check("the store-route fixtures can be written", false)
        return
    }

    func store(_ body: String, token: String = Orchestrator.dispatchToken(),
               key: String? = UUID().uuidString) -> RemoteServer.Response {
        var headers = ["X-Clawdline-Orchestrator": token]
        if let key { headers["Idempotency-Key"] = key }
        return RemoteServer.shared.route(remoteRequest(
            "POST", "/v1/artifacts/images", headers: headers, body: body))
    }
    func object(_ value: [String: Any]) -> String {
        (try? JSONSerialization.data(withJSONObject: value))
            .flatMap { String(data: $0, encoding: .utf8) } ?? ""
    }

    let accepted = store(object(["images": [["path": input.path]]]))
    expect("one local image is stored", accepted.status, 200)
    let answered = (try? JSONSerialization.jsonObject(with: accepted.body)) as? [String: Any]
    let artifact = (answered?["artifacts"] as? [[String: Any]])?.first
    let artifactID = artifact?["id"] as? String ?? ""
    check("the answer is the existing artifact object",
          artifact?["width"] as? Int == 5 && artifact?["height"] as? Int == 4
            && artifact?["media_type"] as? String == "image/png"
            && artifact?["path"] == nil && artifact?["url"] == nil)
    expect("with a ready-made marker beside it, so nobody builds the syntax by hand",
           artifact?["marker"] as? String ?? "", SessionImageMarker.marker(for: artifactID) ?? "")
    expect("and that marker reads back as exactly that id",
           SessionImageMarker.read(artifact?["marker"] as? String ?? "").ids, [artifactID])
    check("storing an image types nothing into any terminal", sent.isEmpty)

    expect("a request with no machine credential is refused",
           store(object(["images": [["path": input.path]]]), token: "not-the-token").status, 403)
    expect("and says so with the same typed code the other machine routes use",
           remoteErrorCode(store(object(["images": [["path": input.path]]]),
                                 token: "not-the-token")),
           "forbidden")
    expect("a write with no idempotency key is refused",
           store(object(["images": [["path": input.path]]]), key: nil).status, 400)

    expect("an extra top-level field is refused",
           store(object(["images": [["path": input.path]], "to_session": "%1"])).status, 400)
    expect("an empty images array is refused",
           store(object(["images": []])).status, 400)
    expect("a body with no images at all is refused",
           store(object([:])).status, 400)
    let oversized = Array(repeating: ["path": input.path],
                          count: SessionImageArtifactStore.productionPolicy
                            .maxImagesPerMessage + 1)
    expect("more images than one message may carry is refused",
           store(object(["images": oversized])).status, 400)
    let extraField = store(object(["images": [["path": input.path, "url": "https://e.test/x"]]]))
    expect("an extra field inside an image element is refused", extraField.status, 400)
    expect("and every one of those is the same typed body refusal",
           remoteErrorCode(extraField), "bad_request")

    let missing = store(object(["images": [["path": root.appendingPathComponent("gone.png").path]]]))
    expect("a file that is not there is a path refusal", missing.status, 400)
    expect("with the store's own typed code, unchanged",
           remoteErrorCode(missing), "invalid_image_path")
    let relative = store(object(["images": [["path": "shot.png"]]]))
    expect("a relative path is refused before anything is read", relative.status, 400)
    expect("as the same typed path refusal", remoteErrorCode(relative), "invalid_image_path")
    let notAnImage = store(object(["images": [["path": unreadable.path]]]))
    expect("a file that is not a raster is refused", notAnImage.status, 415)
    expect("with the store's typed unsupported code",
           remoteErrorCode(notAnImage), "unsupported_image")

    // The route beside it is unchanged, including the refusal this whole feature exists because of.
    let sameSession = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/messages",
        headers: ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken(),
                  "Idempotency-Key": UUID().uuidString],
        body: object(["from_session": sourceSession.id, "to_session": sourceSession.id,
                      "text": "to myself"])))
    expect("a session still may not send itself a message", sameSession.status, 409)
    expect("and the refusal is still same_session", remoteErrorCode(sameSession), "same_session")
    check("nothing was typed for it", sent.isEmpty)
    let messageBadImage = RemoteServer.shared.route(remoteRequest(
        "POST", "/v1/orchestrator/messages",
        headers: ["X-Clawdline-Orchestrator": Orchestrator.dispatchToken(),
                  "Idempotency-Key": UUID().uuidString],
        body: object(["from_session": sourceSession.id, "to_session": "%missing",
                      "text": "look", "images": [["path": input.path, "url": "x"]]])))
    expect("the message route's own image validation is unmoved", messageBadImage.status, 400)
    expect("and still refuses an extra image field the same way",
           remoteErrorCode(messageBadImage), "bad_request")
}
}
