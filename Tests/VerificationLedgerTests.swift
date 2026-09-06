import Foundation

// **The verification ledger's read.** Four groups, and every one of them is about the same
// sentence: an unknown is not a zero. The route joins four receipt tables to the interval rows
// beside them, and the three answers it has to keep apart — a record that says something, a
// record this Mac does not hold, and a record that exists and cannot be read as a number — are
// one grey rectangle on a screen if any reader lets them become one.
//
// Nothing here opens a store. `VerificationLedgerService` takes its three reads as closures, so
// what the store holds is stated in the test and the arithmetic is the only thing under
// examination.

/// One stored interval as it lands for a dispatched task: a graph, a kind, and four measured
/// parts unless the caller says otherwise.
func ledgerIntervalRow(_ key: String, graph: String?, task: String?, kind: String?,
                       counts: UsageLedger.Counts = UsageLedger.Counts(
                        inputNew: 10, output: 20, cacheRead: 30, cacheWrite: 40),
                       reasons: [String] = [],
                       at: Date = Date(timeIntervalSince1970: 1_788_000_000)) -> UsageLedger.Row {
    var row = UsageLedger.Row()
    row.intervalKey = key
    row.assistant = "claude"
    row.sessionID = "session-" + key
    row.boundaryKind = "task"
    row.boundaryID = task ?? key
    row.origin = "dispatch"
    row.taskID = task
    row.graphID = graph
    row.kindRaw = kind
    row.model = "claude-opus-5"
    row.counts = counts
    row.total = counts.total
    row.coverageReasons = reasons
    row.costBasis = "unknown"
    row.coverage = counts.isEmpty ? "missing" : "complete"
    row.startedAt = at
    row.endedAt = at.addingTimeInterval(60)
    row.localDay = UsageLedger.localDay(of: at)
    return row
}

func ledgerReviewReceipt(task: String, graph: String?, verdict: String,
                         axes: [UsageLedger.StoredReviewAxis] = [],
                         findings: [(String, String, String)] = [])
    -> UsageLedger.StoredReviewReceipt {
    UsageLedger.StoredReviewReceipt(
        taskID: task, graphID: graph, nodeID: nil, projectKey: nil, kindRaw: "code-review",
        taskState: "success", verdict: verdict, axes: axes,
        findings: findings.map { id, severity, summary in
            UsageLedger.StoredReviewFinding(
                taskID: task, graphID: graph, axis: "specification", findingID: id,
                severity: severity, summary: summary, evidence: ["Sources/X.swift:1"])
        },
        recordedAt: Date(timeIntervalSince1970: 1_788_000_100))
}

func ledgerVerificationReceipt(task: String, graph: String?, runs: Int, seconds: Int,
                               last: String, scope: String = "swift suite")
    -> UsageLedger.StoredVerificationReceipt {
    UsageLedger.StoredVerificationReceipt(
        taskID: task, graphID: graph, nodeID: nil, projectKey: nil, kindRaw: "custom",
        taskState: "success", runs: runs, seconds: seconds, last: last, scope: scope,
        recordedAt: Date(timeIntervalSince1970: 1_788_000_100))
}

/// A service whose whole store is the three arrays handed in.
func ledgerService(rows: [UsageLedger.Row] = [],
                   reviews: [UsageLedger.StoredReviewReceipt] = [],
                   verifications: [UsageLedger.StoredVerificationReceipt] = [])
    -> VerificationLedgerService {
    VerificationLedgerService(
        readRows: { _ in
            UsageLedger.AnalyticsRead(rows: rows, corrections: 0, latestLedgerObservation: nil,
                                      acceptedFeatures: [:], acceptedProjects: [:])
        },
        readReviews: { _ in reviews },
        readVerifications: { _ in verifications })
}

/// One Feature out of a list payload, by its graph id.
func ledgerFeature(_ payload: [String: Any]?, _ graphID: String) -> [String: Any]? {
    (payload?["features"] as? [[String: Any]])?.first { $0["graphId"] as? String == graphID }
}

func ledgerTokens(_ feature: [String: Any]?, _ role: String) -> [String: Any]? {
    (feature?["tokens"] as? [String: Any])?[role] as? [String: Any]
}

func runVerificationLedgerTests() {
group("a bucket with nothing in it is not a bucket that spent nothing") {
    // The whole line exists because of a page that would have drawn 48 graphs as `0`. These are
    // the three answers it has to keep apart, asked of the type that decides them.
    var empty = VerificationLedgerService.TokenReading()
    expect("no rows at all is absent", empty.presence, .absent)
    let payloadOfEmpty = VerificationLedgerService.payload(of: empty)
    check("and absent carries no measured figure at all",
          payloadOfEmpty["measured"] is NSNull && payloadOfEmpty["total"] is NSNull,
          "measured=\(payloadOfEmpty["measured"] ?? "nil"), total=\(payloadOfEmpty["total"] ?? "nil")")
    expect("while how many records there are is a fact this side really holds",
           payloadOfEmpty["rows"] as? Int, 0)

    // A row whose source measured nothing at all. `Measurement.unknown` is what says so, and the
    // bucket reports it as a bucket that was spent in and cannot be counted — not as an empty one
    // and never as zero.
    var unmeasured = VerificationLedgerService.TokenReading()
    unmeasured.add(ledgerIntervalRow("u1", graph: "g", task: "t1", kind: "custom",
                                     counts: UsageLedger.Counts()))
    expect("a row that measured nothing is unknown, not absent", unmeasured.presence, .unknown)
    expect("and it is still one row", unmeasured.rows, 1)
    let payloadOfUnknown = VerificationLedgerService.payload(of: unmeasured)
    check("unknown carries no number either",
          payloadOfUnknown["measured"] is NSNull && payloadOfUnknown["total"] is NSNull,
          "measured=\(payloadOfUnknown["measured"] ?? "nil")")
    expect("and says how many rows could not be measured",
           payloadOfUnknown["unknownRows"] as? Int, 1)

    var measured = VerificationLedgerService.TokenReading()
    measured.add(ledgerIntervalRow("m1", graph: "g", task: "t1", kind: "custom"))
    measured.add(ledgerIntervalRow("m2", graph: "g", task: "t1", kind: "custom"))
    expect("two whole rows are present", measured.presence, .present)
    expect("with a strict total", measured.total, 200)
    expect("and a floor that agrees with it", measured.measured, 200)

    // **The floor and the total are two quantities and the page is handed both.** A bucket
    // holding one whole row and one three-quarters-measured row spent at least the sum of what
    // was measured, and has no total at all.
    var partial = VerificationLedgerService.TokenReading()
    partial.add(ledgerIntervalRow("p1", graph: "g", task: "t1", kind: "custom"))
    partial.add(ledgerIntervalRow("p2", graph: "g", task: "t1", kind: "custom",
                                  counts: UsageLedger.Counts(inputNew: 1, output: 2,
                                                             cacheRead: nil, cacheWrite: 4)))
    expect("a partly-measured bucket is present, because something was measured",
           partial.presence, .present)
    expect("its floor is what was measured", partial.measured, 107)
    check("and it has no total", partial.total == nil, "total=\(String(describing: partial.total))")
    let partialPayload = VerificationLedgerService.payload(of: partial)
    check("so the wire carries the floor and a null total",
          partialPayload["measured"] as? Int == 107 && partialPayload["total"] is NSNull,
          "measured=\(partialPayload["measured"] ?? "nil"), total=\(partialPayload["total"] ?? "nil")")
    expect("with the row that could not be read counted", partialPayload["incompleteRows"] as? Int, 1)

    // A total that has gone once does not come back, however many whole rows follow it.
    partial.add(ledgerIntervalRow("p3", graph: "g", task: "t1", kind: "custom"))
    check("a total lost to one unmeasured part stays lost",
          partial.total == nil, "total=\(String(describing: partial.total))")
    expect("and the floor keeps rising with what is measurable", partial.measured, 207)

    // The marks travel with the numbers, which is the seam `UsageLedger.Measurement` exists for.
    var marked = VerificationLedgerService.TokenReading()
    marked.add(ledgerIntervalRow("r1", graph: "g", task: "t1", kind: "custom",
                                 reasons: ["source_regressed"]))
    expect("every mark the store put on a row reaches the payload",
           VerificationLedgerService.payload(of: marked)["reasons"] as? [String],
           ["source_regressed"])

    // Findings and verification obey the same rule one level up: no receipt is not zero findings
    // and not zero seconds.
    let noReview = VerificationLedgerService.FindingsReading()
    expect("no review receipt is absent", noReview.presence, .absent)
    check("and absent has no finding count",
          VerificationLedgerService.payload(of: noReview)["total"] is NSNull)
    var reviewed = VerificationLedgerService.FindingsReading()
    reviewed.reviewReceipts = 1
    expect("a review that found nothing is present, and that is a positive answer",
           reviewed.presence, .present)
    expect("with zero findings, which is a number somebody measured",
           VerificationLedgerService.payload(of: reviewed)["total"] as? Int, 0)
    let noVerification = VerificationLedgerService.VerificationReading()
    let noVerificationPayload = VerificationLedgerService.payload(of: noVerification)
    check("a Feature with no verification receipt reports no seconds rather than no time spent",
          noVerificationPayload["seconds"] is NSNull && noVerificationPayload["runs"] is NSNull,
          "seconds=\(noVerificationPayload["seconds"] ?? "nil")")
}

group("which side of the work a row was on, and the answer that is neither") {
    func role(_ kind: String?, receipt: Bool = false) -> String {
        VerificationLedgerService.role(kindRaw: kind, hasReviewReceipt: receipt).rawValue
    }
    // The dispatch kind is free forty-character text, so this asks the predicate the graph and
    // the broker already share rather than comparing with a literal of its own.
    expect("code-review is review", role("code-review"), "review")
    expect("and so is a security review", role("security review"), "review")
    expect("custom is implementation", role("custom"), "implementation")
    expect("and so is a test", role("test"), "implementation")
    // **The rung that used to be swallowed.** `LEFT JOIN … WHEN NULL THEN 'implementation'` calls
    // a row with nothing on it implementation, which is a claim about the side it was on.
    expect("a row carrying no kind at all claims neither side", role(nil), "undeclared")
    expect("and neither does an empty one", role(""), "undeclared")
    expect("nor one that is only whitespace", role("   "), "undeclared")
    // A stored verdict outranks whatever the dispatch called itself: the receipt is proof.
    expect("a task whose verdict is in the store reviewed, whatever its kind said",
           role("custom", receipt: true), "review")
    expect("and a receipt answers for a row with no kind too", role(nil, receipt: true), "review")
    check("the three roles are the whole vocabulary",
          VerificationLedgerService.Role.allCases.map { $0.rawValue }
            == ["implementation", "review", "undeclared"])
}

group("one Feature's findings, its verification hours, and the tokens on either side") {
    let answer = ledgerService(
        rows: [
            ledgerIntervalRow("i1", graph: "feature-a", task: "build", kind: "custom"),
            ledgerIntervalRow("i2", graph: "feature-a", task: "build", kind: "custom"),
            ledgerIntervalRow("r1", graph: "feature-a", task: "read", kind: "code-review"),
            // A row whose kind never reached the store. It is neither side, and it is drawn as
            // itself rather than added to the implementation figure beside it.
            ledgerIntervalRow("q1", graph: "feature-a", task: "quiet", kind: nil),
            // Another Feature entirely, so the grouping is doing something.
            ledgerIntervalRow("o1", graph: "feature-b", task: "other", kind: "custom"),
            // And the rows the backfill has not reached, which belong to no Feature at all.
            ledgerIntervalRow("n1", graph: nil, task: "unfiled", kind: "custom"),
        ],
        reviews: [
            ledgerReviewReceipt(
                task: "read", graph: "feature-a", verdict: "changes_required",
                axes: [UsageLedger.StoredReviewAxis(axis: "specification", status: "findings",
                                                    findingCount: 2),
                       UsageLedger.StoredReviewAxis(axis: "repository_invariants", status: "pass",
                                                   findingCount: 0)],
                findings: [("F2", "minor", "a wording"), ("F1", "blocking", "a real one")]),
        ],
        verifications: [
            ledgerVerificationReceipt(task: "build", graph: "feature-a", runs: 2, seconds: 940,
                                      last: "pass"),
            ledgerVerificationReceipt(task: "read", graph: "feature-a", runs: 1, seconds: 60,
                                      last: "fail"),
        ]).read(VerificationLedgerService.Query())
    let payload = answer.payload
    check("the list answers", payload != nil, "refusal=\(String(describing: answer.refusal))")
    let featureA = ledgerFeature(payload, "feature-a")
    check("and it holds the Feature asked about", featureA != nil)

    // **The question the user asked, and the reason this page exists.**
    expect("implementation tokens are counted on their own",
           ledgerTokens(featureA, "implementation")?["measured"] as? Int, 200)
    expect("review tokens are counted separately",
           ledgerTokens(featureA, "review")?["measured"] as? Int, 100)
    expect("and the row that says neither is a third figure, not part of either",
           ledgerTokens(featureA, "undeclared")?["measured"] as? Int, 100)
    // The three gaps are three words. This bucket's tokens were measured perfectly well; what is
    // missing is the side of the work they were spent on, so its `state` is `present`.
    expect("a bucket whose role is undeclared still says its tokens are measured",
           ledgerTokens(featureA, "undeclared")?["state"] as? String, "present")

    let findings = featureA?["findings"] as? [String: Any]
    expect("the findings are counted", findings?["total"] as? Int, 2)
    expect("and the state says they were found rather than assumed",
           findings?["state"] as? String, "present")
    let severities = findings?["severities"] as? [[String: Any]]
    expect("severity is a distribution, worst first",
           severities?.map { $0["severity"] as? String ?? "" }, ["blocking", "minor"])
    expect("with a count each", severities?.first?["count"] as? Int, 1)

    let verification = featureA?["verification"] as? [String: Any]
    expect("the verification hours are summed across the Feature's tasks",
           verification?["seconds"] as? Int, 1000)
    expect("and so are the runs", verification?["runs"] as? Int, 3)
    expect("and a run that ended red is counted rather than averaged away",
           verification?["endedRed"] as? Int, 1)

    // **A Feature nobody has reviewed reads differently from one reviewed clean.**
    let featureB = ledgerFeature(payload, "feature-b")
    expect("a Feature with no review receipt says so",
           (featureB?["findings"] as? [String: Any])?["state"] as? String, "absent")
    check("and reports no finding count at all, because it does not have one",
          (featureB?["findings"] as? [String: Any])?["total"] is NSNull)
    expect("its verification is absent for the same reason",
           (featureB?["verification"] as? [String: Any])?["state"] as? String, "absent")
    expect("and its review tokens are absent rather than zero",
           ledgerTokens(featureB, "review")?["state"] as? String, "absent")
    check("with no number beside them",
          ledgerTokens(featureB, "review")?["measured"] is NSNull)

    // **The block the backfill has not reached.** Drawn as its own subject rather than folded
    // into a Feature or dropped: on a Mac that has not relaunched, this is most of the store.
    let unattributed = payload?["unattributed"] as? [String: Any]
    check("rows carrying no Feature key are their own block", unattributed != nil)
    check("named by a null graph id rather than by an invented one",
          unattributed?["graphId"] is NSNull)
    expect("and they are counted", unattributed?["rows"] as? Int, 1)
    expect("with their tokens visible", ledgerTokens(unattributed, "implementation")?["measured"] as? Int,
           100)
    check("no Feature in the list carries a null id",
          (payload?["features"] as? [[String: Any]])?.allSatisfy { $0["graphId"] is String } == true)

    let read = payload?["read"] as? [String: Any]
    expect("the receipt says how much was scanned", read?["rowsScanned"] as? Int, 6)
    expect("and how many Features were found", read?["featuresFound"] as? Int, 2)
}

group("the ledger refuses what it cannot answer instead of drawing a blank Feature") {
    let parsed = VerificationLedgerService.parse(["graph": "feature-a"], repeatedKeys: [])
    expect("a graph is the one field this read takes", parsed.query?.graphID, "feature-a")
    expect("an empty graph is the whole list rather than a Feature called nothing",
           VerificationLedgerService.parse(["graph": "  "], repeatedKeys: []).query?.graphID, nil)
    check("a misspelled field is refused rather than quietly widening the read",
          VerificationLedgerService.parse(["grapf": "x"], repeatedKeys: []).query == nil)
    check("and so is a field sent twice",
          VerificationLedgerService.parse(["graph": "x"], repeatedKeys: ["graph"]).query == nil)

    let service = ledgerService(
        rows: [ledgerIntervalRow("i1", graph: "feature-a", task: "build", kind: "custom")],
        reviews: [ledgerReviewReceipt(
            task: "read", graph: "feature-a", verdict: "safe_to_land",
            axes: [UsageLedger.StoredReviewAxis(axis: "specification", status: "pass",
                                                findingCount: 0)],
            findings: [("F9", "important", "one to fix")])])
    // **A Feature nothing names is a refusal, not an empty row**: the two are one grey rectangle
    // on a screen and only one of them is something a reader can act on.
    let missing = service.read(VerificationLedgerService.Query(graphID: "nobody"))
    expect("a Feature no record names is a 404", missing.refusal?.status, 404)
    expect("with a code the page can act on", missing.refusal?.code, "graph_not_found")
    check("and no payload at all", missing.payload == nil)

    let detail = service.read(VerificationLedgerService.Query(graphID: "feature-a")).payload
    let feature = detail?["feature"] as? [String: Any]
    expect("the detail carries the findings themselves",
           (feature?["items"] as? [[String: Any]])?.first?["findingId"] as? String, "F9")
    expect("with the evidence the reviewer named",
           ((feature?["items"] as? [[String: Any]])?.first?["evidence"] as? [String])?.count, 1)
    expect("and which axes were answered",
           (feature?["axes"] as? [[String: Any]])?.first?["axis"] as? String, "specification")
    expect("the verdict is counted beside them",
           (feature?["verdicts"] as? [[String: Any]])?.first?["verdict"] as? String, "safe_to_land")
    // The detail's tokens obey the same rule as the list's: this Feature has no review interval.
    expect("a Feature whose review left no interval row shows absent review tokens",
           ledgerTokens(feature, "review")?["state"] as? String, "absent")

    // Worst first is a property of the payload, not of the order the store happened to return.
    let ordered = ledgerService(
        rows: [ledgerIntervalRow("i1", graph: "g", task: "b", kind: "custom")],
        reviews: [ledgerReviewReceipt(task: "r", graph: "g", verdict: "changes_required",
                                      findings: [("Z", "minor", "last"),
                                                 ("A", "important", "middle"),
                                                 ("M", "blocking", "first"),
                                                 ("B", "spicy", "unrecognised")])])
        .read(VerificationLedgerService.Query(graphID: "g")).payload
    let items = ((ordered?["feature"] as? [String: Any])?["items"] as? [[String: Any]])
    expect("findings come back worst first, with a severity nobody knows last rather than dropped",
           items?.map { $0["findingId"] as? String ?? "" }, ["M", "A", "Z", "B"])
}
}
