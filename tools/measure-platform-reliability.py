#!/usr/bin/env python3
"""W0-C characterization, not a production readiness gate (Python standard library).

Source-only output is deterministic. The reviewed input hashes deliberately reject *any*
source drift: this is a frozen baseline, not a Swift parser or a general reliability linter.
Re-characterize the affected claims before updating a hash. Optional probes touch only
an explicit scratch directory and unauthenticated loopback GET /v1/health.
"""

import argparse
import ast
from datetime import datetime, timezone
import errno
import hashlib
import http.client
import json
import os
from pathlib import Path
import platform
import re
import sys
import tempfile
import time
import uuid


BASE_COMMIT = "e9939b55b0ca4705b3b880ddaaccc9921363d689"
# Measurement safety bound, not a production HTTP budget or SLA.
HEALTH_BODY_LIMIT = 16384
# Whole-file seals prevent an unchanged matching line from concealing a changed caller,
# guard, comment-only decoy, or failure path elsewhere in the reviewed file.
SOURCE_SEALS = {
    "Sources/CloudTransport.swift": "1996000707852cac1a71b2a9622cca7aea996e79d000d7d1ddc9f4175425465d",
    "Sources/CloudAppBridge.swift": "853c4431771550175c6c485b9da648edb72ef1d24b350030402dee74785aaabc",
    "Sources/RemoteServer.swift": "1386d9decd40ae711116bdd3cdad22a34fb490dda314938f5c62523b178a0d36",
    "Sources/TerminalCommandScheduler.swift": "c9a423b31b8e8d374c26ea97b999a73f3e6cbceed00e31519147f9b28fafcd7a",
    "Sources/Orchestrator.swift": "1b9b54e96d3608faa4e6e761020548b2904cd7e812c1ea42267fdd687fdde928",
    "Sources/OrchestratorPersistence.swift": "375628d34df7a2b7679b1e5519d2cfd81f92a3ba86df62b12ae95dfd43d9482c",
    "Sources/OrchestratorRegistry.swift": "f0022c5cefc99a26e7ae278f1e286025e693c32ffdf0c286fa7361142e9ec27b",
    "Sources/OrchestratorStore.swift": "ed31e8ceb7aab18aee23efdf8c3a20805e61a576f89673a4b79deeffd58bcfcc",
    "Sources/Coordinator.swift": "8c465b30b97aacd421de00db92416712e7b16f556ade97d0ebb16d39ea3f69ae",
    "Sources/SessionWatch.swift": "d1d47930535b2e79bea14d13d56482885adeff09fcaa0326b84f8a60701beb5c",
    "Sources/TranscriptReadCoordinator.swift": "61c1adf7558d54bff495128b78450b849eb48dd69bcddd724148b72f855cd371",
    "Sources/ReadingFreshness.swift": "4592b2a84c03d196707626df2876f3ef4e5274149addf361b7738a2888ed672f",
    "Sources/CloudOutboundSpool.swift": "aef545decd4c7c95b5c566cb60cb11455eaadf6b52f84c3707b76df165459485",
    "Sources/CloudCommandLedger.swift": "def726c029d4fb17e0d096c3187d87b2ea89fdc413e69b95bb02cf0a27862fcf",
    "Sources/CloudBridgeLifecycle.swift": "07f585fd60b4e99abe42d89ca72087c6339456cb844b088718c503cd9ef6b663",
    "Tests/OrchestratorRecoveryTests.swift": "c15ff1acdc556cdf914ad774892c4818117a60709be06086810ce014db3ea753",
    "Tests/CloudOutboundSpoolTests.swift": "da5322716a38ed3732fd113afdf507587c11f033e0a30d32f91be227b34fa244",
}


class MeasurementError(Exception):
    def __init__(self, code, detail):
        super().__init__(detail)
        self.code = code


def require(condition, code, detail):
    if not condition:
        raise MeasurementError(code, detail)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


class SourceEvidence:
    def __init__(self, root):
        self.text = {}
        self.manifest = []
        for name, expected in sorted(SOURCE_SEALS.items()):
            try:
                data = (root / name).read_bytes()
                text = data.decode("utf-8", errors="strict")
            except (OSError, UnicodeError) as error:
                raise MeasurementError("source_unreadable", name) from error
            require(bool(text.strip()), "source_empty", name)
            observed = digest(data)
            if observed != expected:
                raise MeasurementError("source_drift", name + ": re-characterize before resealing")
            self.text[name] = text
            self.manifest.append({"path": name, "sha256": observed, "bytes": len(data)})
        require(bool(self.manifest), "zero_observations", "source manifest is empty")

    def ref(self, name, anchor, end=None):
        text = self.text[name]
        require(text.count(anchor) == 1, "source_shape_changed", name + ": non-unique anchor")
        start = text.index(anchor)
        stop = start + len(anchor)
        if end is not None:
            require(text.count(end) == 1, "source_shape_changed", name + ": non-unique end")
            stop = text.index(end)
            require(stop > start, "source_shape_changed", name + ": reversed range")
        return {"path": name, "line": text.count("\n", 0, start) + 1,
                "end_line": text.count("\n", 0, stop) + 1,
                "sha256": digest(text[start:stop].encode()), "anchor": anchor}

    def integer(self, name, symbol):
        matches = list(re.finditer(
            r"^\s*(?:public |private )?(?:static )?(?:let|var) " + re.escape(symbol)
            + r"(?:\s*:\s*[A-Za-z0-9]+)?\s*=\s*([^\n]+)$", self.text[name], re.M))
        require(len(matches) == 1, "source_shape_changed", name + ": " + symbol)
        expression = matches[0].group(1).strip()
        try:
            tree = ast.parse(expression, mode="eval")
        except SyntaxError as error:
            raise MeasurementError("constant_parse_failed", symbol) from error

        def evaluate(node):
            if isinstance(node, ast.Constant) and type(node.value) is int:
                return node.value
            if isinstance(node, ast.BinOp) and isinstance(node.op, (ast.Mult, ast.LShift)):
                left, right = evaluate(node.left), evaluate(node.right)
                require(0 <= right <= (64 if isinstance(node.op, ast.LShift) else 2 ** 32),
                        "constant_parse_failed", symbol)
                return left * right if isinstance(node.op, ast.Mult) else left << right
            raise MeasurementError("constant_parse_failed", symbol)

        value = evaluate(tree.body)
        require(value > 0, "constant_parse_failed", symbol)
        return {"value": value, "expression": expression,
                "source": self.ref(name, matches[0].group().strip())}


def unobserved(status):
    return {"status": status, "observation_count": None}


def characterize(source):
    rows = []
    t, b, h, o = ("Sources/" + name + ".swift" for name in
                  ("CloudTransport", "CloudAppBridge", "RemoteServer", "Orchestrator"))
    terminal = "Sources/TerminalCommandScheduler.swift"
    c, w, p, l = ("Sources/" + name + ".swift" for name in
                  ("Coordinator", "SessionWatch", "CloudOutboundSpool", "CloudCommandLedger"))
    op = "Sources/OrchestratorPersistence.swift"

    def row(key, finding, refs, unknown, owner, values=None):
        rows.append({"id": key, "finding": finding, "values": values or {},
                     "source-derived": refs,
                     "executable-test-derived": unobserved("not-run"),
                     "measured-runtime": unobserved("not-measured"),
                     "unknown": unknown, "downstream_owner": owner})

    def ref(name, anchor, end=None):
        return source.ref(name, anchor, end)

    def constants(name, *symbols):
        return {symbol: source.integer(name, symbol) for symbol in symbols}

    row("cloud-ingress", "commands 未指定 bufferingPolicy；acceptInbound 忽略 yield 回傳。"
        "bridge 逐筆 await consume，再 await commandRouter；worker 上限不等於 ingress 上限。",
        [ref(t, "commands = AsyncStream { continuation = $0 }"),
         ref(t, "    private func acceptInbound(", "    private func dropInbound("),
         ref(b, "            let commandStream = transport.commands", "            let readyStream = transport.readyGenerations"),
         ref(b, "        let result = await commandRouter.route(", "        commandResult(result)")],
        ["Swift 預設 unbounded 語意為來源推論，未執行 Swift；未量到實際 backlog/RSS、bytes、wait/work。",
         "authenticated command 的穩定 retry identity、durable re-read 與 typed overload 尚須 R-1 設計。"],
        "R-1 transport reliability owner", {"explicit_count_cap": None, "explicit_byte_cap": None,
                                             "yield_result_handled": False})
    row("cloud-ready", "readyGenerations 明定 bufferingNewest(1)，dropped 計數；與 commands 是不同流。",
        [ref(t, "        readyGenerations = AsyncStream(bufferingPolicy: .bufferingNewest(1)) {"),
         ref(t, "            switch readyContinuation.yield(UInt64(currentGeneration)) {", "            return (authenticated, currentGeneration)")],
        ["沒有執行 ready-generation 消費／丟棄測試；不能把此策略當成 command 可丟棄政策。"],
        "R-1 transport reliability owner", {"pending_generations": 1})
    row("http-admission", "accept 直接 start/receive；這條 accept/receive 路徑沒有全域連線、"
        "aggregate body bytes 或 request-read deadline admission。response close grace 是另一階段。",
        [ref(h, "    private func accept(_ conn:", "    /// Read until the headers"),
         ref(h, "    private func receive(_ conn:", "    /// The largest request body"),
         ref(h, "    private static let responseCloseGraceSeconds = 30")],
        ["NWListener/kernel 上限、同時連線數、記憶體峰值與 slowloris 行為未量測。",
         "64 KiB 是未找到 header terminator 時的條件檢查，不能稱所有完整 header 的嚴格上限。"],
        "R-2 HTTP reliability owner", constants(h, "bodyLimit", "responseCloseGraceSeconds"))
    row("sse-output", "Stream 只保存 connection；openStream 登錄串流，授權檢查沒有容量條件；"
        "write 與 heartbeat 的 contentProcessed 忽略 error，沒有此層 outstanding-byte 計數、"
        "slow-consumer eviction 或待送 snapshot 合併。",
        [ref(h, "    private final class Stream {", "    static func restartHelloPayload()"),
         ref(h, "    private func openStream(on", "    /// A comment line every fifteen seconds"),
         ref(h, "    func eventStreamRefusal(", "    /// A JSON object body"),
         ref(h, "    private func startHeartbeat()", "    /// Called on the main thread by the watch"),
         ref(h, "    private func write(event:", "    // MARK: - Writing a response out")],
        ["未開 authenticated SSE 或使真實 consumer 停讀；連線／待送 bytes、kernel buffer 與 callback 延遲未知。",
         "Network 可能有下層背壓，來源不能證明本層已受保護；publication 合併不是 local SSE 合併。"],
        "R-2 HTTP reliability owner", {"connection_cap": None, "outstanding_byte_cap": None})
    row("sse-reconnect", "openStream 送 hello 與當前 sessions/orchestrator 全量 snapshot；"
        "write 分配 event id。重連以當前狀態 realign，這些路徑沒有 Last-Event-ID replay。",
        [ref(h, "    private func openStream(on", "    /// A comment line every fifteen seconds"),
         ref(h, "    private func write(event:", "    // MARK: - Writing a response out")],
        ["未測斷線重連、遺失事件與 viewer 觀察；snapshot 送出不等於 command effect 完成／ACK。"],
        "R-2 HTTP reliability owner")
    row("terminal-queue", "同一 serial terminal worker 在入列前計 total/per-channel outstanding；"
        "HTTP 容量拒絕為 429 busy，maintenance 為 503 restart_maintenance；nested inline 仍計新增 channel。",
        [ref(terminal, "    @discardableResult\n    func enqueue(channels rawChannels:",
             "    /// Test receipt for the production admission counters"),
         ref(h, "    func enqueueTerminalCommand(channels", "    /// Test receipt for the production admission counters"),
         ref(h, "    private func terminalMutation(", "    private static func keepsTerminalMutation("),
         ref(c, "    func terminalMaintenanceRefusal()", "    func beginRestartMaintenance(requestID:")],
        ["count 是 queued+active，不是 byte cap；8/2 為現行常數，沒有量到負載適足性。",
         "同 key terminalPending waiters 另外 append，沒有此路徑的 waiter count/byte cap；重試可增加連線債務。"],
        "W2-1 application owner / R-1 / R-2", {
            "terminalDepth": constants(terminal, "depth")["depth"],
            "terminalChannelDepth": constants(terminal, "channelDepth")["channelDepth"],
        })
    row("read-queues", "slow reads/analytics、transcript、voice/planner 有獨立 admission；"
        "overflow 分別為 429 busy/usage_analytics_busy/transcript_busy，voice/planner 亦回 429 busy。"
        "列出的 depth 都是 request 數量；沒有把單一 body cap 當 aggregate queued-byte cap。",
        [ref(h, "    struct ReadingLimiter {", "    /// Eight, shared only"),
         ref(h, "    static func transcriptBusyResponse(", "    /// Authenticate and encode at the transport boundary"),
         ref(h, "        guard voiceQueued < Self.voiceDepth else {"),
         ref(h, "        guard planQueued < Self.planDepth else {"),
         ref("Sources/TranscriptReadCoordinator.swift", "    struct Limiter {", "    // Interactive reads and agent/background")],
        ["未測 queue wait/work、current debt 或 bytes；count ceiling 不是延遲保證。"],
        "R-2 HTTP reliability owner", {
            **constants(h, "readingDepth", "usageAnalyticsDepth", "voiceDepth", "planDepth"),
            **constants("Sources/TranscriptReadCoordinator.swift", "depth", "backgroundDepth")})
    row("coalesced-read-waiters", "FreshReadings 對同 key 共用一次 refresh，但會 append 每個 waiter；"
        "因此 worker count cap 不能證明 parked replies 有界。",
        [ref("Sources/ReadingFreshness.swift", "        waiters[key, default: []].append(deliver)"),
         ref("Sources/ReadingFreshness.swift", "    private func revalidate(", "    /// Called on the owner queue with a completed read")],
        ["waiter 數量、closure 持有 bytes、disconnect 後回收及重複 retry 負載未測。"],
        "R-2 HTTP reliability owner")
    row("cloud-read-queues", "Cloud read 的 foreground/background 分別按 queued+active 計 4/16；"
        "超額回 429 cloud_read_busy，拒絕回覆另以 Task publish。",
        [ref(b, "    private func enqueueRead(", "    private func publishBusyRead(")],
        ["沒有此路徑的 aggregate byte ceiling；拒絕回覆 Tasks 的數量／輸出債務未測。"],
        "R-1 transport reliability owner", {"foreground_requests": 4, "background_requests": 16})
    row("cloud-publication", "CloudSnapshotPublicationQueue 合併 snapshot，保留 authoritative barrier；"
        "最多 3 個 pending（active work 另計）。CloudTransport pendingByChannel 按 channel 保留最新 envelope。",
        [ref(b, "final class CloudSnapshotPublicationQueue:", "    @discardableResult\n    func cancelAndReset()"),
         ref(t, "    private var pendingByChannel: [String: CloudEnvelope] = [:]"),
         ref(t, "    func publish(envelope:", "    func shutdown()")],
        ["未量到 pending bytes；未證明 channel cardinality 有界；不能套用為 local SSE 或 command ingress 保證。"],
        "R-1 / W5-1 durability owner", constants(b, "maximumPending"))
    row("spool-component", "CloudOutboundSpool 元件預設有 row/charged-byte caps，"
        "admission 比較 candidate commit 後數量並回 typed rowCap/byteCap scope。",
        [ref(p, "public struct CloudSpoolLimits:", "// MARK: - Metrics"),
         ref(p, "        let withinRecipientRows =", "        let seq = working.nextSeq"),
         ref("Tests/CloudOutboundSpoolTests.swift", "restart burns every sent row — old continuous instants are incomparable")],
        ["此元件數值不證明 production transport 接線或實際 occupancy；本工具沒有驗證接線。",
         "charged bytes 不是 sealed envelope bytes 或 process RSS；runtime override 與負載 adequacy 未測。"],
        "W5-1 cross-runtime durability owner", constants(p, "globalRowCap", "globalByteCap", "recipientRowCap", "recipientByteCap",
                                                       "fairnessRecipientRowCap", "fairnessRecipientByteCap"))
    row("ledger-component", "CloudCommandLedger 元件有 global/actor/fairness row caps，"
        "refuseCapacity 計數並丟出 idempotencyCapacity(scope:reason:)。",
        [ref(l, "    public static let globalHardLimit ="),
         ref(l, "                let actorCount =", "                let row = CloudCommandLedgerRow("),
         ref(l, "    private func refuseCapacity(", "    private func removeReserved(")],
        ["元件未在此工具證明 production 接線；未測 bytes、waiters 與 runtime override。",
         "稽核指 production idempotency 仍使用 sender/sequence；R-1 應先定 stable retry identity。"],
        "W5-1 cross-runtime durability owner", constants(l, "globalHardLimit", "normalActorLimit", "fairnessReserveStart", "fairnessActorLimit"))
    row("store-read-health", "readStore 將 absent、ready、corrupt、unsupported-version 與 unreadable 分開；"
        "load 只發布完整 schema-v1，要求 tasks 並拒絕錯型、無法 decode 或重複 identity 的 row。"
        "non-authoritative health 保留既有記憶體但阻止 projection 與 route 使用。",
        [ref(op, "    static func readStore(at url:", "    static func rejectedStore("),
         ref(o, "    static func load(force:", "    /// Atomically replace the registry"),
         ref("Sources/OrchestratorStore.swift", "    static func task(from obj:")],
        ["focused Swift fixture 覆蓋 absent/corrupt/partial/wrong-row/future/directory，但不是 live registry 或 power-loss 觀測。",
         "corrupt whole store 與可 parse 但 invalid restart 子記錄是不同政策。"],
        "W1-5 persistence owner (accepted source boundary)",
        {"read_health_fence_in_load": True, "top_level_version_gate": True})
    row("store-overwrite-risk", "save 以 storeSaveLock 序列化 snapshot/write，輸出 version 1 原子替換，"
        "並拒絕 non-authoritative read health。Readable rejected bytes 保留在 canonical path 且另作"
        "content-addressed quarantine；不存在才是 authoritative empty。",
        [ref(o, "    static func save() -> Bool", "    // MARK: - Cleanup"),
         ref(op, "    static func rejectedStore(", "    static func readSecret(")],
        ["沒有實際 ENOSPC/EROFS、rename/chmod failure、雙 writer 或 power-loss 測試。",
         "Data.atomic 與 quarantine copy 不構成 fsync/power-loss durability receipt。"],
        "W4-2 packaging / operational recovery owner")
    row("disk-failure-seams", "save 的 serializer/write/chmod 失敗回 false；write 後 chmod 失敗"
        "可能已換檔，因此 false 不等於磁碟完全沒變。Root Assignment、startup recovery 與"
        "cleanup 現在 gate effect；storeSaveInterceptorForTesting 可攔截寫入。",
        [ref(o, "    static func save() -> Bool", "    // MARK: - Cleanup"),
         ref(op, "    static func persistRootAssignmentActivation(", "    // MARK: - Installation secrets"),
         ref("Sources/OrchestratorRegistry.swift",
             "        func withdrawRootAssignmentActivation(",
             "        // MARK: Coordination waits"),
         ref("Tests/OrchestratorRecoveryTests.swift", 'group("restart reconciliation is bounded, fail-closed on corruption, and rolls back atomically")')],
        ["EACCES/ENOSPC/EROFS、partial write、rename、chmod、fsync/power-loss 與雙 writer 未實測。",
         "direct dispatch 與一般 terminal briefing lane 的更廣 ordering 仍由 W2-1 接手。"],
        "W2-1 terminal lane / W4-2 packaging owner")
    row("sequence-disk", "CloudSequenceFile 有 unreadable/unwritable typed failure，load 檢查 v=1；"
        "nextSequence 的正常 reserve 分支嘗試先 persist ceiling 再發序號。"
        "來源控制流程顯示 reserved[sender] 先於 try persist() 更新，"
        "persist 失敗不還原 reserved 且 next 未前進；同一物件重試可略過 persist 並發出序號。"
        "若失敗未替換磁碟舊 ceiling，重啟後可能重用已發序號；production composition 持續提供同一 sequenceFile。",
        [ref("Sources/CloudBridgeLifecycle.swift", "    func nextSequence(sender:", "    /// The ceiling currently promised on disk."),
         ref("Sources/CloudBridgeLifecycle.swift", "    private func loadIfNeeded() throws {", "    private func persist() throws {"),
         ref("Sources/CloudBridgeLifecycle.swift", "    private func persist() throws {", "/// The transport's view of this Mac's keys."),
         ref("Sources/CloudBridgeLifecycle.swift", "            sequencing: { _ in sequenceFile },"),
         ref(b, "        let sequence = try await sequencing.nextSequence(sender: identity.deviceID)")],
        ["上述是來源推導風險；實際序號重用或資料損失未觀測，未執行 Swift failure injection。",
         "寫入失敗後重試、斷電、備份還原與同 identity 雙 writer 的實測及 production 修復由 W5-1 接手。",
         "不得把 registry、sequence、ledger 合成同一錯誤政策。"],
        "W5-1 durability owner / security reviewer")
    row("restart-contract", "restart receipt 以 instance 與 total/per-channel drain 決定 ready；"
        "spawning 阻擋，queued 依 secret 是否可恢復判斷，briefed 本身不阻擋。"
        "reconcile 要新且完整 inventory；超過 grace 可帶 unresolved IDs 完成，不能讀成全部已觀察。",
        [ref(w, "    struct RestartReceipt:", "    static let appInstanceID"),
         ref(w, "    static func restartBlockers(", "    static func restartTransition("),
         ref(c, "    static func reconcileRestartInventory(", "    static func restartBlockerRecord("),
         ref("Tests/OrchestratorRecoveryTests.swift", 'group("restart maintenance transition is durable and idempotent")')],
        ["existing test source 不是已執行 receipt；未重啟此 Mac，daemon restart time/p95 與 VM reboot 都未知。",
         "R-1/R-2 與 W4 disposable Ubuntu 須測 admission/effect/write/ACK 各故障點；terminal service 和 daemon 分別測。"],
        "W4 Linux runtime owner / W1-5 persistence owner")
    row("restart-liveness", "可選 probe 僅 GET loopback /v1/health，記錄同一 instance/build/protocol"
        "與單次 request 耗時；它不重啟、不讀 store、不證明 recovery 或 Cloud acceptance。",
        [ref(h, "    static func restartHelloPayload()", "    private func openStream(on")],
        ["未量測 restart/reconcile time、terminal preservation 或 storage readiness；health 不回 schema/read-health。",
         "installed build 與來源 checkout 的 commit 關係未驗證。"],
        "W4 Linux runtime owner / root finalizer")
    row("filesystem-fixture", "可選 probe 在指定 scratch 的私人目錄測 byte round-trip、"
        "corrupt JSON、missing read、ENOTDIR write 與 replace failure；這是 Python/host filesystem fixture，"
        "不是 Swift Orchestrator.load/save。",
        [ref(o, "        if let intercepted = storeSaveInterceptorForTesting?(data) { return intercepted }")],
        ["真實 store 的 disk-full、permission failure、atomic rename 與 power-loss durability 仍未知。"],
        "W4-2 packaging / operational recovery owner")
    require(len(rows) == 19 and len({r["id"] for r in rows}) == 19,
            "zero_observations", "characterization row set is incomplete")
    return rows


def strict_json(data):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "json_parse_failed", "duplicate JSON key")
            result[key] = value
        return result

    def invalid_constant(_):
        raise MeasurementError("json_parse_failed", "non-finite JSON number")

    try:
        return json.loads(data, object_pairs_hook=pairs, parse_constant=invalid_constant)
    except (ValueError, UnicodeError) as error:
        raise MeasurementError("json_parse_failed", "invalid JSON") from error


def validate_observations(observations):
    require(bool(observations), "zero_observations", "a requested probe returned no observations")


def read_health_body(response):
    """Read a complete supported HTTP body without an unbounded allocation.

    HTTPResponse.read(amt) tolerates short Content-Length reads; its chunk reader
    also tolerates EOF before the trailer terminator. Validate both explicitly.
    Only a single Content-Length, a single chunked transfer coding, or EOF framing
    is supported. Chunk metadata shares the existing small probe bound separately
    from decoded body bytes; this is not a new production capacity policy.
    """
    # The email header parser can record a defect and hide later framing fields.
    # Reject that parse before absence is mistaken for valid EOF framing.
    require(not response.headers.defects, "health_framing_invalid", "invalid health response headers")
    lengths = response.headers.get_all("Content-Length", [])
    transfers = response.headers.get_all("Transfer-Encoding", [])
    require(not (lengths and transfers), "health_framing_invalid", "conflicting health framing")

    def bounded_size(token, base, remaining):
        # Compare before int(): even a huge declared length must be a typed error.
        normalized = token.lower().lstrip("0") or "0"
        ceiling = str(remaining) if base == 10 else format(remaining, "x")
        require(len(normalized) < len(ceiling)
                or (len(normalized) == len(ceiling) and normalized <= ceiling),
                "health_payload_invalid", "declared health body exceeds probe cap")
        return int(normalized, base)

    if lengths:
        require(len(lengths) == 1 and re.fullmatch(r"[0-9]+", lengths[0].strip()),
                "health_framing_invalid", "invalid or repeated Content-Length")
        size = bounded_size(lengths[0].strip(), 10, HEALTH_BODY_LIMIT)
        body = response.read(size)
        require(len(body) == size, "health_response_incomplete", "premature EOF in Content-Length body")
        return body
    if not transfers:
        # With no framing headers, a bounded buffered read returns short only at
        # EOF; cap+1 distinguishes a complete cap-sized body from an oversized one.
        return response.read(HEALTH_BODY_LIMIT + 1)

    require(len(transfers) == 1 and transfers[0].strip().lower() == "chunked",
            "health_framing_invalid", "unsupported or repeated Transfer-Encoding")
    reader = response.fp
    metadata_bytes = 0

    def framing_line():
        nonlocal metadata_bytes
        line = reader.readline(HEALTH_BODY_LIMIT - metadata_bytes + 1)
        metadata_bytes += len(line)
        require(metadata_bytes <= HEALTH_BODY_LIMIT, "health_framing_invalid", "chunk metadata exceeds probe cap")
        require(line.endswith(b"\n"), "health_response_incomplete", "premature EOF in chunk framing")
        require(line.endswith(b"\r\n"), "health_framing_invalid", "chunk framing requires CRLF")
        return line

    body = bytearray()
    while True:
        size_line = framing_line()
        match = re.fullmatch(rb"([0-9a-fA-F]+)(?:;[^\r\n]*)?\r\n", size_line)
        require(match is not None, "health_framing_invalid", "invalid chunk size")
        size = bounded_size(match[1].decode("ascii"), 16, HEALTH_BODY_LIMIT - len(body))
        if size == 0:
            while True:
                trailer = framing_line()
                if trailer == b"\r\n":
                    return bytes(body)
                require(re.fullmatch(rb"[!#$%&'*+.^_`|~0-9a-zA-Z-]+:[^\r\n]*\r\n", trailer),
                        "health_framing_invalid", "invalid chunk trailer")
        data = reader.read(size)
        require(len(data) == size, "health_response_incomplete", "premature EOF in chunk data")
        ending = reader.read(2)
        require(len(ending) == 2, "health_response_incomplete", "premature EOF after chunk data")
        require(ending == b"\r\n", "health_framing_invalid", "chunk data requires CRLF")
        metadata_bytes += len(ending)
        require(metadata_bytes <= HEALTH_BODY_LIMIT, "health_framing_invalid", "chunk metadata exceeds probe cap")
        body.extend(data)


def health_probe(port, samples):
    require(1 <= port <= 65535 and 1 <= samples <= 20, "invalid_probe", "port/samples out of bounds")
    observations = []
    for _ in range(samples):
        connection = http.client.HTTPConnection("127.0.0.1", port, timeout=2)
        response = None
        at = datetime.now(timezone.utc).isoformat()
        started = time.monotonic_ns()
        try:
            connection.request("GET", "/v1/health", headers={"Connection": "close"})
            response = connection.getresponse()
            require(response.status == 200, "health_http_failed", "health did not return 200")
            body = read_health_body(response)
            require(0 < len(body) <= HEALTH_BODY_LIMIT, "health_payload_invalid", "empty or oversized health response")
            obj = strict_json(body)
            require(type(obj) is dict and obj.get("ok") is True, "health_payload_invalid", "health is not ok")
            require(type(obj.get("build")) is int and obj["build"] > 0
                    and type(obj.get("protocol")) is int and obj["protocol"] > 0
                    and type(obj.get("version")) is str
                    and re.fullmatch(r"[A-Za-z0-9.+_-]{1,80}", obj["version"]),
                    "health_identity_invalid", "missing/invalid build, protocol or version")
            try:
                instance = str(uuid.UUID(obj["instance"]))
            except (KeyError, ValueError, TypeError, AttributeError) as error:
                raise MeasurementError("health_identity_invalid", "missing/invalid instance") from error
            observations.append({"at": at, "duration_ms": round((time.monotonic_ns() - started) / 1e6, 3),
                                 "response_bytes": len(body), "instance": instance, "build": obj["build"],
                                 "version": obj["version"], "protocol": obj["protocol"]})
        except (OSError, http.client.HTTPException) as error:
            raise MeasurementError("health_unavailable", type(error).__name__) from error
        finally:
            if response is not None:
                response.close()
            connection.close()
    validate_observations(observations)
    identities = {(r["instance"], r["build"], r["protocol"], r["version"]) for r in observations}
    require(len(identities) == 1, "health_identity_changed", "samples do not describe one process/build")
    return {"status": "observed", "observation_count": len(observations), "observations": observations,
            "endpoint": f"http://127.0.0.1:{port}/v1/health", "source_build_relation": "unverified",
            "scope": "sequential liveness requests only; not restart, capacity, storage or Cloud acceptance"}


def filesystem_probe(scratch):
    require(scratch.is_dir(), "scratch_unavailable", "explicit scratch directory must already exist")
    observations = []
    with tempfile.TemporaryDirectory(prefix="w0c-fs-", dir=scratch) as name:
        work = Path(name)
        original = work / "original.json"
        before = b'{"version":1,"tasks":[{"fixture":"nonempty"}]}'
        original.write_bytes(before)
        require(original.read_bytes() == before, "fixture_failed", "round-trip mismatch")
        observations.append({"case": "nonempty-roundtrip", "bytes": len(before), "preserved": True})
        bad = work / "corrupt.json"
        bad.write_bytes(b'{"version":')
        try:
            strict_json(bad.read_bytes())
        except MeasurementError as error:
            require(error.code == "json_parse_failed", "fixture_failed", "wrong parse failure")
            observations.append({"case": "corrupt-json", "error": error.code,
                                 "preserved": bad.read_bytes() == b'{"version":'})
        else:
            raise MeasurementError("fixture_failed", "corrupt JSON was accepted")
        operations = [("missing-read", lambda: (work / "missing").read_bytes(), errno.ENOENT),
                      ("parent-is-file-write", lambda: (original / "child").write_bytes(b"x"), errno.ENOTDIR),
                      ("parent-is-file-replace", lambda: os.replace(bad, original / "child"), errno.ENOTDIR)]
        for case, operation, expected in operations:
            try:
                operation()
            except OSError as error:
                require(error.errno == expected, "fixture_failed", case + ": unexpected errno")
                preserved = original.read_bytes() == before and bad.read_bytes() == b'{"version":'
                require(preserved, "fixture_failed", case + ": fixture changed")
                observations.append({"case": case, "error": errno.errorcode[error.errno], "preserved": True})
            else:
                raise MeasurementError("fixture_failed", case + ": expected failure did not occur")
    validate_observations(observations)
    return {"status": "observed", "observation_count": len(observations), "observations": observations,
            "scope": "isolated Python/host filesystem fixture; no Swift store behavior exercised"}


def make_report(root, health_port=None, samples=3, scratch=None):
    source = SourceEvidence(root)
    rows = characterize(source)
    by_id = {row["id"]: row for row in rows}
    if health_port is not None:
        by_id["restart-liveness"]["measured-runtime"] = health_probe(health_port, samples)
    if scratch is not None:
        by_id["filesystem-fixture"]["executable-test-derived"] = filesystem_probe(scratch)
    report = {"schema": 1, "canonical_program": "CLA-296", "question": "W0-C reliability characterization",
              "scope": "reviewed source baseline plus explicitly requested safe probes; no production policy change",
              "source_reference_commit": BASE_COMMIT, "source_manifest": source.manifest,
              "measurement_tool_sha256": digest(Path(__file__).read_bytes()),
              "source_scope_sha256": digest(canonical(source.manifest).encode()),
              "source_file_count": len(source.manifest), "row_count": len(rows), "rows": rows,
              "approved_new_budgets": None, "production_readiness": "not-established",
              "swift_tests_executed": False,
              "provenance_rule": "test source is source-derived, not an executed test receipt; null is unknown, not zero"}
    if health_port is not None or scratch is not None:
        report["probe_environment"] = {"os": platform.system(), "release": platform.release(),
                                       "machine": platform.machine(), "python": platform.python_version()}
    return report


def markdown(report):
    lines = ["# 平台可靠性基準（W0-C）", "", "此文件由 `tools/measure-platform-reliability.py` 產生；CLA-296 Plan v4 的量測輸入。",
             "來源引用提交：`" + report["source_reference_commit"] + "`。",
             "來源範圍 SHA-256：`" + report["source_scope_sha256"] + "`。",
             "工具 SHA-256：`" + report["measurement_tool_sha256"] + "`。", "",
             f"{report['source_file_count']} 個非空來源檔、{report['row_count']} 列。這是範圍摘要，並非整棵 commit-tree 驗證。",
             "整檔 seal 拒絕來源漂移；本工具不解析或編譯 Swift。更新 seal 前須重讀受影響行為。",
             "未提供 probe 時保留 unknown；要求 probe 卻無資料、輸入缺失或 parse 失敗則非零退出。",
             "existing test source 一律屬 source-derived；本工具未執行 Swift tests。數值是現行常數或該次觀測，沒有批准新預算。", "",
             "重現靜態文件：`python3 tools/measure-platform-reliability.py --format markdown`。",
             "核對文件：`python3 tools/measure-platform-reliability.py --check docs/generated/platform-reliability-baseline.md`。",
             "聚焦測試：`node Tests/platform-reliability-characterization.mjs`。", "",
             "選用 probe：`python3 tools/measure-platform-reliability.py --health-port 7717 --samples 3 --scratch <existing-private-directory> --format json`。",
             "只讀 unauthenticated loopback health；不開 SSE、不做壓測、不讀真實 registry、不重啟。scratch fixture 會自行清除。",
             "health 支援單一 Content-Length、chunked（含 extensions/trailers）或無長度的 close-delimited 回應；"
             "須收齊 framing 才記錄觀測。body 上限 16 KiB，chunk metadata 另以相同上限約束；皆為量測工具防護，非 production budget。",
             "宣告或實收 body 超限為 health_payload_invalid；提前 EOF 為 health_response_incomplete；"
             "framing 衝突、不支援或 metadata 超限為 health_framing_invalid。失敗 exit 2，stdout 為空。", ""]
    for row in report["rows"]:
        lines += ["## " + row["id"], "", row["finding"], "", "- **source-derived**："]
        for ref in row["source-derived"]:
            lines.append(f"  - `{ref['path']}:{ref['line']}–{ref['end_line']}`；slice SHA-256 `{ref['sha256']}`。")
        if row["values"]:
            values = {key: value["value"] if isinstance(value, dict) and "value" in value else value
                      for key, value in row["values"].items()}
            lines.append("- **來源數值／屬性**：`" + canonical(values) + "`。")
        for kind in ("executable-test-derived", "measured-runtime"):
            observation = row[kind]
            lines.append("- **" + kind + "**：`" + canonical(observation) + "`。")
        lines += ["- **unknown**：" + " ".join(row["unknown"]), "- **接手 owner**：" + row["downstream_owner"] + "。", ""]
    lines += ["## 預算批准所需的下一批輸入", "",
              "| 後續節點 | 必須取得的觀測與決策 |", "|---|---|",
              "| R-1 | ingress/admission/accepted/executing/publication/ACK 各段 count、bytes、wait/work；burst + suspended consumer；穩定 retry identity、durable re-read、typed overflow 與無 silent command loss。 |",
              "| R-2 | connection/aggregate body/stream/outstanding-byte ceiling；慢 consumer 與 duplicate retry 下，同時追蹤 health、其他 lane、completion/error、eviction、snapshot coalescing 及 reconnect realign。 |",
              "| W1-5 store-health | fresh/existing state × missing/EACCES/corrupt JSON/wrong row shape/unsupported version；load→save 原檔保留；ENOSPC/EROFS/rename/chmod/partial-write；各 effect 的 persist-before-effect/send 與 typed read-health。 |",
              "| W4/W6 | disposable Ubuntu 的 daemon/terminal service restart 分離、VM reboot、非空 restore、同 process/boot identity、queue drain 與 reconciliation 耗時分布。 |", "",
              "Plan v4 的 restart 60s p95、reboot 120s、RPO/RTO 是提案目標；本基準沒有實測或批准它們。",
              "沒有新增全管線 count/byte budget：需 reference workload、樣本數、duration、環境、拒絕／丟棄與 peak debt，再由具名 owner/approver 決定。",
              "health liveness、fixture 通過、source seal 相符，都不構成 restart、durability、Ubuntu 或 Cloud 發布驗收。", "",
              "## 來源檔案 seal", "", "| 檔案 | bytes | SHA-256 |", "|---|---:|---|"]
    lines.extend(f"| `{r['path']}` | {r['bytes']} | `{r['sha256']}` |" for r in report["source_manifest"])
    return "\n".join(lines) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--format", choices=("json", "markdown"), default="json")
    parser.add_argument("--check", type=Path, help="compare the deterministic source-only Markdown; never write")
    parser.add_argument("--health-port", type=int, help="explicit opt-in: GET 127.0.0.1:<port>/v1/health only")
    parser.add_argument("--samples", type=int, default=3, help="1..20 sequential health requests; default 3")
    parser.add_argument("--scratch", type=Path, help="explicit opt-in: isolated filesystem fixtures here")
    args = parser.parse_args()
    try:
        require(args.health_port is not None or args.samples == 3, "invalid_probe", "samples require --health-port")
        require(args.check is None or (args.health_port is None and args.scratch is None),
                "invalid_probe", "--check compares source-only evidence")
        report = make_report(args.root, args.health_port, args.samples, args.scratch)
        if args.check is not None:
            require(args.check.read_text(encoding="utf-8") == markdown(report),
                    "baseline_stale", "generated Markdown differs")
            print(canonical({"status": "pass", "row_count": report["row_count"],
                             "source_file_count": report["source_file_count"]}))
        else:
            print(markdown(report) if args.format == "markdown" else json.dumps(report, ensure_ascii=False, indent=2), end="\n" if args.format == "json" else "")
        return 0
    except (MeasurementError, OSError, UnicodeError) as error:
        print(canonical({"status": "error", "code": getattr(error, "code", "input_unreadable"),
                         "detail": str(error)}), file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
