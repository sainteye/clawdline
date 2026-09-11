// swift-tools-version:5.9
import PackageDescription

// This package exists so an editor can understand the code. It is not how the app is built.
//
// `swift build` produces a bare executable with no Info.plist and no Resources, which cannot
// register a hotkey, find a mascot pack, or talk to iTerm2. **Use ./build.sh**, which compiles
// the same files with swiftc and assembles the .app around them.
//
// Why have it at all: without a package (or a compile_commands.json, which swiftc does not
// emit), SourceKit-LSP has nothing to index — so VS Code, Zed, Neovim and everything else give
// a contributor no completion, no jump-to-definition, and no inline errors on a codebase of
// seven thousand lines. That is the first hour of anyone's first contribution, and it was
// being spent on nothing.
//
// It is kept honest by CI: the build job runs `swift build` as well as ./build.sh, so a new
// file or a raised deployment target cannot leave this file quietly wrong.
//
// W3-1 adds two more targets that this comment's "not how the app is built" does not cover:
// `ClawdlineCore` and `ClawdlineApplication` are the real product-graph boundary named in
// docs/adr/0001-platform-boundary-and-evidence.md and Plan v4's W3 — not editor metadata, and
// not a probe. Each `path:` below is a directory of symlinks into `Sources/`, so the bytes have
// exactly one source of truth: the checked-out filesystem resolves each symlink path to the same
// production file `./build.sh` compiles into the Mac app (the Git symlink object instead stores
// only the link text — see `Packages/README.md`'s correction note) — and
// `tools/check-architecture-boundaries.sh` fails
// closed if a link is missing, repointed, or drifts from the file it names. Nothing here is
// copied, and nothing in `Sources/` changed to make this possible.
//
// `ClawdlineCore` has zero target dependencies and compiles on Ubuntu 24.04 amd64 under the same
// pinned toolchain as the W0-F probe (`tools/ubuntu-core-probe/README.md`); see
// `tools/swift-core-application-linux-build.sh` and the `swift-core-application-linux` CI job.
// `ClawdlineApplication` depends only on `ClawdlineCore`. `Clawdline`, the Mac composition
// target, depends on `ClawdlineApplication` — a real, compiler-checked edge pointing the same
// direction as the ADR's diagram (Mac/Linux composition depends inward on Application, which
// depends inward on Core).
//
// **W3-1 correction (`spec-mac-does-not-consume-application`).** The original delivery stopped
// there: the edge existed in the manifest, but `Clawdline`'s own recursive scan of `Sources/`
// *also* compiled `HostPorts.swift`, `Assistant.swift`, `CloudCanonicalJSON.swift` and
// `CloudClock.swift` a second time, as ordinary members of the `Clawdline` module — so under
// `swift build` there were two unrelated `TargetSession` types, two unrelated `Assistant` types,
// and so on, one per module, and nothing in `Clawdline` ever imported `ClawdlineApplication` to
// reach the real ones. The dependency compiled; it did nothing. The `exclude:` list below is the
// fix: these four files are no longer part of `Clawdline`'s own SwiftPM source set, so under
// `swift build` there is exactly one SwiftPM identity for each of their types, and every one of
// `Clawdline`'s remaining ~60 files that names that vocabulary (`Sources/Targets.swift`,
// `Sources/ITerm.swift`, `Sources/MacHostAdapters.swift`, `Sources/Orchestrator.swift` and
// its neighbours) now reaches it through a guarded `import ClawdlineApplication` — guarded
// because `./build.sh` compiles the very same `Sources/*.swift` files as one flat, single
// module with no `ClawdlineApplication.swiftmodule` to import: see the `#if
// canImport(ClawdlineApplication)` note repeated at the top of each of those files, and
// `Sources/HostPorts.swift`'s own header for why `ClawdlineApplication` re-exports
// `ClawdlineCore` (`@_exported import`) rather than asking every one of those files to also
// learn which of the two layers it needs. `exclude:` only changes what SwiftPM's `Clawdline`
// target compiles; the physical files stay exactly where they were, so
// `tools/swift-source-manifest.sh`'s flat list — what `./build.sh` and `./test.sh` actually
// compile — and its production-file count are unchanged, and so is every runtime, wire and
// crypto behavior. `Packages/README.md`'s correction note and
// `artifacts/W3_1_CORRECTION.md` carry the full before/after and the verification that ran
// against it.
let package = Package(
    name: "Clawdline",
    platforms: [.macOS(.v13)],
    targets: [
        .target(name: "ClawdlineCore", path: "Packages/ClawdlineCore"),
        .target(name: "ClawdlineApplication", dependencies: ["ClawdlineCore"], path: "Packages/ClawdlineApplication"),
        .executableTarget(
            name: "Clawdline",
            dependencies: ["ClawdlineApplication"],
            path: "Sources",
            exclude: [
                "HostPorts.swift",
                "Assistant.swift",
                "CloudCanonicalJSON.swift",
                "CloudClock.swift",
            ]
        ),
    ]
)
