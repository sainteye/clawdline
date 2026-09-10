import CoreFoundation
import Foundation

/// Closed, bounded value contract for one advisory Program Plan.
///
/// The Board Store remains the only writer.  This type parses and validates a complete draft
/// before the Store mutates its copy, and its frontier is deliberately incapable of authorizing
/// broker execution.
enum ProjectBoardProgramPlan {
    static let schemaVersion = 1
    static let maximumNodes = 128
    static let maximumGates = 64
    static let maximumCapabilities = 64
    static let maximumEdgesPerNode = 64
    static let maximumClaimsPerNode = 64
    static let maximumBindings = 256
    static let reusedNodeResolution = "reused_imported_program_node"

    struct Refusal: Error {
        let status: Int
        let code: String
        let message: String
    }

    struct Document: Codable, Equatable {
        var documentId: String
        var version: Int
        var title: String
        var url: String
        var supersedesId: String?
    }

    struct Capability: Codable, Equatable {
        var key: String
        var status: String
        var observedAt: Double
        var lastImportedVersion: Int
    }

    struct GateApproval: Codable, Equatable {
        var decision: String
        var planVersion: Int
        var actor: String
        var note: String
        var decidedAt: Double
    }

    struct Gate: Codable, Equatable {
        var key: String
        var title: String
        var authority: String
        var approval: GateApproval?
        var lastImportedVersion: Int
    }

    struct Node: Codable, Equatable {
        var key: String
        var graphNodeId: String
        var itemId: String
        var title: String
        var type: String
        var summary: String
        var owner: String
        var dependsOn: [String]
        var gateKeys: [String]
        var capabilityKeys: [String]
        var claims: [String]
        var lastImportedVersion: Int
    }

    struct Binding: Codable, Equatable {
        var receiptId: String
        var runId: String
        var requestedClassification: String
        var requestedItemId: String
        var effectiveItemId: String
        var sessionId: String
        var provider: String
        var processGeneration: String
        var planVersion: Int
        var graphId: String
        var nodeId: String
        var boardRevision: Int
        var settledAt: Double
        var resolution: String
    }

    struct Record: Codable, Equatable {
        var schemaVersion: Int
        var programKey: String
        var planId: String
        var planVersion: Int
        var predecessorVersion: Int?
        var graphId: String
        var destination: String
        var document: Document
        var documentReferenceId: String
        var nodes: [Node]
        var gates: [Gate]
        var capabilities: [Capability]
        var bindings: [Binding]?
        var importedAt: Double
        var actor: String
    }

    struct Draft {
        var programKey: String
        var planId: String
        var planVersion: Int
        var predecessorVersion: Int?
        var graphId: String
        var destination: String
        var document: Document
        var nodes: [Node]
        var gates: [Gate]
        var capabilities: [Capability]
    }

    static func parse(_ body: [String: Any]) throws -> Draft {
        guard exactInt(body["schemaVersion"]) == schemaVersion,
              let programKey = text(body["programKey"], maximum: 64),
              let planId = identifier(body["planId"]),
              let planVersion = exactInt(body["planVersion"]), planVersion > 0,
              let graphId = identifier(body["graphId"]),
              let destination = text(body["destination"], maximum: 1_000),
              let documentObject = body["document"] as? [String: Any],
              let nodeObjects = body["nodes"] as? [[String: Any]],
              let gateObjects = body["gates"] as? [[String: Any]],
              let capabilityObjects = body["capabilities"] as? [[String: Any]],
              nodeObjects.count <= maximumNodes, gateObjects.count <= maximumGates,
              capabilityObjects.count <= maximumCapabilities else {
            throw refusal("program_plan_invalid", "The Program Plan header and bounded collections are required.")
        }
        let predecessor: Int?
        if body.keys.contains("predecessorVersion") {
            guard let value = exactInt(body["predecessorVersion"]), value > 0 else {
                throw refusal("program_plan_predecessor_invalid", "predecessorVersion must be a positive integer.")
            }
            predecessor = value
        } else { predecessor = nil }
        let document = try parseDocument(documentObject)
        var nodes: [Node] = []
        for object in nodeObjects { nodes.append(try parseNode(object, version: planVersion)) }
        var gates: [Gate] = []
        for object in gateObjects { gates.append(try parseGate(object, version: planVersion)) }
        var capabilities: [Capability] = []
        for object in capabilityObjects {
            capabilities.append(try parseCapability(object, version: planVersion))
        }
        guard Set(nodes.map(\.key)).count == nodes.count,
              Set(nodes.map(\.graphNodeId)).count == nodes.count,
              Set(gates.map(\.key)).count == gates.count,
              Set(capabilities.map(\.key)).count == capabilities.count else {
            throw refusal("program_plan_duplicate_key", "Logical, graph-node, gate and capability keys must be unique.")
        }
        return Draft(programKey: programKey, planId: planId, planVersion: planVersion,
                     predecessorVersion: predecessor, graphId: graphId,
                     destination: destination, document: document, nodes: nodes,
                     gates: gates, capabilities: capabilities)
    }

    static func validateMerged(nodes: [Node], gates: [Gate],
                               capabilities: [Capability]) throws {
        guard nodes.count <= maximumNodes, gates.count <= maximumGates,
              capabilities.count <= maximumCapabilities,
              Set(nodes.map(\.key)).count == nodes.count,
              Set(nodes.map(\.graphNodeId)).count == nodes.count,
              Set(gates.map(\.key)).count == gates.count,
              Set(capabilities.map(\.key)).count == capabilities.count else {
            throw refusal("program_plan_duplicate_key", "The merged Program Plan contains duplicate or excessive keys.")
        }
        let nodeKeys = Set(nodes.map(\.key))
        let gateKeys = Set(gates.map(\.key))
        guard nodes.allSatisfy({ Set($0.dependsOn).isSubset(of: nodeKeys)
            && Set($0.gateKeys).isSubset(of: gateKeys) }) else {
            throw refusal("program_plan_reference_invalid", "Every dependency and gate must name a retained Program Plan row.")
        }
        var visiting: Set<String> = [], visited: Set<String> = []
        let dependencies = Dictionary(uniqueKeysWithValues: nodes.map { ($0.key, $0.dependsOn) })
        func visit(_ key: String) -> Bool {
            if visiting.contains(key) { return false }
            if visited.contains(key) { return true }
            visiting.insert(key)
            for dependency in dependencies[key] ?? [] where !visit(dependency) { return false }
            visiting.remove(key); visited.insert(key)
            return true
        }
        guard nodes.allSatisfy({ visit($0.key) }) else {
            throw refusal("program_plan_cycle", "Program dependencies must form a directed acyclic graph.")
        }
    }

    static func validRecord(_ record: Record, programItemID: String,
                            itemIDs: Set<String>, maximumBoardRevision: Int) -> Bool {
        let bindings = record.bindings ?? []
        guard record.schemaVersion == schemaVersion,
              text(record.programKey, maximum: 64) == record.programKey,
              identifier(record.planId) == record.planId,
              record.planVersion > 0,
              (record.planVersion == 1 && record.predecessorVersion == nil)
                || (record.planVersion > 1
                    && record.predecessorVersion == record.planVersion - 1),
              identifier(record.graphId) == record.graphId,
              text(record.destination, maximum: 1_000) == record.destination,
              validDocument(record.document),
              identifier(record.documentReferenceId) == record.documentReferenceId,
              record.importedAt.isFinite, record.importedAt >= 0,
              text(record.actor, maximum: 300) == record.actor,
              record.nodes.allSatisfy({ validNode($0, planVersion: record.planVersion)
                  && itemIDs.contains($0.itemId) }),
              record.gates.allSatisfy({ validGate($0, planVersion: record.planVersion) }),
              record.capabilities.allSatisfy({
                  validCapability($0, planVersion: record.planVersion)
              }),
              bindings.count <= maximumBindings,
              Set(bindings.map(\.receiptId)).count == bindings.count,
              Set(bindings.map(\.runId)).count == bindings.count,
              bindings.allSatisfy({ binding in
                  identifier(binding.receiptId) == binding.receiptId
                      && text(binding.runId, maximum: 200) == binding.runId
                      && binding.requestedClassification == "existing_item"
                      && binding.requestedItemId == programItemID
                      && itemIDs.contains(binding.effectiveItemId)
                      && UUID(uuidString: binding.sessionId) != nil
                      && ["codex", "claude"].contains(binding.provider)
                      && text(binding.processGeneration, maximum: 300)
                        == binding.processGeneration
                      && binding.planVersion > 0
                      && binding.planVersion <= record.planVersion
                      && binding.graphId == record.graphId
                      && identifier(binding.nodeId) == binding.nodeId
                      && binding.boardRevision >= 0
                      && binding.boardRevision <= maximumBoardRevision
                      && binding.settledAt.isFinite && binding.settledAt >= 0
                      && binding.resolution == reusedNodeResolution
                      && record.nodes.contains(where: {
                          $0.itemId == binding.effectiveItemId
                              && $0.graphNodeId == binding.nodeId
                      })
              }) else { return false }
        do {
            try validateMerged(nodes: record.nodes, gates: record.gates,
                               capabilities: record.capabilities)
            return true
        } catch { return false }
    }

    static func projection(_ record: Record, progressByItemID: [String: String]) -> [String: Any] {
        let capabilities = Dictionary(uniqueKeysWithValues: record.capabilities.map { ($0.key, $0) })
        let gates = Dictionary(uniqueKeysWithValues: record.gates.map { ($0.key, $0) })
        let nodes = Dictionary(uniqueKeysWithValues: record.nodes.map { ($0.key, $0) })
        var states: [String: (String, [String])] = [:]
        var readyKeys: Set<String> = []
        for node in record.nodes {
            let own = progressByItemID[node.itemId] ?? "unknown"
            if ["landed", "settled"].contains(own) {
                states[node.key] = ("blocked", ["already_settled"]); continue
            }
            var blocked: [String] = [], unknown: [String] = []
            for dependencyKey in node.dependsOn {
                guard let dependency = nodes[dependencyKey] else {
                    unknown.append("dependency_missing:\(dependencyKey)"); continue
                }
                let progress = progressByItemID[dependency.itemId] ?? "unknown"
                if progress == "unknown" { unknown.append("dependency_unknown:\(dependencyKey)") }
                else if !["landed", "settled"].contains(progress) {
                    blocked.append("dependency_incomplete:\(dependencyKey)")
                }
            }
            for gateKey in node.gateKeys {
                guard let gate = gates[gateKey] else { blocked.append("gate_missing:\(gateKey)"); continue }
                guard gate.lastImportedVersion == record.planVersion else {
                    blocked.append("gate_stale:\(gateKey)"); continue
                }
                guard let approval = gate.approval,
                      approval.planVersion == record.planVersion else {
                    blocked.append("gate_pending:\(gateKey)"); continue
                }
                if approval.decision != "approved" { blocked.append("gate_rejected:\(gateKey)") }
            }
            for capabilityKey in node.capabilityKeys {
                guard let capability = capabilities[capabilityKey] else {
                    unknown.append("capability_unknown:\(capabilityKey)"); continue
                }
                if capability.lastImportedVersion != record.planVersion {
                    unknown.append("capability_stale:\(capabilityKey)")
                } else if capability.status == "unsupported" {
                    blocked.append("capability_unsupported:\(capabilityKey)")
                } else if capability.status == "unknown" {
                    unknown.append("capability_unknown:\(capabilityKey)")
                }
            }
            if !blocked.isEmpty { states[node.key] = ("blocked", blocked.sorted()) }
            else if !unknown.isEmpty { states[node.key] = ("unknown", unknown.sorted()) }
            else { states[node.key] = ("planning_ready", []); readyKeys.insert(node.key) }
        }
        var claimOwners: [String: [String]] = [:]
        for node in record.nodes where readyKeys.contains(node.key) {
            for claim in node.claims { claimOwners[claim, default: []].append(node.key) }
        }
        for (claim, owners) in claimOwners where owners.count > 1 {
            for key in owners {
                states[key] = ("blocked", ["claim_conflict:\(claim)"]); readyKeys.remove(key)
            }
        }
        let nodeRows: [[String: Any]] = record.nodes.map { node in
            let value = states[node.key] ?? ("unknown", ["projection_unknown"])
            return ["key": node.key, "graphNodeId": node.graphNodeId, "itemId": node.itemId,
                    "title": node.title, "type": node.type, "summary": node.summary,
                    "owner": node.owner, "dependsOn": node.dependsOn, "gateKeys": node.gateKeys,
                    "capabilityKeys": node.capabilityKeys, "claims": node.claims,
                    "lastImportedVersion": node.lastImportedVersion,
                    "planningState": value.0, "basisCodes": value.1] as [String: Any]
        }
        let gateRows: [[String: Any]] = record.gates.map { gate in
            var row: [String: Any] = ["key": gate.key, "title": gate.title,
                "authority": gate.authority, "lastImportedVersion": gate.lastImportedVersion]
            if let approval = gate.approval {
                row["approval"] = ["decision": approval.decision,
                    "planVersion": approval.planVersion, "actor": approval.actor,
                    "note": approval.note, "decidedAt": approval.decidedAt] as [String: Any]
            } else { row["approval"] = NSNull() }
            return row
        }
        let capabilityRows: [[String: Any]] = record.capabilities.map {
            ["key": $0.key, "status": $0.status, "observedAt": $0.observedAt,
             "lastImportedVersion": $0.lastImportedVersion]
        }
        let bindingRows: [[String: Any]] = (record.bindings ?? []).suffix(64).map {
            ["receiptId": $0.receiptId, "runId": $0.runId,
             "requestedClassification": $0.requestedClassification,
             "requestedItemId": $0.requestedItemId, "effectiveItemId": $0.effectiveItemId,
             "sessionId": $0.sessionId, "provider": $0.provider,
             "processGeneration": $0.processGeneration, "planVersion": $0.planVersion,
             "graphId": $0.graphId, "nodeId": $0.nodeId,
             "boardRevision": $0.boardRevision, "settledAt": $0.settledAt,
             "resolution": $0.resolution,
             "authority": "advisory_only"] as [String: Any]
        }
        return ["schemaVersion": record.schemaVersion, "programKey": record.programKey,
                "planId": record.planId, "planVersion": record.planVersion,
                "predecessorVersion": record.predecessorVersion as Any? ?? NSNull(),
                "graphId": record.graphId, "destination": record.destination,
                "documentReferenceId": record.documentReferenceId,
                "nodes": nodeRows, "gates": gateRows, "capabilities": capabilityRows,
                "bindingReceipts": bindingRows,
                "frontier": ["states": ["planning_ready", "blocked", "unknown"],
                    "nodes": record.nodes.filter { readyKeys.contains($0.key) }.map(\.key),
                    "authority": "advisory_only", "executionAuthority": false,
                    "criticalPath": NSNull()] as [String: Any],
                "importedAt": record.importedAt, "actor": record.actor]
    }

    private static func parseDocument(_ object: [String: Any]) throws -> Document {
        let allowed = Set(["documentId", "version", "title", "url", "supersedesId"])
        guard Set(object.keys).isSubset(of: allowed),
              let documentId = identifier(object["documentId"]),
              let version = exactInt(object["version"]), version > 0,
              let title = text(object["title"], maximum: 300),
              let url = text(object["url"], maximum: 2_000) else {
            throw refusal("program_plan_document_invalid", "The planning document uses a closed versioned schema.")
        }
        let supersedes: String?
        if object.keys.contains("supersedesId") {
            guard let value = text(object["supersedesId"], maximum: 200) else {
                throw refusal("program_plan_document_invalid", "supersedesId must be a bounded reference identity.")
            }
            supersedes = value
        } else { supersedes = nil }
        return Document(documentId: documentId, version: version, title: title,
                        url: url, supersedesId: supersedes)
    }

    private static func parseNode(_ object: [String: Any], version: Int) throws -> Node {
        let exact = Set(["key", "graphNodeId", "title", "type", "summary", "owner",
                         "dependsOn", "gateKeys", "capabilityKeys", "claims"])
        guard Set(object.keys) == exact, let key = identifier(object["key"]),
              let graphNodeId = identifier(object["graphNodeId"]),
              let title = text(object["title"], maximum: 300),
              let type = text(object["type"], maximum: 32),
              ["feature", "refactor", "task", "bug", "coordination"].contains(type),
              let summary = string(object["summary"], maximum: 4_000),
              let owner = string(object["owner"], maximum: 300),
              let dependsOn = identifierArray(object["dependsOn"], maximum: maximumEdgesPerNode),
              let gateKeys = identifierArray(object["gateKeys"], maximum: maximumEdgesPerNode),
              let capabilityKeys = identifierArray(object["capabilityKeys"], maximum: maximumEdgesPerNode),
              let claims = claimArray(object["claims"], maximum: maximumClaimsPerNode),
              !dependsOn.contains(key) else {
            throw refusal("program_plan_node_invalid", "Every Program node must satisfy the closed bounded schema.")
        }
        return Node(key: key, graphNodeId: graphNodeId, itemId: "", title: title, type: type,
                    summary: summary, owner: owner, dependsOn: dependsOn, gateKeys: gateKeys,
                    capabilityKeys: capabilityKeys, claims: claims, lastImportedVersion: version)
    }

    private static func parseGate(_ object: [String: Any], version: Int) throws -> Gate {
        guard Set(object.keys) == Set(["key", "title", "authority"]),
              let key = identifier(object["key"]),
              let title = text(object["title"], maximum: 300),
              let authority = text(object["authority"], maximum: 300) else {
            throw refusal("program_plan_gate_invalid", "Gate definitions cannot import approval authority or status.")
        }
        return Gate(key: key, title: title, authority: authority, approval: nil,
                    lastImportedVersion: version)
    }

    private static func parseCapability(_ object: [String: Any], version: Int) throws -> Capability {
        guard Set(object.keys) == Set(["key", "status", "observedAt"]),
              let key = identifier(object["key"]),
              let status = text(object["status"], maximum: 32),
              ["supported", "unsupported", "unknown"].contains(status),
              let observedAt = exactDouble(object["observedAt"]) else {
            throw refusal("program_plan_capability_invalid", "Capability observations require key, status and observedAt.")
        }
        return Capability(key: key, status: status, observedAt: observedAt,
                          lastImportedVersion: version)
    }

    private static func validDocument(_ document: Document) -> Bool {
        identifier(document.documentId) == document.documentId
            && document.version > 0
            && text(document.title, maximum: 300) == document.title
            && text(document.url, maximum: 2_000) == document.url
            && (document.supersedesId.map { text($0, maximum: 200) == $0 } ?? true)
    }

    private static func validNode(_ node: Node, planVersion: Int) -> Bool {
        identifier(node.key) == node.key
            && identifier(node.graphNodeId) == node.graphNodeId
            && identifier(node.itemId) == node.itemId
            && text(node.title, maximum: 300) == node.title
            && ["feature", "refactor", "task", "bug", "coordination"].contains(node.type)
            && string(node.summary, maximum: 4_000) == node.summary
            && string(node.owner, maximum: 300) == node.owner
            && identifierArray(node.dependsOn, maximum: maximumEdgesPerNode) != nil
            && identifierArray(node.gateKeys, maximum: maximumEdgesPerNode) != nil
            && identifierArray(node.capabilityKeys, maximum: maximumEdgesPerNode) != nil
            && claimArray(node.claims, maximum: maximumClaimsPerNode) != nil
            && !node.dependsOn.contains(node.key)
            && node.lastImportedVersion > 0 && node.lastImportedVersion <= planVersion
    }

    private static func validGate(_ gate: Gate, planVersion: Int) -> Bool {
        guard identifier(gate.key) == gate.key,
              text(gate.title, maximum: 300) == gate.title,
              text(gate.authority, maximum: 300) == gate.authority,
              gate.lastImportedVersion > 0,
              gate.lastImportedVersion <= planVersion else { return false }
        guard let approval = gate.approval else { return true }
        return ["approved", "rejected"].contains(approval.decision)
            && approval.planVersion == planVersion
            && gate.lastImportedVersion == planVersion
            && approval.actor == gate.authority
            && text(approval.note, maximum: 1_000) == approval.note
            && approval.decidedAt.isFinite && approval.decidedAt >= 0
    }

    private static func validCapability(_ capability: Capability, planVersion: Int) -> Bool {
        identifier(capability.key) == capability.key
            && ["supported", "unsupported", "unknown"].contains(capability.status)
            && capability.observedAt.isFinite && capability.observedAt >= 0
            && capability.lastImportedVersion > 0
            && capability.lastImportedVersion <= planVersion
    }

    private static func identifierArray(_ raw: Any?, maximum: Int) -> [String]? {
        guard let values = raw as? [Any], values.count <= maximum else { return nil }
        let result = values.compactMap(identifier)
        return result.count == values.count && Set(result).count == result.count ? result : nil
    }

    private static func claimArray(_ raw: Any?, maximum: Int) -> [String]? {
        guard let values = raw as? [Any], values.count <= maximum else { return nil }
        let result = values.compactMap { value -> String? in
            guard let path = text(value, maximum: 512), !path.hasPrefix("/"),
                  !path.split(separator: "/", omittingEmptySubsequences: false).contains(".."),
                  !path.contains("\\") else { return nil }
            return path
        }
        return result.count == values.count && Set(result).count == result.count ? result : nil
    }

    private static func identifier(_ raw: Any?) -> String? {
        guard let value = text(raw, maximum: 80),
              value.unicodeScalars.allSatisfy({ scalar in
                  (scalar.value >= 48 && scalar.value <= 57)
                      || (scalar.value >= 65 && scalar.value <= 90)
                      || (scalar.value >= 97 && scalar.value <= 122)
                      || [45, 46, 95].contains(scalar.value)
              }) else { return nil }
        return value
    }

    private static func text(_ raw: Any?, maximum: Int) -> String? {
        guard let value = raw as? String else { return nil }
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return !trimmed.isEmpty && trimmed.utf8.count <= maximum ? trimmed : nil
    }

    private static func string(_ raw: Any?, maximum: Int) -> String? {
        guard let value = raw as? String, value.utf8.count <= maximum else { return nil }
        return value.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private static func exactInt(_ raw: Any?) -> Int? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
        let value = number.intValue
        return number.doubleValue == Double(value) ? value : nil
    }

    private static func exactDouble(_ raw: Any?) -> Double? {
        guard let number = raw as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID(), number.doubleValue.isFinite,
              number.doubleValue >= 0 else { return nil }
        return number.doubleValue
    }

    private static func refusal(_ code: String, _ message: String,
                                status: Int = 400) -> Refusal {
        Refusal(status: status, code: code, message: message)
    }
}
