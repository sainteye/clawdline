import AppKit
import Foundation
import ImageIO

/// The only image metadata allowed to survive inside a session-message envelope or transcript.
/// Bytes and filesystem locations remain in ``SessionImageArtifactStore``.
struct SessionImageArtifact: Codable, Equatable {
    let id: String
    let mediaType: String
    let byteCount: Int
    let width: Int
    let height: Int
    let expiresAt: Int

    var object: [String: Any] {
        [
            "id": id,
            "media_type": mediaType,
            "byte_count": byteCount,
            "width": width,
            "height": height,
            "expires_at": expiresAt,
        ]
    }

    /// Strict validation shared by the v2 envelope decoder and the owned store.
    var isValidReference: Bool {
        SessionImageArtifactStore.isArtifactID(id)
            && mediaType == "image/png"
            && byteCount > 0
            && byteCount <= SessionImageArtifactStore.productionPolicy.maxEncodedBytes
            && width > 0 && height > 0
            && width <= SessionImageArtifactStore.productionPolicy.maxDimension
            && height <= SessionImageArtifactStore.productionPolicy.maxDimension
            && width <= SessionImageArtifactStore.productionPolicy.maxPixels / height
            && expiresAt > 0
    }

    static func decode(_ object: [String: Any]) -> SessionImageArtifact? {
        guard Set(object.keys) == Set([
            "id", "media_type", "byte_count", "width", "height", "expires_at",
        ]),
        let id = object["id"] as? String,
        let mediaType = object["media_type"] as? String,
        let byteCount = integer(object["byte_count"]),
        let width = integer(object["width"]),
        let height = integer(object["height"]),
        let expiresAt = integer(object["expires_at"])
        else { return nil }
        let artifact = SessionImageArtifact(
            id: id, mediaType: mediaType, byteCount: byteCount,
            width: width, height: height, expiresAt: expiresAt)
        return artifact.isValidReference ? artifact : nil
    }

    /// JSONSerialization bridges booleans through NSNumber too. Reject those rather than letting
    /// `true` become a one-pixel image or a one-second expiry.
    private static func integer(_ value: Any?) -> Int? {
        guard let number = value as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
        let double = number.doubleValue
        guard double.isFinite, double.rounded() == double,
              double >= Double(Int.min), double <= Double(Int.max) else { return nil }
        return Int(double)
    }
}

/// Decode, normalize, bound and expire the raster bytes referenced by live session messages.
///
/// The directory is an ownership boundary, not merely a default. Every removable filename is
/// generated from a validated opaque id, and pruning never follows an input path back to its
/// source. `CLAWDLINE_SESSION_IMAGE_DIR` moves this deleting store for tests and installations.
struct SessionImageArtifactStore {
    struct Policy {
        let ttl: TimeInterval
        let maxCount: Int
        let maxTotalBytes: Int
        let maxInputBytes: Int
        let maxEncodedBytes: Int
        let maxDimension: Int
        let maxPixels: Int
        let tombstoneTTL: TimeInterval
        let maxMetadataCount: Int
        let maxImagesPerMessage: Int
    }

    struct Stored {
        let artifact: SessionImageArtifact
        let file: URL
    }

    struct Refusal: Error {
        let status: Int
        let code: String
        let message: String
    }

    enum Lookup {
        case live(SessionImageArtifact, Data)
        case expired
        case missing
    }

    static let productionPolicy = Policy(
        ttl: 24 * 60 * 60,
        maxCount: 64,
        maxTotalBytes: 64 << 20,
        maxInputBytes: 12 << 20,
        maxEncodedBytes: 12 << 20,
        maxDimension: 12_000,
        maxPixels: 40_000_000,
        tombstoneTTL: 7 * 24 * 60 * 60,
        maxMetadataCount: 512,
        maxImagesPerMessage: 6)

    static var defaultDirectory: URL {
        if let override = ProcessInfo.processInfo.environment["CLAWDLINE_SESSION_IMAGE_DIR"],
           !override.isEmpty {
            return URL(fileURLWithPath: Paths.expand(override), isDirectory: true)
        }
        let base = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent(
            "dev.sainteye.clawdline/session-images", isDirectory: true)
    }

    let directory: URL
    let policy: Policy

    init(directory: URL = defaultDirectory, policy: Policy = productionPolicy) {
        self.directory = directory.standardizedFileURL
        self.policy = policy
    }

    /// Prepare every input before writing any output. A malformed second image therefore cannot
    /// leave a first one behind, and every refusal happens before terminal delivery.
    func importPaths(_ paths: [String], now: Date = Date()) throws -> [Stored] {
        guard !paths.isEmpty, paths.count <= policy.maxImagesPerMessage else {
            throw Refusal(status: 400, code: "bad_request",
                          message: "images must contain 1…\(policy.maxImagesPerMessage) local files.")
        }
        let prepared = try paths.map(prepare)
        let batchBytes = prepared.reduce(0) { $0 + $1.data.count }
        guard batchBytes <= policy.maxTotalBytes else {
            throw Refusal(status: 413, code: "image_too_large",
                          message: "Those normalized images exceed the artifact-store byte limit.")
        }

        Self.lock.lock()
        defer { Self.lock.unlock() }
        do {
            try FileManager.default.createDirectory(at: directory,
                                                    withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
            try FileManager.default.setAttributes([.posixPermissions: 0o700],
                                                  ofItemAtPath: directory.path)
        } catch {
            throw Refusal(status: 500, code: "artifact_storage_failed",
                          message: "Clawdline could not open its image-artifact store.")
        }
        pruneUnlocked(now: now)

        var written: [Stored] = []
        do {
            for item in prepared {
                let id = UUID().uuidString.lowercased()
                let artifact = SessionImageArtifact(
                    id: id, mediaType: "image/png", byteCount: item.data.count,
                    width: item.width, height: item.height,
                    expiresAt: Int(now.addingTimeInterval(policy.ttl).timeIntervalSince1970))
                let metadata = Metadata(artifact: artifact,
                                        createdAt: now.timeIntervalSince1970,
                                        deletedAt: nil)
                let file = imageURL(id)
                try item.data.write(to: file, options: .atomic)
                try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                                      ofItemAtPath: file.path)
                do {
                    try write(metadata)
                } catch {
                    try? FileManager.default.removeItem(at: file)
                    throw error
                }
                written.append(Stored(artifact: artifact, file: file))
            }
            pruneUnlocked(now: now)
            return written
        } catch {
            // Nothing from a refused transaction has crossed an API boundary, so these are
            // removed completely rather than retained as public tombstones.
            for item in written {
                try? FileManager.default.removeItem(at: item.file)
                try? FileManager.default.removeItem(at: metadataURL(item.artifact.id))
            }
            throw Refusal(status: 500, code: "artifact_storage_failed",
                          message: "Clawdline could not persist the image artifacts.")
        }
    }

    func lookup(id: String, now: Date = Date()) -> Lookup {
        guard Self.isArtifactID(id) else { return .missing }
        Self.lock.lock()
        defer { Self.lock.unlock() }
        guard var metadata = readMetadata(id) else { return .missing }
        let file = imageURL(id)
        let expired = Int(now.timeIntervalSince1970) >= metadata.artifact.expiresAt
        if metadata.deletedAt != nil || expired
            || !FileManager.default.fileExists(atPath: file.path) {
            if metadata.deletedAt == nil {
                metadata.deletedAt = now.timeIntervalSince1970
                try? FileManager.default.removeItem(at: file)
                try? write(metadata)
            }
            pruneTombstonesUnlocked(now: now)
            return .expired
        }
        guard let data = try? Data(contentsOf: file), data.count == metadata.artifact.byteCount
        else {
            metadata.deletedAt = now.timeIntervalSince1970
            try? FileManager.default.removeItem(at: file)
            try? write(metadata)
            return .expired
        }
        return .live(metadata.artifact, data)
    }

    /// Turn already-published references into tombstones. Source files are never inputs here.
    func delete(ids: [String], now: Date = Date()) {
        Self.lock.lock()
        defer { Self.lock.unlock() }
        for id in ids where Self.isArtifactID(id) {
            guard var metadata = readMetadata(id) else { continue }
            try? FileManager.default.removeItem(at: imageURL(id))
            if metadata.deletedAt == nil {
                metadata.deletedAt = now.timeIntervalSince1970
                try? write(metadata)
            }
        }
        pruneTombstonesUnlocked(now: now)
    }

    func prune(now: Date = Date()) {
        Self.lock.lock()
        defer { Self.lock.unlock() }
        pruneUnlocked(now: now)
    }

    static func isArtifactID(_ id: String) -> Bool {
        guard id == id.lowercased(), let uuid = UUID(uuidString: id) else { return false }
        return uuid.uuidString.lowercased() == id
    }

    // MARK: - Decoding

    private struct Prepared {
        let data: Data
        let width: Int
        let height: Int
    }

    private func prepare(_ path: String) throws -> Prepared {
        guard path.hasPrefix("/"), !path.contains("\0") else {
            throw Refusal(status: 400, code: "invalid_image_path",
                          message: "Each image path must be one normalized absolute local path.")
        }
        let url = URL(fileURLWithPath: path)
        guard url.standardizedFileURL.path == path else {
            throw Refusal(status: 400, code: "invalid_image_path",
                          message: "Each image path must be one normalized absolute local path.")
        }
        let values: URLResourceValues
        do {
            values = try url.resourceValues(forKeys: [.isRegularFileKey, .fileSizeKey])
        } catch {
            throw Refusal(status: 400, code: "invalid_image_path",
                          message: "One local image file could not be read.")
        }
        guard values.isRegularFile == true, let inputBytes = values.fileSize else {
            throw Refusal(status: 400, code: "invalid_image_path",
                          message: "Each image path must name one regular local file.")
        }
        guard inputBytes > 0, inputBytes <= policy.maxInputBytes else {
            throw Refusal(status: 413, code: "image_too_large",
                          message: "One source image exceeds the per-image byte limit.")
        }
        guard let raw = try? Data(contentsOf: url, options: .mappedIfSafe), !raw.isEmpty,
              let source = CGImageSourceCreateWithData(raw as CFData, nil),
              CGImageSourceGetCount(source) > 0,
              let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil)
                as? [CFString: Any],
              let width = (properties[kCGImagePropertyPixelWidth] as? NSNumber)?.intValue,
              let height = (properties[kCGImagePropertyPixelHeight] as? NSNumber)?.intValue,
              width > 0, height > 0 else {
            throw Refusal(status: 415, code: "unsupported_image",
                          message: "One file was not a supported decodable raster image.")
        }
        guard width <= policy.maxDimension, height <= policy.maxDimension,
              width <= policy.maxPixels / height else {
            throw Refusal(status: 413, code: "image_too_large",
                          message: "One decoded image exceeds the pixel or dimension limit.")
        }
        guard let decoded = CGImageSourceCreateImageAtIndex(source, 0, nil),
              let png = NSBitmapImageRep(cgImage: decoded)
                .representation(using: .png, properties: [:]) else {
            throw Refusal(status: 415, code: "unsupported_image",
                          message: "One file could not be normalized to PNG.")
        }
        guard png.count <= policy.maxEncodedBytes else {
            throw Refusal(status: 413, code: "image_too_large",
                          message: "One normalized image exceeds the per-image byte limit.")
        }
        return Prepared(data: png, width: width, height: height)
    }

    // MARK: - Owned files and pruning

    private struct Metadata: Codable {
        let artifact: SessionImageArtifact
        let createdAt: TimeInterval
        var deletedAt: TimeInterval?
    }

    private static let lock = NSLock()

    private func imageURL(_ id: String) -> URL {
        directory.appendingPathComponent(id + ".png", isDirectory: false)
    }

    private func metadataURL(_ id: String) -> URL {
        directory.appendingPathComponent(id + ".json", isDirectory: false)
    }

    private func write(_ metadata: Metadata) throws {
        let data = try JSONEncoder().encode(metadata)
        let url = metadataURL(metadata.artifact.id)
        try data.write(to: url, options: .atomic)
        try FileManager.default.setAttributes([.posixPermissions: 0o600],
                                              ofItemAtPath: url.path)
    }

    private func readMetadata(_ id: String) -> Metadata? {
        guard Self.isArtifactID(id),
              let data = try? Data(contentsOf: metadataURL(id)),
              let metadata = try? JSONDecoder().decode(Metadata.self, from: data),
              metadata.artifact.id == id,
              metadata.artifact.isValidReference else { return nil }
        return metadata
    }

    private func allMetadata() -> [Metadata] {
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: directory.path)
        else { return [] }
        return names.compactMap { name in
            guard name.hasSuffix(".json") else { return nil }
            let id = String(name.dropLast(".json".count))
            return readMetadata(id)
        }
    }

    private func pruneUnlocked(now: Date) {
        let nowSeconds = now.timeIntervalSince1970
        // A process can stop between the PNG rename and its metadata rename. Reclaim only names
        // in this store's exact opaque-id namespace; unrelated files in or above the directory
        // are never candidates.
        if let names = try? FileManager.default.contentsOfDirectory(atPath: directory.path) {
            for name in names {
                if name.hasSuffix(".png") {
                    let id = String(name.dropLast(".png".count))
                    if Self.isArtifactID(id), readMetadata(id) == nil {
                        try? FileManager.default.removeItem(at: imageURL(id))
                    }
                } else if name.hasSuffix(".json") {
                    let id = String(name.dropLast(".json".count))
                    if Self.isArtifactID(id), readMetadata(id) == nil {
                        try? FileManager.default.removeItem(at: metadataURL(id))
                        try? FileManager.default.removeItem(at: imageURL(id))
                    }
                }
            }
        }
        var metadata = allMetadata()
        for index in metadata.indices where metadata[index].deletedAt == nil {
            let item = metadata[index]
            if nowSeconds >= Double(item.artifact.expiresAt)
                || !FileManager.default.fileExists(atPath: imageURL(item.artifact.id).path) {
                try? FileManager.default.removeItem(at: imageURL(item.artifact.id))
                metadata[index].deletedAt = nowSeconds
                try? write(metadata[index])
            }
        }

        var live = metadata.filter { item in
            item.deletedAt == nil
                && FileManager.default.fileExists(atPath: imageURL(item.artifact.id).path)
        }.sorted {
            if $0.createdAt != $1.createdAt { return $0.createdAt < $1.createdAt }
            return $0.artifact.id < $1.artifact.id
        }
        var bytes = live.reduce(0) { $0 + $1.artifact.byteCount }
        while live.count > policy.maxCount || bytes > policy.maxTotalBytes {
            var oldest = live.removeFirst()
            bytes -= oldest.artifact.byteCount
            try? FileManager.default.removeItem(at: imageURL(oldest.artifact.id))
            oldest.deletedAt = nowSeconds
            try? write(oldest)
        }
        pruneTombstonesUnlocked(now: now)
    }

    private func pruneTombstonesUnlocked(now: Date) {
        let nowSeconds = now.timeIntervalSince1970
        var records = allMetadata()
        var tombstones = records.filter { $0.deletedAt != nil }.sorted {
            ($0.deletedAt ?? $0.createdAt) < ($1.deletedAt ?? $1.createdAt)
        }
        for item in tombstones {
            let boundary = max(Double(item.artifact.expiresAt), item.deletedAt ?? 0)
                + policy.tombstoneTTL
            if nowSeconds >= boundary {
                try? FileManager.default.removeItem(at: metadataURL(item.artifact.id))
            }
        }
        records = allMetadata()
        tombstones = records.filter { $0.deletedAt != nil }.sorted {
            ($0.deletedAt ?? $0.createdAt) < ($1.deletedAt ?? $1.createdAt)
        }
        let excess = max(0, records.count - policy.maxMetadataCount)
        for item in tombstones.prefix(excess) {
            try? FileManager.default.removeItem(at: metadataURL(item.artifact.id))
        }
    }
}
