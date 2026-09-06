import Foundation

/// The one spelling a live session may put in its own reply to show the person a stored image.
///
/// A session cannot send itself a ``ClawdlineSessionMessage``. The terminal transport has no
/// metadata channel — both Claude Code and Codex record injected input as a user turn — so typing
/// an envelope into your own terminal would be you handing yourself a new instruction, and
/// `POST /v1/orchestrator/messages` refuses `same_session` for exactly that reason. What a session
/// *can* do is say the marker in the reply it is already writing: its own CLI records that turn,
/// Clawdline reads the transcript back the way it always has, and nothing writes into `~/.claude/`.
///
/// Recognition is all-or-nothing, exactly as ``ClawdlineMessage``'s is. The wrapper is one fixed
/// spelling around one opaque artifact id and carries nothing else — no path, no bytes, no
/// dimensions, which are resolved from ``SessionImageArtifactStore`` the way the v2 envelope
/// resolves them. Anything that is not that exact shape stays visible as ordinary text, because a
/// marker that vanishes silently is indistinguishable from one that worked.
enum SessionImageMarker {
    /// `<clawdline-image id="…">`, and only that. One spelling, pinned by
    /// `Tests/SessionImageMarkerTests.swift`, so a session that copies what the route handed it
    /// never has to know how the tag is written.
    static let opening = "<clawdline-image id=\""
    static let closing = "\">"

    /// The literal a caller pastes into its own reply. Nil for anything that is not an opaque
    /// artifact id, so a route can never hand back a string this same file would refuse to read.
    static func marker(for id: String) -> String? {
        guard SessionImageArtifactStore.isArtifactID(id) else { return nil }
        return opening + id + closing
    }

    /// One assistant turn's prose after the markers it honoured have been lifted out of it.
    struct Reading: Equatable {
        /// What the reader sees. Every honoured marker is gone from it; every other occurrence of
        /// the tag — malformed, over the cap, quoted in prose — is still exactly where it was.
        let text: String
        /// The opaque ids, in the order they appeared.
        let ids: [String]
    }

    /// Split one turn's text into the prose to display and the artifact ids to attach.
    ///
    /// `limit` is honoured rather than enforced: markers past it stay as literal text instead of
    /// being dropped, so an assistant that pastes seven of them sees six pictures and one line of
    /// wire it can read, rather than six pictures and a seventh that disappeared.
    static func read(
        _ raw: String,
        limit: Int = SessionImageArtifactStore.productionPolicy.maxImagesPerMessage
    ) -> Reading {
        guard limit > 0, raw.contains(opening) else { return Reading(text: raw, ids: []) }
        let fenced = fencedRanges(in: raw)
        var text = ""
        var ids: [String] = []
        var cursor = raw.startIndex
        while let open = raw.range(of: opening, range: cursor..<raw.endIndex) {
            var honoured: (id: String, end: String.Index)?
            if ids.count < limit, !fenced.contains(where: { $0.contains(open.lowerBound) }),
               let close = raw.range(of: closing, range: open.upperBound..<raw.endIndex) {
                let candidate = String(raw[open.upperBound..<close.lowerBound])
                if SessionImageArtifactStore.isArtifactID(candidate) {
                    honoured = (candidate, close.upperBound)
                }
            }
            guard let honoured else {
                // Not a marker this reader honours. It stays exactly as written, and scanning
                // resumes just past the opening so a real marker later in the same turn is still
                // found rather than being swallowed by the one that was wrong.
                text += raw[cursor..<open.upperBound]
                cursor = open.upperBound
                continue
            }
            ids.append(honoured.id)
            let cut = removal(in: raw, from: open.lowerBound, to: honoured.end)
            text += raw[cursor..<cut.lowerBound]
            cursor = cut.upperBound
        }
        text += raw[cursor...]
        return Reading(text: text, ids: ids)
    }

    /// The fenced code blocks in one turn, so that a reply *about* this format is not read as a
    /// reply *using* it.
    ///
    /// Only line-anchored ``` and ~~~ fences, which is what an assistant showing somebody the
    /// wire actually writes. Inline spans are deliberately not parsed: a backtick run is ambiguous
    /// on its own line and the documented example carries `ARTIFACT_ID`, which already fails the
    /// opaque-id check, so the cheap half of this covers the case that occurs and the expensive
    /// half would mostly find new ways to make a real marker inert.
    ///
    /// An unclosed fence runs to the end of the turn, because that is what the reader sees too.
    private static func fencedRanges(in raw: String) -> [Range<String.Index>] {
        var ranges: [Range<String.Index>] = []
        var openedAt: String.Index?
        var openedWith: (Character, Int)?
        var lineStart = raw.startIndex
        while lineStart <= raw.endIndex {
            let lineEnd = raw[lineStart...].firstIndex(of: "\n") ?? raw.endIndex
            let line = raw[lineStart..<lineEnd]
            if let fence = fenceRun(in: line) {
                if let start = openedAt, let opener = openedWith {
                    if fence.0 == opener.0, fence.1 >= opener.1 {
                        ranges.append(start..<lineEnd)
                        openedAt = nil
                        openedWith = nil
                    }
                } else {
                    openedAt = lineStart
                    openedWith = fence
                }
            }
            if lineEnd == raw.endIndex { break }
            lineStart = raw.index(after: lineEnd)
        }
        if let start = openedAt { ranges.append(start..<raw.endIndex) }
        return ranges
    }

    /// The fence character and how many of it this line opens or closes with, after at most the
    /// leading whitespace a fence is allowed to carry.
    private static func fenceRun(in line: Substring) -> (Character, Int)? {
        var index = line.startIndex
        while index < line.endIndex, line[index] == " " || line[index] == "\t" {
            index = line.index(after: index)
        }
        guard index < line.endIndex else { return nil }
        let marker = line[index]
        guard marker == "`" || marker == "~" else { return nil }
        var run = 0
        while index < line.endIndex, line[index] == marker {
            run += 1
            index = line.index(after: index)
        }
        return run >= 3 ? (marker, run) : nil
    }

    /// What to take out for one honoured marker.
    ///
    /// A marker alone on its line takes the whole line with it, newline included: an assistant
    /// that puts the picture on a line of its own must not leave a blank gap where it was. A
    /// marker with prose beside it takes only itself, because the words around it are the
    /// sentence somebody wrote.
    private static func removal(in raw: String,
                                from start: String.Index,
                                to end: String.Index) -> Range<String.Index> {
        var lineStart = start
        while lineStart > raw.startIndex {
            let previous = raw.index(before: lineStart)
            let character = raw[previous]
            if character == " " || character == "\t" { lineStart = previous; continue }
            if character == "\n" { break }
            return start..<end
        }
        var lineEnd = end
        while lineEnd < raw.endIndex {
            let character = raw[lineEnd]
            if character == " " || character == "\t" {
                lineEnd = raw.index(after: lineEnd)
                continue
            }
            guard character == "\n" else { return start..<end }
            // The line and its newline. One blank line goes with it when the marker had one on
            // each side, so a picture between two paragraphs leaves one paragraph break rather
            // than two — which is the difference between a gap and a seam.
            var cut = raw.index(after: lineEnd)
            if lineStart > raw.startIndex, raw[raw.index(before: lineStart)] == "\n",
               cut < raw.endIndex, raw[cut] == "\n" {
                cut = raw.index(after: cut)
            }
            return lineStart..<cut
        }
        return lineStart..<lineEnd
    }

    /// The transcript-facing references for the ids one turn honoured.
    ///
    /// A marker carries an id and nothing else, so what it resolves to comes from the owned store
    /// and only from there. ``SessionImageArtifactStore/liveness(id:now:)`` is the cheap door —
    /// metadata and file existence, never the PNG — and anything it cannot call live resolves to
    /// ``expiredReference(id:)``, which both renderers already draw as the explicit expired tile.
    /// Standing in for a reference the store can no longer describe is deliberate: an entry that
    /// shows a picture today and prints raw wire text in a week is worse than one that says the
    /// picture is gone.
    static func artifacts(for ids: [String],
                          store: SessionImageArtifactStore,
                          now: Date) -> [SessionImageArtifact] {
        ids.map { id in
            if case .live(let artifact) = store.liveness(id: id, now: now) { return artifact }
            return expiredReference(id: id)
        }
    }

    /// A valid reference that is already past its expiry, carrying the id and nothing that
    /// pretends to describe an image: 1970 is not a plausible expiry and no renderer reads the
    /// dimensions of a tile it is not drawing.
    static func expiredReference(id: String) -> SessionImageArtifact {
        SessionImageArtifact(id: id, mediaType: "image/png", byteCount: 1,
                             width: 1, height: 1, expiresAt: 1)
    }
}
