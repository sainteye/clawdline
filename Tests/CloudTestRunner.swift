import Foundation

func runCloudTestsAndFinish() {
validateExecutedTestGroupManifest()
// MARK: - Cloud suite registry

let cloudVectorsURL = URL(
    fileURLWithPath: FileManager.default.currentDirectoryPath,
    isDirectory: true
).appendingPathComponent("Tests/protocol-vectors.json")

struct CloudTestSuite {
    let name: String
    let run: () async throws -> Int
}

struct CloudTestHarnessFailure: Error, CustomStringConvertible {
    let description: String
}

let expectedCloudSuiteNames = [
    "CloudEnvelope", "CloudAccount", "CloudTransport", "CloudAppBridge", "CloudSettings",
    "ScheduleResume", "CloudClock", "CloudCanonicalJSON", "CloudCommandLedger",
    "CloudOutboundSpool", "CloudPairing", "CloudLifecycle",
]
let cloudTestSuites: [CloudTestSuite] = [
    CloudTestSuite(name: "CloudEnvelope", run: {
        try runCloudEnvelopeTests(vectorsURL: cloudVectorsURL)
    }),
    CloudTestSuite(name: "CloudAccount", run: { try await runCloudAccountTests() }),
    CloudTestSuite(name: "CloudTransport", run: { try await runCloudTransportTests() }),
    CloudTestSuite(name: "CloudAppBridge", run: { try await runCloudAppBridgeTests() }),
    CloudTestSuite(name: "CloudSettings", run: { try await runCloudSettingsTests() }),
    CloudTestSuite(name: "ScheduleResume", run: { try runScheduleResumeTests() }),
    CloudTestSuite(name: "CloudClock", run: { try await runCloudClockTests() }),
    CloudTestSuite(name: "CloudCanonicalJSON", run: {
        try await runCloudCanonicalJSONTests()
    }),
    // The ledger suite creates unstructured tasks to prove duplicate coalescing. Run its complete
    // lifecycle from the generic executor, matching its independently verified standalone entry,
    // instead of inheriting this top-level task's main-queue executor preference.
    CloudTestSuite(name: "CloudCommandLedger", run: {
        try await Task.detached { try await runCloudCommandLedgerTests() }.value
    }),
    CloudTestSuite(name: "CloudOutboundSpool", run: {
        try await runCloudOutboundSpoolTests()
    }),
    CloudTestSuite(name: "CloudPairing", run: { try await runCloudPairingTests() }),
    // Last, and reading the same checked-in vectors the envelope suite does: the handover half
    // of this suite is a cross-runtime agreement, not a self-consistency check.
    CloudTestSuite(name: "CloudLifecycle", run: {
        try await runCloudLifecycleTests(vectorsURL: cloudVectorsURL)
    }),
]
let cloudTestCompletionReceiptPrefix = "CLAWDLINE_CLOUD_TESTS_COMPLETE v=1 suite_count=12 suites="

// The transport runner is async. Entering the dispatch main loop keeps Foundation callbacks
// available while its task runs. A process-wide watchdog prevents an await regression from
// leaving that loop alive forever. The override is intentionally test-only, integer-valued and
// bounded so a mutation test can prove the timeout without weakening the normal ceiling.
let cloudRunnerTimeoutEnvironment = "CLAWDLINE_TEST_CLOUD_RUNNER_TIMEOUT_SECONDS"
let cloudRunnerTimeoutSeconds: Int
if let rawTimeout = ProcessInfo.processInfo.environment[cloudRunnerTimeoutEnvironment] {
    guard let timeout = Int(rawTimeout), (1...30).contains(timeout) else {
        FileHandle.standardError.write(Data(
            "\(cloudRunnerTimeoutEnvironment) must be an integer from 1 through 30\n".utf8
        ))
        exit(2)
    }
    cloudRunnerTimeoutSeconds = timeout
} else {
    cloudRunnerTimeoutSeconds = 180
}
let cloudRunnerWatchdog = DispatchWorkItem {
    FileHandle.standardError.write(Data(
        "Cloud async runner timed out after \(cloudRunnerTimeoutSeconds) seconds\n".utf8
    ))
    exit(124)
}
if !focusedTestGroups.isEmpty {
    let missingFocusedTestGroups = focusedTestGroups.subtracting(matchedFocusedTestGroups).sorted()
    if !missingFocusedTestGroups.isEmpty {
        failures.append("focused test group(s) not found: "
            + missingFocusedTestGroups.joined(separator: "; "))
    }
    if checks == 0 {
        failures.append("focused test selection executed zero checks")
    }
    try? FileManager.default.removeItem(at: isolatedTestStoreDirectory)
    print("")
    if failures.isEmpty {
        print("\(checks) focused checks passed")
        exit(0)
    }
    print("\(failures.count) of \(checks) focused checks failed:")
    for failure in failures { print("  ✗ \(failure)") }
    exit(1)
}
// The deadline is kept on a thread of its own rather than on a global dispatch queue. The suites
// being watched park worker threads on semaphores, and a regression that leaks enough of those
// saturates the pool the timer would need — measured here: with the pool full, a five-second
// `asyncAfter` watchdog had still not fired three minutes later. A dedicated thread cannot be
// starved by the code it is watching. `perform()` does nothing once the result path cancels it.
Thread.detachNewThread {
    Thread.sleep(forTimeInterval: TimeInterval(cloudRunnerTimeoutSeconds))
    cloudRunnerWatchdog.perform()
}

Task {
    let mainQueueIdentity = await mainQueueIdentityProbe()
    check("dispatchMain drains a delayed main-queue block off the main thread",
          !mainQueueIdentity.isMainThread)
    check("the delayed block still identifies itself as running on the main queue",
          mainQueueIdentity.isOnMainQueue)
    check("records returns without synchronously dispatching onto its current queue",
          mainQueueIdentity.recordsReturned)
    check("record(id:) returns without synchronously dispatching onto its current queue",
          mainQueueIdentity.missingRecord)

    let imageCrossing = await sessionImagePresentationCrossingProbe()
    check("a worker can materialize the real image attachment through the main-queue boundary",
          imageCrossing.rendered)
    check("image attachment materialization is observed at its named main-queue site",
          imageCrossing.observedSites == ["SessionImagePresentation.materialize"],
          "\(imageCrossing.observedSites)")
    check("the image materialization hop recorder returns the complete reading",
          !imageCrossing.hopsOverflowed)
    check("image materialization never mistakes thread identity for main-queue identity",
          imageCrossing.reentrantHops.isEmpty, "\(imageCrossing.reentrantHops)")

    let crossings = await sessionWatchCrossingProbe()
    check("a reading left a live target behind for the crossings to be asked about",
          crossings.targets > 0)
    check("the crossings are asked from a main-queue block that is not the main thread",
          !crossings.isMainThread && crossings.isOnMainQueue)
    check("both crossings answer for every one of them",
          crossings.crossed == crossings.targets, "\(crossings.crossed) of \(crossings.targets)")
    check("no inventory seam was left set to answer a production read out of a fixture",
          crossings.seamsLeftSet.isEmpty, "\(crossings.seamsLeftSet)")
    check("the hop recorder did not overflow, so the observed sites are the whole list",
          !crossings.hopsOverflowed)
    let observedCrossingSites = Set(crossings.observedSites)
    let missingCrossingSites = dynamicallyExercisedCrossingSites
        .subtracting(observedCrossingSites).sorted()
    check("every crossing the fixture drives is observed hopping, by its own name",
          missingCrossingSites.isEmpty,
          "missing \(missingCrossingSites) — observed \(observedCrossingSites.sorted())")
    for file in ["StartPoints", "RemoteIcon", "SessionImagePresentation",
                 "Orchestrator", "RemoteServer"] {
        let wrong = crossings.reentrantHops.filter { $0.hasPrefix(file + ".") }
        check("\(file) never mistakes main-thread identity for main-queue identity",
              wrong.isEmpty, "\(wrong)")
    }
    let earlier = crossings.reentrantHops.filter {
        $0 == "Config.hookSessionID(of:)" || $0 == "Transcript.sessionID(of:)"
    }
    check("the two earlier SessionWatch crossings remain queue-identity safe",
          earlier.isEmpty, "\(earlier)")

    var completedCloudSuiteNames: [String] = []
    var completedCloudSuiteReceipts: [String] = []
    var cloudReceiptReady = false
    do {
        let registeredNames = cloudTestSuites.map(\.name)
        guard cloudTestSuites.count == 12 else {
            throw CloudTestHarnessFailure(
                description: "Cloud suite registry has \(cloudTestSuites.count) entries, expected 12")
        }
        let (afterRegistryCountCheck, registryCountOverflow) = checks.addingReportingOverflow(1)
        guard !registryCountOverflow else {
            throw CloudTestHarnessFailure(description: "total check count overflow")
        }
        checks = afterRegistryCountCheck

        guard Set(registeredNames).count == registeredNames.count else {
            throw CloudTestHarnessFailure(
                description: "Cloud suite registry contains duplicate names")
        }
        let (afterDuplicateCheck, duplicateCheckOverflow) = checks.addingReportingOverflow(1)
        guard !duplicateCheckOverflow else {
            throw CloudTestHarnessFailure(description: "total check count overflow")
        }
        checks = afterDuplicateCheck

        guard registeredNames.sorted() == expectedCloudSuiteNames.sorted() else {
            throw CloudTestHarnessFailure(
                description: "Cloud suite registry does not match the expected suite set")
        }
        let (afterExpectedSetCheck, expectedSetCheckOverflow) = checks.addingReportingOverflow(1)
        guard !expectedSetCheckOverflow else {
            throw CloudTestHarnessFailure(description: "total check count overflow")
        }
        checks = afterExpectedSetCheck

        for suite in cloudTestSuites {
            try Task.checkCancellation()
            let suiteChecks: Int
            do {
                suiteChecks = try await suite.run()
            } catch {
                throw CloudTestHarnessFailure(description: "\(suite.name) — \(error)")
            }
            try Task.checkCancellation()
            guard suiteChecks > 0 else {
                throw CloudTestHarnessFailure(
                    description: "\(suite.name) returned non-positive check count \(suiteChecks)")
            }
            let (newTotal, overflow) = checks.addingReportingOverflow(suiteChecks)
            guard !overflow else {
                throw CloudTestHarnessFailure(
                    description: "total check count overflow after \(suite.name)")
            }
            checks = newTotal
            completedCloudSuiteNames.append(suite.name)
            completedCloudSuiteReceipts.append("\(suite.name):\(suiteChecks)")
            print("  ✓ \(suite.name) (\(suiteChecks) checks)")
        }

        guard completedCloudSuiteNames.count == 12,
              completedCloudSuiteNames == expectedCloudSuiteNames else {
            throw CloudTestHarnessFailure(
                description: "Cloud suite completion order/count did not match the expected registry")
        }
        let (afterCompletionCheck, completionCheckOverflow) = checks.addingReportingOverflow(1)
        guard !completionCheckOverflow else {
            throw CloudTestHarnessFailure(description: "total check count overflow")
        }
        checks = afterCompletionCheck
        cloudReceiptReady = true
    } catch {
        failures.append("Cloud suite harness — \(error)")
        print("  ✗ Cloud suite harness")
    }

    // MARK: - Result

    cloudRunnerWatchdog.cancel()
    Orchestrator.storeURLOverrideForTesting = nil
    try? FileManager.default.removeItem(at: isolatedTestStoreDirectory)
    print("")
    let finalStatus: Int32
    if failures.isEmpty {
        print("\(checks) checks passed")
        finalStatus = 0
    } else {
        print("\(failures.count) of \(checks) checks failed:")
        for failure in failures { print("  ✗ \(failure)") }
        finalStatus = 1
    }
    if cloudReceiptReady {
        print(cloudTestCompletionReceiptPrefix + completedCloudSuiteReceipts.joined(separator: ","))
    }
    exit(finalStatus)
}
}
