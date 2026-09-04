import Foundation

// The document route's boundary, exercised from both sides.
//
// Its production half lives in `Sources/ProjectArtifact.swift`, beside the two named artifact
// slots it is the successor to. What is worth testing is not that a document can be read — it is
// every path that must not read one, so the refusals are enumerated here one at a time rather
// than sampled.
func runProjectDocumentsTests() {

group("documents leave one of two roots and a path chooses only inside one") {
    // The two roots are a project's own `artifacts/` and a dispatched task's `artifacts/`, and
    // this fixture is built to look like both of the real ones — including the symlinked project
    // root this repository actually has, and the task directory whose `task.json` holds the
    // secret that authenticates that child.
    let fixture = FileManager.default.temporaryDirectory
        .appendingPathComponent("clawdline-documents-\(UUID().uuidString)", isDirectory: true)
    let elsewhere = fixture.appendingPathComponent("elsewhere", isDirectory: true)
    let published = fixture.appendingPathComponent("published", isDirectory: true)
    let project = fixture.appendingPathComponent("project", isDirectory: true)
    let taskDirectory = fixture.appendingPathComponent("task", isDirectory: true)
    let taskArtifacts = taskDirectory.appendingPathComponent("artifacts", isDirectory: true)
    let nested = published.appendingPathComponent("nested", isDirectory: true)
    for directory in [elsewhere, published, project, taskDirectory, taskArtifacts, nested] {
        try! FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    }
    defer { try? FileManager.default.removeItem(at: fixture) }

    // The project's `artifacts` is a symlink into a directory beside it, which is what
    // `<repo>/artifacts -> ../clawdline-cloud/artifacts` is on this machine.
    try! FileManager.default.createSymbolicLink(
        at: project.appendingPathComponent("artifacts"), withDestinationURL: published)
    let secret = "0123456789abcdef0123456789abcdef"
    try! Data("# inventory\n\n883 lines of it.\n".utf8)
        .write(to: published.appendingPathComponent("inventory.md"))
    try! Data("# design\n".utf8).write(to: nested.appendingPathComponent("design.md"))
    try! Data("plain".utf8).write(to: published.appendingPathComponent("notes.txt"))
    try! Data("<b>markup</b>".utf8).write(to: published.appendingPathComponent("page.html"))
    try! Data("hidden".utf8).write(to: published.appendingPathComponent(".hidden.md"))
    try! Data(repeating: 0x61, count: ProjectDocuments.maximumBytes + 1)
        .write(to: published.appendingPathComponent("huge.md"))
    let outside = elsewhere.appendingPathComponent("private.md")
    try! Data("not this one".utf8).write(to: outside)
    try! FileManager.default.createSymbolicLink(
        at: published.appendingPathComponent("escape.md"), withDestinationURL: outside)
    // And a symlinked *directory*, which is the escape the file above does not actually test:
    // Foundation reports a symlink as not-a-regular-file, so `escape.md` is refused by that
    // clause whether or not anything checks containment. A path *through* a symlinked directory
    // ends on an ordinary regular file, so only containment can refuse it.
    try! FileManager.default.createSymbolicLink(
        at: published.appendingPathComponent("outward", isDirectory: true),
        withDestinationURL: elsewhere)
    // A task directory in the shape the broker writes: the secret and the briefing at the top,
    // the child's deliverables one level down.
    try! Data(#"{"task_secret":"\#(secret)"}"#.utf8)
        .write(to: taskDirectory.appendingPathComponent("task.json"))
    try! Data(#"{"summary":"done","task_secret":"\#(secret)"}"#.utf8)
        .write(to: taskDirectory.appendingPathComponent("result.json"))
    try! Data("briefing".utf8).write(to: taskDirectory.appendingPathComponent("CHILD.md"))
    try! Data("# what the child found\n".utf8)
        .write(to: taskArtifacts.appendingPathComponent("report.md"))

    guard let root = ProjectDocuments.projectRoot(under: project.path) else {
        check("the project's document root resolves through its symlink", false)
        return
    }
    expect("the symlink's target is what becomes the root",
           ProjectDocuments.resolvedPath(root), ProjectDocuments.resolvedPath(published))
    check("a project with no artifacts directory has no root",
          ProjectDocuments.projectRoot(under: elsewhere.path) == nil)

    // Normalising is relied on to be a fixed point, and only in one place: the listing resolves
    // its root once and every entry the walk hands back a second time, so a call that alternated
    // would leave no entry beginning with its own root and the listing would come back empty.
    // `/private/tmp` is here because it is the one input Foundation documents as changing —
    // stripping is what makes it a fixed point, not what threatens it — and because the fixtures
    // live under `/var/folders` while a real task root lives under `/tmp`, so the strip is a
    // path these tests would otherwise never take.
    for path in ["/tmp", "/private/tmp", "/var/folders", published.path, taskDirectory.path] {
        let once = ProjectDocuments.resolvedPath(URL(fileURLWithPath: path))
        expect("normalising \(path) twice is normalising it once",
               ProjectDocuments.resolvedPath(URL(fileURLWithPath: once)), once)
    }

    let served = RemoteServer.documentResponse(root: root, path: "inventory.md")
    expect("a document in the root is served", served.status, 200)
    expect("markdown is served as markdown", served.headers["Content-Type"],
           "text/markdown; charset=utf-8")
    expect("a browser may not decide it is HTML instead",
           served.headers["X-Content-Type-Options"], "nosniff")
    expect("a document is not cached anywhere on the way",
           served.headers["Cache-Control"], "private, no-store")
    check("and the bytes are the file's",
          String(data: served.body, encoding: .utf8)?.contains("883 lines of it") == true)
    expect("a document below the root is served too",
           RemoteServer.documentResponse(root: root, path: "nested/design.md").status, 200)
    expect("plain text keeps its own type",
           RemoteServer.documentResponse(root: root, path: "notes.txt")
            .headers["Content-Type"], "text/plain; charset=utf-8")

    // Every refusal, one at a time. A document route tested only on the paths that work has
    // tested nothing: the whole question is what happens to the paths that should not.
    func refused(_ path: String) -> RemoteServer.Response {
        RemoteServer.documentResponse(root: root, path: path)
    }
    expect("a parent segment is refused", refused("../elsewhere/private.md").status, 404)
    expect("a parent segment refuses by name", remoteErrorCode(refused("../elsewhere/private.md")),
           "document_not_found")
    expect("a parent segment deeper in is refused too",
           refused("nested/../../elsewhere/private.md").status, 404)
    expect("an absolute path is refused", refused(outside.path).status, 404)
    expect("an absolute path inside the root is refused as well",
           refused(published.appendingPathComponent("inventory.md").path).status, 404)
    expect("a symlink out of the root is refused", refused("escape.md").status, 404)
    expect("and so is a path that leaves through a symlinked directory",
           refused("outward/private.md").status, 404)
    check("that one really does end on a readable file when nothing checks containment",
          FileManager.default.contents(
            atPath: published.appendingPathComponent("outward/private.md").path) != nil)
    expect("a file the allowlist does not name is refused", refused("page.html").status, 404)
    expect("a dotfile is refused", refused(".hidden.md").status, 404)
    expect("an empty segment is refused", refused("nested//design.md").status, 404)
    expect("a directory is not a document", refused("nested").status, 404)
    expect("a document that is not there is refused", refused("missing.md").status, 404)
    expect("an empty path is refused", refused("").status, 404)
    expect("a document over the cap is refused", refused("huge.md").status, 413)
    expect("and the cap has a refusal of its own", remoteErrorCode(refused("huge.md")),
           "document_too_large")
    check("no refusal carries the file's bytes",
          refused("escape.md").body.count < 512
            && String(data: refused(outside.path).body, encoding: .utf8)?
                .contains("not this one") != true)

    // The listing offers exactly what the read would agree to serve. Zero rows is a failure
    // here, not a pass: a walker that has stopped finding anything reads like a clean directory.
    let listed = ProjectDocuments.documents(in: root)
    let paths = Set(listed.map { $0.path })
    check("the listing found the project's documents", listed.count >= 3)
    check("it lists a document at the top and one below it",
          paths.contains("inventory.md") && paths.contains("nested/design.md")
            && paths.contains("notes.txt"))
    check("it never offers what the read would refuse",
          paths.isDisjoint(with: ["page.html", ".hidden.md", "escape.md", "huge.md",
                                  "outward/private.md"]))
    check("every listed row can actually be read",
          listed.allSatisfy { RemoteServer.documentResponse(root: root, path: $0.path).status == 200 })
    check("a row says how big it is", listed.first { $0.path == "inventory.md" }
            .map { $0.bytes > 0 } == true)

    // ...and only in that direction. The sentence above used to read as an equality, and a page
    // that believed it would be drawing an inventory of the root rather than a menu. Both ways
    // the listing is strictly smaller are pinned here, so that either one changing is a red
    // check and not a surprise on a phone.
    let package = published.appendingPathComponent("bundle.rtfd", isDirectory: true)
    try! FileManager.default.createDirectory(at: package, withIntermediateDirectories: true)
    try! Data("# inside a package\n".utf8)
        .write(to: package.appendingPathComponent("inside.md"))
    let withPackage = ProjectDocuments.documents(in: root).map { $0.path }
    check("a document inside a package directory is not listed",
          !withPackage.contains("bundle.rtfd/inside.md"))
    expect("and it reads at its own address anyway",
           RemoteServer.documentResponse(root: root, path: "bundle.rtfd/inside.md").status, 200)
    check("the package directory itself is not offered either",
          !withPackage.contains("bundle.rtfd"))

    // The cut is the second way. It is taken after the walk, so what it removes is rows and not
    // reachability.
    let overflow = fixture.appendingPathComponent("overflow", isDirectory: true)
    let crowdedFiles = overflow.appendingPathComponent("artifacts", isDirectory: true)
    try! FileManager.default.createDirectory(at: crowdedFiles, withIntermediateDirectories: true)
    let overflowing = ProjectDocuments.maximumListed + 5
    let names = (0..<overflowing).map { String(format: "doc-%04d.md", $0) }
    for name in names {
        try! Data("# \(name)\n".utf8).write(to: crowdedFiles.appendingPathComponent(name))
    }
    guard let crowded = ProjectDocuments.projectRoot(under: overflow.path) else {
        check("a root with more documents than the cap is still a root", false)
        return
    }
    let crowdedRows = ProjectDocuments.documents(in: crowded)
    expect("a root of \(overflowing) documents lists the cap and no more",
           crowdedRows.count, ProjectDocuments.maximumListed)
    let listedNames = Set(crowdedRows.map { $0.path })
    guard let cut = names.first(where: { !listedNames.contains($0) }) else {
        check("the cap left something out", false)
        return
    }
    expect("a document past the cut still reads at its own address",
           RemoteServer.documentResponse(root: crowded, path: cut).status, 200)

    // The task half. The root is the deliverables directory, so the secret is not filtered
    // out — it is outside the only place a path can name.
    guard let deliveries = ProjectDocuments.taskRoot(under: taskDirectory.path) else {
        check("a task's deliverables directory is a root", false)
        return
    }
    expect("a child's deliverable is served",
           RemoteServer.documentResponse(root: deliveries, path: "report.md").status, 200)
    expect("the task's own briefing is above the root and out of reach",
           RemoteServer.documentResponse(root: deliveries, path: "../CHILD.md").status, 404)
    let attempts = ["../task.json", "../result.json", "..%2Fresult.json", "./../result.json",
                    taskDirectory.appendingPathComponent("result.json").path]
    for attempt in attempts {
        let response = RemoteServer.documentResponse(root: deliveries, path: attempt)
        expect("the task secret stays behind the root (\(attempt))", response.status, 404)
        check("no answer for \(attempt) carries the secret",
              String(data: response.body, encoding: .utf8)?.contains(secret) != true)
    }
    let delivered = ProjectDocuments.documents(in: deliveries)
    expect("a task listing holds only its deliverables", delivered.count, 1)
    expect("and names the one file that is there", delivered.first?.path, "report.md")

    // The root itself is the other thing a caller never names, and the two roots do not follow a
    // symlink for the same reason. A person puts the project's there; nobody puts this one there
    // — the child whose deliverables these are creates the directory, and it is the party the
    // boundary bounds. So it may not move the root.
    let movedTask = fixture.appendingPathComponent("task-moved", isDirectory: true)
    try! FileManager.default.createDirectory(at: movedTask, withIntermediateDirectories: true)
    try! FileManager.default.createSymbolicLink(
        at: movedTask.appendingPathComponent("artifacts"), withDestinationURL: elsewhere)
    check("a task's artifacts pointing out of its task directory is not a root",
          ProjectDocuments.taskRoot(under: movedTask.path) == nil)
    check("that one really does reach the outside document when the root is allowed to move",
          FileManager.default.contents(
            atPath: movedTask.appendingPathComponent("artifacts/private.md").path) != nil)
    check("while the project root, which a person places, still follows its own symlink",
          ProjectDocuments.projectRoot(under: project.path) != nil)

    // It refuses leaving, not symlinks: one that stays inside the task directory is a root, so
    // a child is free to keep its deliverables under another name of its own.
    let linkedTask = fixture.appendingPathComponent("task-linked", isDirectory: true)
    let inside = linkedTask.appendingPathComponent("delivered", isDirectory: true)
    try! FileManager.default.createDirectory(at: inside, withIntermediateDirectories: true)
    try! Data("# delivered\n".utf8).write(to: inside.appendingPathComponent("inner.md"))
    try! FileManager.default.createSymbolicLink(
        at: linkedTask.appendingPathComponent("artifacts"), withDestinationURL: inside)
    guard let linked = ProjectDocuments.taskRoot(under: linkedTask.path) else {
        check("a task's artifacts symlinked inside its own directory is still a root", false)
        return
    }
    expect("and the deliverable behind it is served",
           RemoteServer.documentResponse(root: linked, path: "inner.md").status, 200)

    // The address a listing row hands to a page. An ordinary name has to survive it, or the
    // link the page draws is uglier than the file it points at.
    expect("an ordinary name keeps an ordinary address",
           ProjectDocuments.escaped("nested/design.md"), "nested/design.md")
    expect("a space is escaped and the separators are not",
           ProjectDocuments.escaped("two words/a b.md"), "two%20words/a%20b.md")
    expect("and a question mark cannot start a query string",
           ProjectDocuments.escaped("what?.md"), "what%3F.md")

    // The route shape, matched whole rather than by suffix.
    check("the documents listing is the documents route",
          RemoteServer.isDocumentsReading("/v1/sessions/ABC/documents"))
    check("so is one document below it",
          RemoteServer.isDocumentsReading("/v1/sessions/ABC/documents/project/inventory.md"))
    check("an empty session id is not selected",
          !RemoteServer.isDocumentsReading("/v1/sessions//documents"))
    check("a route that merely starts with the word is not selected",
          !RemoteServer.isDocumentsReading("/v1/sessions/ABC/documentation"))
    check("a deeper route with the same segment is not selected",
          !RemoteServer.isDocumentsReading("/v1/sessions/ABC/agents/child/documents"))
    check("another session read is not selected",
          !RemoteServer.isDocumentsReading("/v1/sessions/ABC/info"))

    // Authorisation is the one every other session read has, and it is checked before the
    // route is reached rather than inside it.
    let phone = RemoteAuth.addDevice(name: "document reader", caps: [.read])
    defer { RemoteAuth.revoke(id: phone.id) }
    let unauthenticated = RemoteServer.shared.route(
        remoteRequest("GET", "/v1/sessions/ABC/documents"))
    expect("documents need a paired device", unauthenticated.status, 401)
    expect("and say so in the shape every other refusal uses",
           remoteErrorCode(unauthenticated), "unauthorized")
    let unknown = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/sessions/no-such-session/documents",
        headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("a read token still cannot name a session that is not there", unknown.status, 404)
    expect("an unknown session is a document refusal, not a session one",
           remoteErrorCode(unknown), "document_not_found")
    let unknownDocument = RemoteServer.shared.route(remoteRequest(
        "GET", "/v1/sessions/no-such-session/documents/project/inventory.md",
        headers: ["Authorization": "Bearer \(phone.token)"]))
    expect("nor one of its documents", unknownDocument.status, 404)
}

}
