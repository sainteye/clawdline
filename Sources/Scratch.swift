import Foundation

/// The working directory this app starts a headless assistant in, and one private file inside it
/// per run.
///
/// A fresh `…-<UUID>` directory per invocation looks free — it is made under the temporary root
/// and removed on the way out — and is not. Claude Code records a transcript folder under
/// `~/.claude/projects/` named after the directory it ran in, and that folder is **outside** the
/// directory it is named after, so deleting the scratch directory cannot reach it. One run leaves
/// one permanent folder, in the same place the person's own projects are listed from. Counted on
/// this Mac on 2026-08-28: 97 folders from smart notifications and 22 from the planner, against
/// 27 real projects — and they had pushed the real ones off the start-a-session list on the phone.
///
/// The per-call directory was doing two jobs, and only one of them has to be per call:
///
/// - **A neutral working directory.** An empty directory under the temporary root, so `claude -p`
///   cannot pick up the `CLAUDE.md`, settings or skills of whichever project the app happened to
///   be started in. That comes from *where* the directory is, not from it being new: one stable
///   directory is exactly as empty on the thousandth run as on the first.
/// - **A private output file.** Two runs must not write into one sink. That is kept — by giving
///   the file the unique name the directory used to carry.
///
/// So the directory is stable for the lifetime of the machine, the sink is unique per run, and
/// the number of folders left behind stops growing.
enum Scratch {
    /// The one directory every run of `purpose` shares. It carries no per-call component, which
    /// is the whole point: the name is what `~/.claude/projects` is keyed on.
    static func directory(for purpose: String,
                          in root: URL = FileManager.default.temporaryDirectory) -> URL {
        root.appendingPathComponent("clawdline-\(purpose)", isDirectory: true)
    }

    /// A sink inside that directory that no other run can be holding.
    ///
    /// Concurrency is not hypothetical even where one caller's own worker is serial: `build.sh`
    /// replaces and restarts the app while the outgoing copy may still be finishing a turn, so
    /// two Clawdline processes can be inside the same stable directory at the same moment.
    static func file(_ name: String, extension ext: String,
                     in directory: URL, id: UUID = UUID()) -> URL {
        directory.appendingPathComponent("\(name)-\(id.uuidString).\(ext)")
    }

    /// The stable directory, made if it is not there yet, and a private sink inside it.
    ///
    /// Nothing already in the directory is touched. A caller removes its own file when it is
    /// done and never the directory: a sibling run's sink is none of its business, and an empty
    /// directory in the temporary root costs nothing to leave.
    static func prepare(_ directory: URL, output name: String, extension ext: String) -> URL? {
        let fm = FileManager.default
        guard (try? fm.createDirectory(at: directory, withIntermediateDirectories: true)) != nil
        else { return nil }
        return file(name, extension: ext, in: directory)
    }
}
