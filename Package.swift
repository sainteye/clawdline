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
let package = Package(
    name: "Clawdline",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(name: "Clawdline", path: "Sources"),
    ]
)
