import Foundation

/// Buzzing a phone when a deploy stops running.
///
/// The other half of the notification story, and the better half. `StateHook` watches sessions —
/// things you are in the middle of — where the argument for staying quiet is strong because you
/// are usually still there. A deploy is the opposite: it is rare, it takes minutes, you started
/// it deliberately, and then you walked away. **That is the shape of a thing worth a notification**
/// — infrequent, awaited, and with an outcome you cannot guess.
///
/// Both outcomes are sent, and the failure is the one that matters most. A deploy that worked can
/// wait until you look; a deploy that failed is a thing that is *not true yet* about production
/// and every minute it goes unnoticed is a minute of believing something shipped that did not.
///
/// Reads the same files the footer draws from — see `ProjectStatus` and docs/project-status.md.
/// Nothing here is specific to claude-bestiary: anything that writes `ghrun-<repo>.json` in the
/// documented shape gets this for free, which is the point of that format being written down.
enum DeployWatch {

    private static let observerKey = "deploywatch"

    /// The last state seen per repo, so a change can be told from a reading.
    private static var previous: [String: String] = [:]

    /// `cwd` → repo slug. A git remote does not change between two reads of a directory, and
    /// asking costs a subprocess, so it is asked once. `nil` is cached too — a project with no
    /// remote is not going to grow one mid-session, and re-asking every tick would be a `git`
    /// invocation per second per session for a permanent answer.
    private static var remotes: [String: String?] = [:]

    /// Not every tick. Sessions are polled far more often than a deploy can change state, and a
    /// deploy that takes four minutes is not served any better by asking twenty times a second.
    private static var lastLook = Date.distantPast
    private static let interval: TimeInterval = 5

    static func observe() {
        // Seeded on the next look rather than here: `previous` being empty is exactly what
        // `look()` treats as "first reading, say nothing", so there is nothing to prime.
        SessionWatch.shared.observers[observerKey] = { tick() }
    }

    static func stop() {
        SessionWatch.shared.observers.removeValue(forKey: observerKey)
        previous = [:]
    }

    private static func tick() {
        guard Config.shared.pushOnDeploy else { return }
        let now = Date()
        guard now.timeIntervalSince(lastLook) >= interval else { return }
        lastLook = now
        let sessions = SessionWatch.shared.targets
        DispatchQueue.global(qos: .utility).async { look(sessions) }
    }

    /// Off the main thread: `Project.info` runs `git`, and the first look at a new project pays
    /// for it once per session.
    private static func look(_ sessions: [TargetSession]) {
        var seen: [String: (state: String, session: TargetSession)] = [:]
        for session in sessions {
            guard let cwd = Targets.workingDirectory(of: session) else { continue }
            let repo: String?
            if let cached = remotes[cwd] {
                repo = cached
            } else {
                repo = Project.info(cwd: cwd)?.remote
                remotes[cwd] = repo
            }
            guard let repo, seen[repo] == nil else { continue }
            let file = ProjectStatus.cacheDirectory.appendingPathComponent("ghrun-\(repo).json")
            guard let deploy = ProjectStatus.deploy(ProjectStatus.json(file)) else { continue }
            seen[repo] = (deploy.state, session)
        }

        DispatchQueue.main.async { react(seen) }
    }

    /// Which repos stopped running, and how it went.
    ///
    /// Pure, and separate from the sending, so the decision can be tested without a phone, a
    /// subscription, or a deploy — which between them are most of the reasons a rule like this
    /// goes unexercised until it misfires at somebody.
    ///
    /// The two rules worth stating:
    ///
    /// - **A repo appearing for the first time is not a transition.** Without this, launching the
    ///   app while a deploy happened to have finished an hour ago announces it as news, and the
    ///   first thing somebody learns about the feature is that it lies about when things happened.
    /// - **Only leaving `running` counts.** A deploy *starting* is something you already know,
    ///   because you started it.
    static func finished(from previous: [String: String],
                         to seen: [String: String]) -> [(repo: String, ok: Bool)] {
        seen.compactMap { repo, state in
            guard previous[repo] == "running", state != "running" else { return nil }
            return (repo, state == "ok")
        }.sorted { $0.repo < $1.repo }
    }

    /// Split out and given the reading rather than taking it, so a test can drive it.
    static func react(_ seen: [String: (state: String, session: TargetSession)]) {
        defer { for (repo, row) in seen { previous[repo] = row.state } }
        let states = seen.mapValues { $0.state }
        for change in finished(from: previous, to: states) {
            guard let row = seen[change.repo] else { continue }
            let message = StateHook.pushMessage(
                for: row.session,
                project: StateHook.projectName(for: row.session),
                event: change.ok ? L.t.pushDeployOk : L.t.pushDeployFail)
            WebPush.send(title: message.title,
                         body: message.body,
                         url: WebPush.sessionURL(forSessionID: row.session.id),
                         tag: "deploy-\(change.repo)",
                         icon: RemoteIcon.projectPath(for: StateHook.projectMark(for: row.session)))
        }
    }
}
