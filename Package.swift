// swift-tools-version:5.9
import PackageDescription

// This package is the compiler-owned product graph. `swift build --product Clawdline` produces
// the Mac executable and `./build.sh` wraps that exact artifact in its Info.plist/resources,
// signs it, and performs the existing guarded install/restart sequence. A bare SwiftPM executable
// is still not a distributable .app bundle.
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
// W3-1 added two inward targets:
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
// target, and `ClawdlineLinux` each depend only on `ClawdlineApplication` — real,
// compiler-checked edges pointing inward. W4-1 adds Linux host adapters and shared admission
// policy. W4-2 adds a long-running systemd composition, durable restart ledger and signed package
// transition gate without adding a direct Linux-to-Core or Linux-to-Mac dependency. W5-1 moves
// the Cloud command ledger, outbound spool and their portable durable stores into that shared
// Application target; both host compositions therefore compile the same state machines and file
// format rather than parallel Mac/Linux copies. W5-4 adds the envelope, transport-facing durable
// outbound owner and relay runtime surface to Application; Linux composes those same owners with
// its protected identity and daemon ingress instead of adding another queue or sequence counter.
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
// because `./test.sh` still compiles the very same `Sources/*.swift` files as one flat, single
// module with no `ClawdlineApplication.swiftmodule` to import: see the `#if
// canImport(ClawdlineApplication)` note repeated at the top of each of those files, and
// `Sources/HostPorts.swift`'s own header for why `ClawdlineApplication` re-exports
// `ClawdlineCore` (`@_exported import`) rather than asking every one of those files to also
// learn which of the two layers it needs. `exclude:` only changes what SwiftPM's `Clawdline`
// target compiles; the physical files stay exactly where they were, so
// `tools/swift-source-manifest.sh`'s flat test list and its production-file count are unchanged,
// and so is every runtime, wire and
// crypto behavior. `Packages/README.md`'s correction note and
// `artifacts/W3_1_CORRECTION.md` carry the full before/after and the verification that ran
// against it.
var products: [Product] = [
    .executable(name: "ClawdlineLinux", targets: ["ClawdlineLinux"]),
]
let dependencies: [Package.Dependency] = [
    // Linux has no system CryptoKit module. Pin the same reviewed swift-crypto release as the
    // Ubuntu Core probe so the shared Application pairing bytes compile on both hosts.
    .package(url: "https://github.com/apple/swift-crypto.git", exact: "4.5.2"),
    // FoundationNetworking's URLSessionWebSocketTask returns NSURLErrorUnsupportedURL on the
    // pinned Ubuntu 24.04 / Swift 6.1.3 image. These exact releases are the reviewed Linux-only
    // transport; macOS continues to use Foundation's URLSession implementation.
    .package(url: "https://github.com/apple/swift-nio.git", exact: "2.102.0"),
    .package(url: "https://github.com/apple/swift-nio-ssl.git", exact: "2.37.4"),
]
var targets: [Target] = [
        .target(name: "ClawdlineCore", path: "Packages/ClawdlineCore"),
        .target(
            name: "ClawdlineApplication",
            dependencies: [
                "ClawdlineCore",
                .product(name: "Crypto", package: "swift-crypto",
                         condition: .when(platforms: [.linux])),
                .product(name: "NIOCore", package: "swift-nio",
                         condition: .when(platforms: [.linux])),
                .product(name: "NIOPosix", package: "swift-nio",
                         condition: .when(platforms: [.linux])),
                .product(name: "NIOHTTP1", package: "swift-nio",
                         condition: .when(platforms: [.linux])),
                .product(name: "NIOWebSocket", package: "swift-nio",
                         condition: .when(platforms: [.linux])),
                .product(name: "NIOSSL", package: "swift-nio-ssl",
                         condition: .when(platforms: [.linux])),
            ],
            path: "Packages/ClawdlineApplication",
            swiftSettings: [.define("CLAWDLINE_APPLICATION_TARGET")]
        ),
        .executableTarget(
            name: "ClawdlineLinux",
            dependencies: ["ClawdlineApplication"],
            path: "Packages/ClawdlineLinux"
        ),
        .testTarget(
            name: "ClawdlineLinuxTests",
            dependencies: [
                "ClawdlineApplication", "ClawdlineLinux",
                .product(name: "NIOCore", package: "swift-nio",
                         condition: .when(platforms: [.linux])),
                .product(name: "NIOEmbedded", package: "swift-nio",
                         condition: .when(platforms: [.linux])),
                .product(name: "NIOHTTP1", package: "swift-nio",
                         condition: .when(platforms: [.linux])),
                .product(name: "NIOWebSocket", package: "swift-nio",
                         condition: .when(platforms: [.linux])),
            ],
            path: "Packages/ClawdlineLinuxTests"
        ),
]

// Linux's package test command must not discover and attempt to compile the AppKit composition.
// On macOS the graph remains byte-for-byte the shipped executable product and source boundary.
#if os(macOS)
products.insert(.executable(name: "Clawdline", targets: ["Clawdline"]), at: 0)
targets.insert(
        .executableTarget(
            name: "Clawdline",
            dependencies: ["ClawdlineApplication"],
            path: "Sources",
            exclude: [
                "HostPorts.swift",
                "ProjectRootPolicy.swift",
                "ProviderLifecyclePolicy.swift",
                "SessionLaunchPolicy.swift",
                "TerminalCommandScheduler.swift",
                "Assistant.swift",
                "CloudCanonicalJSON.swift",
                "CloudV2Protocol.swift",
                "CloudClock.swift",
                "CloudCommandLedger.swift",
                "CloudOutboundSpool.swift",
                "CloudDurableStores.swift",
                "CloudAccount.swift",
                "CloudKeys.swift",
                "CloudPairing.swift",
                "CloudEnvelope.swift",
            ]
        ),
    at: 2
)
#endif

let package = Package(
    name: "Clawdline",
    platforms: [.macOS(.v13)],
    products: products,
    dependencies: dependencies,
    targets: targets
)
