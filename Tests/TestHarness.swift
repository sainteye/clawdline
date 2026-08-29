import AppKit
import Foundation

// A test binary rather than XCTest, for the same reason the app has no Xcode project:
// `swiftc` and nothing else. Run it with ./test.sh.
//
// What is worth testing here is the parsing and the arithmetic — the shapes that a
// contributor can quietly break and not notice. Anything that needs a window on screen is
// deliberately absent; a test that cannot run in CI is a test nobody runs.

var checks = 0
var failures: [String] = []
var executedTestGroupTitles: [String] = []

func check(_ name: String, _ ok: Bool, _ detail: @autoclosure () -> String = "") {
    checks += 1
    if !ok {
        let d = detail()
        failures.append(d.isEmpty ? name : "\(name) — \(d)")
    }
}

func expect<T: Equatable>(_ name: String, _ got: T, _ want: T) {
    check(name, got == want, "got \(got), want \(want)")
}

func expectClose(_ name: String, _ got: CGFloat, _ want: CGFloat, _ tolerance: CGFloat = 0.001) {
    check(name, abs(got - want) < tolerance, "got \(got), want \(want)")
}

func group(_ title: String, _ body: () -> Void) {
    executedTestGroupTitles.append(title)
    let before = failures.count
    body()
    let mark = failures.count == before ? "✓" : "✗"
    print("  \(mark) \(title)")
}

/// Keep main available to background work that completes with `DispatchQueue.main.sync`, while
/// retaining a hard ceiling so an asynchronous regression fails instead of hanging the suite.
func eventually(timeout: TimeInterval = 3, _ predicate: () -> Bool) -> Bool {
    let deadline = Date().addingTimeInterval(timeout)
    repeat {
        if predicate() { return true }
        _ = RunLoop.main.run(mode: .default, before: Date().addingTimeInterval(0.01))
    } while Date() < deadline
    return predicate()
}

/// What build.sh stamps into the bundle, read out of build.sh itself — the tests have no bundle
/// to ask, and a version that lives in two places is a version that disagrees with itself.
func appVersion() -> String {
    let script = (try? String(contentsOfFile: "build.sh", encoding: .utf8)) ?? ""
    guard let line = script.split(separator: "\n").first(where: {
        $0.contains("CFBundleShortVersionString")
    }) else { return "" }
    guard let open = line.range(of: "<string>"),
          let close = line.range(of: "</string>", range: open.upperBound..<line.endIndex)
    else { return "" }
    return String(line[open.upperBound..<close.lowerBound])
}
