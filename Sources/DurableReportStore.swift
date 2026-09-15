import CryptoKit
import Foundation
#if canImport(Darwin)
import Darwin
#elseif canImport(Glibc)
import Glibc
#endif

/// The one managed home for explicitly promoted, immutable task reports.
///
/// Report bytes remain part of ProjectDocuments: this store supplies one reserved virtual path
/// below the existing `project` scope, so local, paired-device and Cloud readers all cross the
/// same authenticated document boundary.  The index is an authority, not a cache.  Unknown or
/// damaged state therefore refuses reads and writes instead of presenting an incomplete menu.
final class DurableReportStore: @unchecked Sendable {
    struct Policy {
        var maximumReportBytes = ProjectDocuments.maximumBytes
        var maximumReports = 500
        var maximumTotalBytes = 256 * 1024 * 1024
        var maximumReportsPerProject = 100
        var maximumProjectBytes = 64 * 1024 * 1024
        var maximumIndexBytes = 4 * 1024 * 1024
    }

    struct PromotionRequest {
        let requestID: String
        let taskID: String
        let sessionID: String
        let path: String
        let title: String
    }

    /// Constructed only after the HTTP adapter has matched the request to an authenticated task
    /// result, its declared artifact list, its Root Session and its canonical Project.
    struct TrustedTaskSource {
        let taskID: String
        let sessionID: String
        let projectDir: String
        let taskDirectory: String
        let path: String
        let title: String
        let resultVerifiedAt: Int
    }

    struct Report: Codable, Equatable {
        let immutableID: String
        let requestID: String
        let receiptID: String
        let fingerprintSHA256: String
        let checksumSHA256: String
        let byteCount: Int
        let createdAt: Int
        let projectDir: String
        let sessionID: String
        let taskID: String
        let sourcePath: String
        let title: String
        let mediaType: String
        let publicPath: String
        let provenance: String
        let resultVerifiedAt: Int

        enum CodingKeys: String, CodingKey {
            case immutableID = "immutable_id"
            case requestID = "request_id"
            case receiptID = "receipt_id"
            case fingerprintSHA256 = "fingerprint_sha256"
            case checksumSHA256 = "checksum_sha256"
            case byteCount = "byte_count"
            case createdAt = "created_at"
            case projectDir = "project_dir"
            case sessionID = "session_id"
            case taskID = "task_id"
            case sourcePath = "source_path"
            case title, mediaType = "media_type", publicPath = "public_path", provenance
            case resultVerifiedAt = "result_verified_at"
        }
    }

    enum ReadResult {
        case report(Report, Data)
        case notFound
        case unavailable
    }

    struct Reply: Error {
        let status: Int
        let body: [String: Any]
    }

    enum FaultPoint { case afterObjectCommit, beforeIndexCommit }

    private struct State: Codable {
        var schemaVersion: Int
        var reports: [Report]

        enum CodingKeys: String, CodingKey {
            case schemaVersion = "schema_version"
            case reports
        }
    }

    private struct Orphan {
        let url: URL
        let byteCount: Int
    }

    static let namespace = "clawdline-durable-reports"
    static var sharedOverrideForTesting: DurableReportStore?
    static var shared: DurableReportStore {
        sharedOverrideForTesting ?? productionShared
    }
    private static let productionShared = DurableReportStore(root: defaultRoot())

    private let root: URL
    private let objects: URL
    private let index: URL
    private let policy: Policy
    private let fault: ((FaultPoint) -> Bool)?
    private let lock = NSLock()
    private var state = State(schemaVersion: 1, reports: [])
    private var orphans: [String: Orphan] = [:]
    private var available = true

    init(root: URL, policy: Policy = Policy(), fault: ((FaultPoint) -> Bool)? = nil) {
        self.root = root.standardizedFileURL
        self.objects = root.standardizedFileURL.appendingPathComponent("objects", isDirectory: true)
        self.index = root.standardizedFileURL.appendingPathComponent("index.json")
        self.policy = policy
        self.fault = fault
        do {
            try load()
        } catch {
            available = false
        }
    }

    static func isReservedPath(_ path: String) -> Bool {
        path == namespace || path.hasPrefix(namespace + "/")
    }

    func reports(projectDir: String) -> Result<[Report], Error> {
        lock.lock(); defer { lock.unlock() }
        guard available else { return .failure(StoreError.unavailable) }
        return .success(state.reports.filter { $0.projectDir == projectDir }
            .sorted { $0.createdAt == $1.createdAt
                ? $0.immutableID < $1.immutableID : $0.createdAt > $1.createdAt })
    }

    /// A durable direct read deliberately does not require a currently live Session.  The
    /// receipt-bound Session id is checked instead, which is what keeps the canonical Cloud link
    /// useful after a task directory is collected or its terminal disappears.
    func read(sessionID: String, publicPath: String) -> ReadResult {
        lock.lock(); defer { lock.unlock() }
        guard available else { return .unavailable }
        guard let report = state.reports.first(where: {
            $0.sessionID == sessionID && $0.publicPath == publicPath
        }) else { return .notFound }
        let url = objectURL(for: report)
        do {
            let data = try Self.secureRead(url, maximumBytes: policy.maximumReportBytes,
                                           privatePermissions: true)
            guard data.count == report.byteCount,
                  Self.sha256(data) == report.checksumSHA256,
                  String(data: data, encoding: .utf8) != nil else {
                available = false
                return .unavailable
            }
            return .report(report, data)
        } catch {
            available = false
            return .unavailable
        }
    }

    func promote(_ request: PromotionRequest, source: TrustedTaskSource,
                 machineID: String?, now: Date = Date()) -> Reply {
        lock.lock()
        let authoritative = available
        lock.unlock()
        guard authoritative else { return Self.error(.unavailable) }
        guard request.taskID == source.taskID, request.sessionID == source.sessionID,
              request.path == source.path, request.title == source.title else {
            return Self.error(.badRequest)
        }
        lock.lock()
        let prior = state.reports.first { $0.requestID == request.requestID }
        lock.unlock()
        if let prior, !Self.sameProvenance(prior, request: request, source: source) {
            return Self.error(.conflict)
        }
        let data: Data
        let mediaType: String
        do {
            (data, mediaType) = try sourceBytes(source)
        } catch let error as StoreError {
            if let prior {
                guard error == .sourceNotFound, Self.taskDirectoryIsAbsent(source) else {
                    return Self.error(.conflict)
                }
                lock.lock(); defer { lock.unlock() }
                guard available else { return Self.error(.unavailable) }
                do {
                    _ = try verifiedObject(for: prior)
                    return Self.success(prior, replayed: true, machineID: machineID)
                } catch {
                    available = false
                    return Self.error(.unavailable)
                }
            }
            return Self.error(error)
        } catch {
            return Self.error(.sourceNotFound)
        }
        let checksum = Self.sha256(data)
        let fingerprint = Self.fingerprint(request: request, source: source,
                                           checksum: checksum, bytes: data.count,
                                           mediaType: mediaType)
        let receiptID = "drp-" + Self.sha256(Data(("promotion-v1\u{1f}" + fingerprint).utf8))
        let fileExtension = URL(fileURLWithPath: request.path).pathExtension.lowercased()
        let publicPath = Self.namespace + "/" + request.requestID + "." + fileExtension

        lock.lock(); defer { lock.unlock() }
        guard available else { return Self.error(.unavailable) }
        if let existing = state.reports.first(where: { $0.requestID == request.requestID }) {
            guard existing.fingerprintSHA256 == fingerprint else {
                return Self.error(.conflict)
            }
            do {
                _ = try verifiedObject(for: existing)
                return Self.success(existing, replayed: true, machineID: machineID)
            } catch {
                available = false
                return Self.error(.unavailable)
            }
        }

        let projectReports = state.reports.filter { $0.projectDir == source.projectDir }
        let indexedBytes = state.reports.reduce(0) { $0 + $1.byteCount }
        let projectBytes = projectReports.reduce(0) { $0 + $1.byteCount }
        let orphanBytes = orphans.values.reduce(0) { $0 + $1.byteCount }
        guard state.reports.count + orphans.count
                + (orphans[request.requestID] == nil ? 1 : 0) <= policy.maximumReports,
              indexedBytes + orphanBytes + (orphans[request.requestID] == nil ? data.count : 0)
                <= policy.maximumTotalBytes,
              projectReports.count < policy.maximumReportsPerProject,
              projectBytes + data.count <= policy.maximumProjectBytes else {
            return Self.error(.quota)
        }

        let createdAt = Int(now.timeIntervalSince1970)
        let report = Report(
            immutableID: request.requestID, requestID: request.requestID,
            receiptID: receiptID, fingerprintSHA256: fingerprint,
            checksumSHA256: checksum, byteCount: data.count, createdAt: createdAt,
            projectDir: source.projectDir, sessionID: source.sessionID,
            taskID: source.taskID, sourcePath: "artifacts/" + source.path,
            title: source.title, mediaType: mediaType, publicPath: publicPath,
            provenance: "authenticated_task_result_artifact",
            resultVerifiedAt: source.resultVerifiedAt)
        let object = objectURL(for: report)
        do {
            try ensurePrivateDirectories()
            if let orphan = orphans[request.requestID] {
                let existing = try Self.secureRead(orphan.url,
                                                   maximumBytes: policy.maximumReportBytes,
                                                   privatePermissions: true)
                guard existing == data else { return Self.error(.conflict) }
            } else {
                try writeImmutable(data, to: object)
                orphans[request.requestID] = Orphan(url: object, byteCount: data.count)
            }
            if fault?(.afterObjectCommit) == true { throw StoreError.unavailable }
            var next = state
            next.reports.append(report)
            try validate(next)
            if fault?(.beforeIndexCommit) == true { throw StoreError.unavailable }
            try persist(next)
            state = next
            orphans.removeValue(forKey: request.requestID)
            return Self.success(report, replayed: false, machineID: machineID)
        } catch let error as StoreError {
            if error == .unavailable { available = false }
            return Self.error(error)
        } catch {
            available = false
            return Self.error(.unavailable)
        }
    }

    // MARK: - Source boundary

    private func sourceBytes(_ source: TrustedTaskSource) throws -> (Data, String) {
        guard source.taskID == source.taskID.lowercased(),
              OwnedStorage.canonicalUUID(source.taskID),
              source.sessionID.utf8.count <= 128,
              source.projectDir.hasPrefix("/"), source.projectDir.utf8.count <= 4096,
              source.taskDirectory == "/tmp/.clawdline/" + source.taskID,
              source.path == source.path.trimmingCharacters(in: .whitespacesAndNewlines),
              source.title == source.title.trimmingCharacters(in: .whitespacesAndNewlines),
              !source.title.isEmpty, source.title.utf8.count <= 300,
              source.resultVerifiedAt > 0,
              let root = ProjectDocuments.taskRoot(under: source.taskDirectory) else {
            throw StoreError.sourceNotFound
        }
        let located: ProjectDocuments.Located
        switch ProjectDocuments.file(in: root, at: source.path) {
        case .success(let value): located = value
        case .failure(.tooLarge): throw StoreError.sourceTooLarge
        case .failure: throw StoreError.sourceNotFound
        }
        guard located.bytes <= policy.maximumReportBytes else {
            throw StoreError.sourceTooLarge
        }
        let data = try Self.secureRead(located.url, maximumBytes: policy.maximumReportBytes,
                                       privatePermissions: false)
        guard !data.isEmpty, data.count == located.bytes else { throw StoreError.sourceUnsafe }
        guard let text = String(data: data, encoding: .utf8) else {
            throw StoreError.sourceUnsafe
        }
        guard !Self.containsSecretMarker(text) else { throw StoreError.sourceSecret }
        let ext = located.url.pathExtension.lowercased()
        let mediaType = ext == "txt" ? "text/plain; charset=utf-8"
                                      : "text/markdown; charset=utf-8"
        return (data, mediaType)
    }

    private static func containsSecretMarker(_ text: String) -> Bool {
        let folded = text.lowercased()
        return ["task_secret", "x-clawdline-orchestrator", "authorization:",
                "bearer ", "-----begin private key-----", "machine_credential",
                "recovery_code"].contains { folded.contains($0) }
    }

    // MARK: - Durable index and immutable objects

    private enum StoreError: Error, Equatable {
        case badRequest, sourceNotFound, sourceTooLarge, sourceUnsafe, sourceSecret
        case conflict, quota, unavailable
    }

    private func load() throws {
        guard Self.safeInternalRoot(root) else { throw StoreError.unavailable }
        if !Self.pathExists(root.path) {
            state = State(schemaVersion: 1, reports: [])
            return
        }
        try Self.validatePrivateDirectory(root)
        let rootNames = try FileManager.default.contentsOfDirectory(atPath: root.path)
        guard rootNames.allSatisfy({
            $0 == "index.json" || $0 == "objects" || Self.isIndexTemporary($0)
        }) else { throw StoreError.unavailable }
        var removedTemporary = false
        for name in rootNames where Self.isIndexTemporary(name) {
            let temporary = root.appendingPathComponent(name)
            _ = try Self.secureRead(temporary, maximumBytes: policy.maximumIndexBytes,
                                    privatePermissions: true)
            guard unlink(temporary.path) == 0 else { throw StoreError.unavailable }
            removedTemporary = true
        }
        if removedTemporary { try Self.syncDirectory(root) }
        if Self.pathExists(objects.path) { try Self.validatePrivateDirectory(objects) }
        let loaded: State
        if Self.pathExists(index.path) {
            let data = try Self.secureRead(index, maximumBytes: policy.maximumIndexBytes,
                                           privatePermissions: true)
            loaded = try JSONDecoder().decode(State.self, from: data)
        } else {
            loaded = State(schemaVersion: 1, reports: [])
        }
        try validate(loaded)
        var indexedNames: Set<String> = []
        for report in loaded.reports {
            let url = objectURL(for: report)
            let data = try Self.secureRead(url, maximumBytes: policy.maximumReportBytes,
                                           privatePermissions: true)
            guard data.count == report.byteCount, Self.sha256(data) == report.checksumSHA256,
                  String(data: data, encoding: .utf8) != nil else {
                throw StoreError.unavailable
            }
            indexedNames.insert(url.lastPathComponent)
        }
        var foundOrphans: [String: Orphan] = [:]
        if Self.pathExists(objects.path) {
            let names = try FileManager.default.contentsOfDirectory(atPath: objects.path)
            guard names.count <= policy.maximumReports else { throw StoreError.unavailable }
            for name in names where !indexedNames.contains(name) {
                guard let identity = Self.objectIdentity(name) else {
                    throw StoreError.unavailable
                }
                let url = objects.appendingPathComponent(name)
                let data = try Self.secureRead(url, maximumBytes: policy.maximumReportBytes,
                                               privatePermissions: true)
                guard foundOrphans[identity] == nil,
                      String(data: data, encoding: .utf8) != nil else {
                    throw StoreError.unavailable
                }
                foundOrphans[identity] = Orphan(url: url, byteCount: data.count)
            }
        }
        let total = loaded.reports.reduce(0) { $0 + $1.byteCount }
            + foundOrphans.values.reduce(0) { $0 + $1.byteCount }
        guard loaded.reports.count + foundOrphans.count <= policy.maximumReports,
              total <= policy.maximumTotalBytes else { throw StoreError.unavailable }
        state = loaded
        orphans = foundOrphans
    }

    private func validate(_ candidate: State) throws {
        guard candidate.schemaVersion == 1,
              candidate.reports.count <= policy.maximumReports else {
            throw StoreError.unavailable
        }
        var ids: Set<String> = [], receipts: Set<String> = [], paths: Set<String> = []
        var total = 0
        var projects: [String: (count: Int, bytes: Int)] = [:]
        for report in candidate.reports {
            let ext = URL(fileURLWithPath: report.publicPath).pathExtension.lowercased()
            let expectedMedia = ext == "txt" ? "text/plain; charset=utf-8"
                                             : "text/markdown; charset=utf-8"
            let reconstructedRequest = PromotionRequest(
                requestID: report.requestID, taskID: report.taskID,
                sessionID: report.sessionID,
                path: String(report.sourcePath.dropFirst("artifacts/".count)),
                title: report.title)
            let reconstructedSource = TrustedTaskSource(
                taskID: report.taskID, sessionID: report.sessionID,
                projectDir: report.projectDir, taskDirectory: "/tmp/.clawdline/" + report.taskID,
                path: reconstructedRequest.path, title: report.title,
                resultVerifiedAt: report.resultVerifiedAt)
            let expectedFingerprint = Self.fingerprint(
                request: reconstructedRequest, source: reconstructedSource,
                checksum: report.checksumSHA256, bytes: report.byteCount,
                mediaType: report.mediaType)
            let relativeSource = report.sourcePath.hasPrefix("artifacts/")
                ? String(report.sourcePath.dropFirst("artifacts/".count)) : ""
            guard OwnedStorage.canonicalUUID(report.immutableID),
                  report.immutableID == report.requestID,
                  report.receiptID == "drp-" + Self.sha256(
                    Data(("promotion-v1\u{1f}" + report.fingerprintSHA256).utf8)),
                  report.fingerprintSHA256 == expectedFingerprint,
                  Self.isSHA256(report.fingerprintSHA256),
                  Self.isSHA256(report.checksumSHA256),
                  report.byteCount > 0, report.byteCount <= policy.maximumReportBytes,
                  report.createdAt > 0, report.resultVerifiedAt > 0,
                  report.projectDir.hasPrefix("/"), report.projectDir.utf8.count <= 4096,
                  !report.sessionID.isEmpty, report.sessionID.utf8.count <= 128,
                  !Self.hasControlCharacters(report.sessionID),
                  OwnedStorage.canonicalUUID(report.taskID),
                  Self.safeRelativeDocumentPath(relativeSource),
                  report.title == report.title.trimmingCharacters(in: .whitespacesAndNewlines),
                  !Self.hasControlCharacters(report.title),
                  report.title.utf8.count > 0, report.title.utf8.count <= 300,
                  ["md", "markdown", "txt"].contains(ext),
                  report.mediaType == expectedMedia,
                  report.publicPath == Self.namespace + "/" + report.immutableID + "." + ext,
                  report.provenance == "authenticated_task_result_artifact",
                  ids.insert(report.immutableID).inserted,
                  receipts.insert(report.receiptID).inserted,
                  paths.insert(report.publicPath).inserted else {
                throw StoreError.unavailable
            }
            total += report.byteCount
            let current = projects[report.projectDir] ?? (0, 0)
            projects[report.projectDir] = (current.count + 1, current.bytes + report.byteCount)
        }
        guard total <= policy.maximumTotalBytes,
              projects.values.allSatisfy({
                $0.count <= policy.maximumReportsPerProject
                    && $0.bytes <= policy.maximumProjectBytes
              }) else { throw StoreError.unavailable }
    }

    private func objectURL(for report: Report) -> URL {
        objects.appendingPathComponent(report.immutableID + "." +
            URL(fileURLWithPath: report.publicPath).pathExtension.lowercased())
    }

    private static func objectIdentity(_ name: String) -> String? {
        let url = URL(fileURLWithPath: name)
        guard !name.hasPrefix("."), !name.contains("/"),
              ["md", "markdown", "txt"].contains(url.pathExtension.lowercased()) else {
            return nil
        }
        let identity = url.deletingPathExtension().lastPathComponent
        let ext = url.pathExtension.lowercased()
        return OwnedStorage.canonicalUUID(identity) && name == identity + "." + ext
            ? identity : nil
    }

    private static func isIndexTemporary(_ name: String) -> Bool {
        guard name.hasPrefix(".index."), name.hasSuffix(".new") else { return false }
        let identity = String(name.dropFirst(".index.".count).dropLast(".new".count))
        return OwnedStorage.canonicalUUID(identity)
    }

    private func verifiedObject(for report: Report) throws -> Data {
        let data = try Self.secureRead(objectURL(for: report),
                                       maximumBytes: policy.maximumReportBytes,
                                       privatePermissions: true)
        guard data.count == report.byteCount, Self.sha256(data) == report.checksumSHA256,
              String(data: data, encoding: .utf8) != nil else {
            throw StoreError.unavailable
        }
        return data
    }

    private func ensurePrivateDirectories() throws {
        guard Self.safeInternalRoot(root) else { throw StoreError.unavailable }
        try Self.ensurePrivateDirectory(root)
        try Self.ensurePrivateDirectory(objects)
    }

    private func writeImmutable(_ data: Data, to url: URL) throws {
        let descriptor = url.path.withCString {
            open($0, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
        }
        guard descriptor >= 0 else {
            if errno == EEXIST {
                let existing = try Self.secureRead(url, maximumBytes: policy.maximumReportBytes,
                                                   privatePermissions: true)
                guard existing == data else { throw StoreError.conflict }
                return
            }
            throw StoreError.unavailable
        }
        var keep = false
        defer {
            _ = close(descriptor)
            if !keep { _ = unlink(url.path) }
        }
        try Self.writeAll(data, descriptor: descriptor)
        guard fsync(descriptor) == 0 else { throw StoreError.unavailable }
        keep = true
        try Self.syncDirectory(objects)
    }

    private func persist(_ next: State) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let data = try encoder.encode(next)
        guard data.count <= policy.maximumIndexBytes else { throw StoreError.quota }
        let temporary = root.appendingPathComponent(".index." + UUID().uuidString.lowercased()
            + ".new")
        let descriptor = temporary.path.withCString {
            open($0, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
        }
        guard descriptor >= 0 else { throw StoreError.unavailable }
        var moved = false
        defer {
            _ = close(descriptor)
            if !moved { _ = unlink(temporary.path) }
        }
        try Self.writeAll(data, descriptor: descriptor)
        guard fsync(descriptor) == 0 else { throw StoreError.unavailable }
        guard rename(temporary.path, index.path) == 0 else { throw StoreError.unavailable }
        moved = true
        try Self.syncDirectory(root)
    }

    private static func secureRead(_ url: URL, maximumBytes: Int,
                                   privatePermissions: Bool) throws -> Data {
        var named = stat()
        guard lstat(url.path, &named) == 0, named.st_mode & S_IFMT == S_IFREG,
              named.st_nlink == 1, named.st_uid == geteuid(), named.st_size >= 0,
              named.st_size <= off_t(maximumBytes),
              privatePermissions ? named.st_mode & 0o777 == 0o600
                                 : named.st_mode & 0o022 == 0 else {
            throw StoreError.sourceUnsafe
        }
        let descriptor = url.path.withCString {
            open($0, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
        }
        guard descriptor >= 0 else { throw StoreError.sourceUnsafe }
        defer { _ = close(descriptor) }
        var opened = stat()
        guard fstat(descriptor, &opened) == 0,
              opened.st_dev == named.st_dev, opened.st_ino == named.st_ino,
              opened.st_size == named.st_size, opened.st_nlink == named.st_nlink else {
            throw StoreError.sourceUnsafe
        }
        var data = Data(count: Int(opened.st_size))
        var offset = 0
        try data.withUnsafeMutableBytes { buffer in
            while offset < buffer.count {
                #if canImport(Darwin)
                let count = Darwin.read(descriptor, buffer.baseAddress!.advanced(by: offset),
                                        buffer.count - offset)
                #else
                let count = Glibc.read(descriptor, buffer.baseAddress!.advanced(by: offset),
                                       buffer.count - offset)
                #endif
                guard count > 0 else { throw StoreError.sourceUnsafe }
                offset += count
            }
        }
        var after = stat(), current = stat()
        guard fstat(descriptor, &after) == 0, lstat(url.path, &current) == 0,
              after.st_dev == opened.st_dev, after.st_ino == opened.st_ino,
              after.st_size == opened.st_size, after.st_nlink == opened.st_nlink,
              current.st_dev == opened.st_dev, current.st_ino == opened.st_ino,
              current.st_size == opened.st_size, current.st_nlink == opened.st_nlink else {
            throw StoreError.sourceUnsafe
        }
        return data
    }

    private static func ensurePrivateDirectory(_ url: URL) throws {
        if !pathExists(url.path) {
            try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
            guard chmod(url.path, 0o700) == 0 else { throw StoreError.unavailable }
            try syncDirectory(url.deletingLastPathComponent())
        }
        try validatePrivateDirectory(url)
    }

    private static func validatePrivateDirectory(_ url: URL) throws {
        var info = stat()
        guard lstat(url.path, &info) == 0, info.st_mode & S_IFMT == S_IFDIR,
              info.st_uid == geteuid(), info.st_mode & 0o777 == 0o700 else {
            throw StoreError.unavailable
        }
    }

    private static func syncDirectory(_ url: URL) throws {
        let descriptor = url.path.withCString {
            open($0, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        }
        guard descriptor >= 0 else { throw StoreError.unavailable }
        defer { _ = close(descriptor) }
        guard fsync(descriptor) == 0 else { throw StoreError.unavailable }
    }

    private static func writeAll(_ data: Data, descriptor: Int32) throws {
        var offset = 0
        while offset < data.count {
            let count = data.withUnsafeBytes { buffer in
                write(descriptor, buffer.baseAddress!.advanced(by: offset), buffer.count - offset)
            }
            guard count > 0 else { throw StoreError.unavailable }
            offset += count
        }
    }

    private static func safeInternalRoot(_ url: URL) -> Bool {
        url.isFileURL && url.path.hasPrefix("/") && url.path != "/"
            && !url.pathComponents.contains("..") && url.lastPathComponent == "durable-reports"
    }

    private static func pathExists(_ path: String) -> Bool {
        var info = stat()
        return lstat(path, &info) == 0
    }

    private static func sha256(_ data: Data) -> String {
        RemoteAuth.hex(SHA256.hash(data: data))
    }

    private static func isSHA256(_ value: String) -> Bool {
        value.count == 64 && value.unicodeScalars.allSatisfy {
            (0x30...0x39).contains($0.value) || (0x61...0x66).contains($0.value)
        }
    }

    private static func hasControlCharacters(_ value: String) -> Bool {
        value.unicodeScalars.contains { $0.value < 0x20 || (0x7f...0x9f).contains($0.value) }
    }

    private static func safeRelativeDocumentPath(_ path: String) -> Bool {
        guard !path.isEmpty, path.count <= 512, !path.contains("\0"), !path.hasPrefix("/")
        else { return false }
        let segments = path.split(separator: "/", omittingEmptySubsequences: false)
        return !segments.isEmpty && segments.count <= ProjectDocuments.maximumDepth
            && segments.allSatisfy { !$0.isEmpty && !$0.hasPrefix(".") }
            && ProjectDocuments.readableExtensions.contains(
                URL(fileURLWithPath: path).pathExtension.lowercased())
    }

    private static func sameProvenance(_ report: Report, request: PromotionRequest,
                                       source: TrustedTaskSource) -> Bool {
        report.requestID == request.requestID && report.taskID == source.taskID
            && report.sessionID == source.sessionID && report.projectDir == source.projectDir
            && report.sourcePath == "artifacts/" + source.path && report.title == source.title
            && report.resultVerifiedAt == source.resultVerifiedAt
    }

    private static func taskDirectoryIsAbsent(_ source: TrustedTaskSource) -> Bool {
        var info = stat()
        return lstat(source.taskDirectory, &info) != 0 && errno == ENOENT
    }

    private static func fingerprint(request: PromotionRequest, source: TrustedTaskSource,
                                    checksum: String, bytes: Int, mediaType: String) -> String {
        let fields = ["durable-report-promotion-v1", request.requestID, source.taskID,
                      source.sessionID, source.projectDir, "artifacts/" + source.path,
                      source.title, String(source.resultVerifiedAt), checksum,
                      String(bytes), mediaType]
        return sha256(Data(fields.joined(separator: "\u{1f}").utf8))
    }

    private static func defaultRoot() -> URL {
        let support = FileManager.default.urls(for: .applicationSupportDirectory,
                                                in: .userDomainMask).first
            ?? FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/Application Support", isDirectory: true)
        return support.appendingPathComponent("Clawdline", isDirectory: true)
            .appendingPathComponent("durable-reports", isDirectory: true)
    }

    static func canonicalCloudURL(machineID: String?, sessionID: String,
                                  publicPath: String) -> String? {
        guard let machineID, !machineID.isEmpty, machineID != "this-mac",
              machineID.utf8.count <= 128, sessionID.utf8.count <= 128,
              !hasControlCharacters(machineID), !hasControlCharacters(sessionID) else { return nil }
        var fragment = URLComponents()
        fragment.queryItems = [
            URLQueryItem(name: "document", value: "1"),
            URLQueryItem(name: "machine", value: machineID),
            URLQueryItem(name: "session", value: sessionID),
            URLQueryItem(name: "scope", value: "project"),
            URLQueryItem(name: "path", value: publicPath),
        ]
        guard let value = fragment.percentEncodedQuery else { return nil }
        return "https://app.clawdline.com/#" + value
    }

    private static func success(_ report: Report, replayed: Bool,
                                machineID: String?) -> Reply {
        let segment = report.sessionID.addingPercentEncoding(
            withAllowedCharacters: CharacterSet.alphanumerics.union(
                CharacterSet(charactersIn: "-._~"))) ?? ""
        let local = "/v1/sessions/" + segment + "/documents/project/"
            + ProjectDocuments.escaped(report.publicPath)
        let cloud = canonicalCloudURL(machineID: machineID, sessionID: report.sessionID,
                                      publicPath: report.publicPath)
        var document: [String: Any] = [
            "session_id": report.sessionID, "scope": "project", "path": report.publicPath,
            "local_url": local, "canonical_cloud_url": cloud ?? NSNull(),
        ]
        if let cloud {
            document["board_document_reference"] = [
                "documentId": report.receiptID, "version": 1, "title": report.title,
                "url": cloud, "purpose": "reference", "authority": "narrative_only",
            ]
        }
        return Reply(status: replayed ? 200 : 201, body: [
            "ok": true, "replayed": replayed,
            "promotion": [
                "immutable_id": report.immutableID, "receipt_id": report.receiptID,
                "request_id": report.requestID, "checksum_sha256": report.checksumSHA256,
                "byte_count": report.byteCount, "created_at": report.createdAt,
                "project_dir": report.projectDir, "session_id": report.sessionID,
                "task_id": report.taskID, "source_path": report.sourcePath,
                "media_type": report.mediaType, "title": report.title,
                "provenance": ["kind": report.provenance,
                    "task_result_verified_at": report.resultVerifiedAt],
                "fingerprint_sha256": report.fingerprintSHA256,
                "authority": "narrative_only", "pinned": true, "document": document,
            ] as [String: Any],
        ])
    }

    private static func error(_ error: StoreError) -> Reply {
        let value: (Int, String, String)
        switch error {
        case .badRequest:
            value = (400, "bad_durable_report_request", "The closed promotion request is invalid.")
        case .sourceNotFound:
            value = (404, "durable_report_source_not_found",
                     "The authenticated task result does not name that safe report source.")
        case .sourceTooLarge:
            value = (413, "durable_report_source_too_large", "That report exceeds the promotion limit.")
        case .sourceUnsafe:
            value = (415, "durable_report_source_unsafe", "That report is not safe inert UTF-8 text.")
        case .sourceSecret:
            value = (422, "durable_report_source_secret", "That report appears to contain a secret marker.")
        case .conflict:
            value = (409, "durable_report_promotion_conflict",
                     "That promotion identity already names different content or provenance.")
        case .quota:
            value = (409, "durable_report_quota_reached",
                     "Pinned durable reports reached a configured count or byte quota.")
        case .unavailable:
            value = (503, "durable_report_store_unavailable",
                     "The durable report authority is unreadable, unsafe, corrupt, or uncertain.")
        }
        return Reply(status: value.0, body: ["error": ["code": value.1, "message": value.2]])
    }
}

/// Closed request/provenance validation kept separate from filesystem persistence so tests can
/// prove that a caller cannot manufacture task identity merely by placing a file under `/tmp`.
enum DurableReportPromotion {
    static func validate(body: [String: Any], idempotencyKey: String?,
                         taskRecord: [String: Any]?, sessionProjectDir: String?)
        -> Result<(DurableReportStore.PromotionRequest,
                   DurableReportStore.TrustedTaskSource), DurableReportStore.Reply> {
        let allowed = Set(["request_id", "task_id", "session_id", "path", "title"])
        guard Set(body.keys).isSubset(of: allowed),
              Set(["request_id", "task_id", "session_id", "path"]).isSubset(of: body.keys),
              let requestID = body["request_id"] as? String,
              let taskID = body["task_id"] as? String,
              let sessionID = body["session_id"] as? String,
              let path = body["path"] as? String,
              OwnedStorage.canonicalUUID(requestID), OwnedStorage.canonicalUUID(taskID),
              idempotencyKey == requestID,
              !sessionID.isEmpty, sessionID.utf8.count <= 128,
              !sessionID.unicodeScalars.contains(where: { $0.value < 0x20 }),
              let record = taskRecord, record["id"] as? String == taskID,
              let projectDir = record["projectDir"] as? String,
              projectDir == sessionProjectDir,
              let directory = record["dir"] as? String,
              directory == "/tmp/.clawdline/" + taskID,
              let verified = record["resultVerifiedAt"] as? Int, verified > 0,
              let state = record["state"] as? String, ["success", "failure"].contains(state),
              let artifacts = record["artifacts"] as? [String],
              artifacts.contains("artifacts/" + path),
              let root = record["root"] as? [String: Any],
              root["terminalId"] as? String == sessionID else {
            return .failure(DurableReportStore.Reply(status: 400, body: [
                "error": ["code": "bad_durable_report_request",
                          "message": "The closed promotion request or task provenance is invalid."],
            ]))
        }
        let title = body["title"] as? String ?? URL(fileURLWithPath: path).lastPathComponent
        guard !title.isEmpty, title.utf8.count <= 300,
              title == title.trimmingCharacters(in: .whitespacesAndNewlines),
              !title.unicodeScalars.contains(where: {
                  $0.value < 0x20 || (0x7f...0x9f).contains($0.value)
              }) else {
            return .failure(DurableReportStore.Reply(status: 400, body: [
                "error": ["code": "bad_durable_report_request",
                          "message": "The report title is invalid."],
            ]))
        }
        let request = DurableReportStore.PromotionRequest(
            requestID: requestID, taskID: taskID, sessionID: sessionID,
            path: path, title: title)
        let source = DurableReportStore.TrustedTaskSource(
            taskID: taskID, sessionID: sessionID, projectDir: projectDir,
            taskDirectory: directory, path: path, title: title,
            resultVerifiedAt: verified)
        return .success((request, source))
    }
}

extension RemoteServer {
    func durableReportPromotionRoute(_ request: Request,
                                     orchestratorAuthed: Bool) -> Response? {
        guard request.method == "POST",
              request.path == "/v1/orchestrator/durable-reports/promotions" else { return nil }
        guard orchestratorAuthed else {
            return .error(403, "forbidden",
                          "Promoting a durable report needs the orchestrator token.")
        }
        return durableReportPromotion(request)
    }

    func durableReportPromotion(_ request: Request) -> Response {
        guard let body = (try? JSONSerialization.jsonObject(with: request.body)) as? [String: Any],
              let taskID = body["task_id"] as? String,
              let sessionID = body["session_id"] as? String,
              let session = session(withID: sessionID),
              let cwd = Targets.workingDirectory(of: session) else {
            return .error(400, "bad_durable_report_request",
                          "A live Root Session and JSON promotion request are required.")
        }
        let validated = DurableReportPromotion.validate(
            body: body, idempotencyKey: request.headers["idempotency-key"],
            taskRecord: Orchestrator.record(id: taskID), sessionProjectDir: cwd)
        let pair: (DurableReportStore.PromotionRequest,
                   DurableReportStore.TrustedTaskSource)
        switch validated {
        case .success(let value): pair = value
        case .failure(let refusal): return Response.json(refusal.body, status: refusal.status)
        }
        let machineID = MainQueue.hop(from: "RemoteServer.durableReportPromotion",
                                      alreadyOnMain: MainQueue.isCurrent) {
            MainActor.assumeIsolated { () -> String? in
                if case .attached(_, let id) = CloudBridgeLifecycle.shared.state { return id }
                return nil
            }
        }
        let reply = DurableReportStore.shared.promote(pair.0, source: pair.1,
                                                      machineID: machineID)
        RemoteAuth.audit("orchestrator.durable_report.promote", [
            "task": pair.0.taskID, "report": pair.0.requestID,
            "ok": reply.status < 300 ? "1" : "0", "status": String(reply.status),
        ])
        return Response.json(reply.body, status: reply.status)
    }
}
