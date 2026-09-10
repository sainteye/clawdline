#!/usr/bin/env node
// Preview-first operator repair. Uses existing root-attested record_evidence authority only.
import fs from "node:fs";
import path from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { pathToFileURL } from "node:url";

const fail = code => { throw new Error(code); };
const hash = value => createHash("sha256").update(JSON.stringify(value)).digest("hex");
const keys = (value, expected) => value && !Array.isArray(value)
    && JSON.stringify(Object.keys(value).sort()) === JSON.stringify([...expected].sort());
const text = (value, max) => typeof value === "string" && value.trim().length > 0
    && Buffer.byteLength(value) <= max && !/[\x00-\x1f\x7f]/.test(value);
function manifestValue(m) {
    if (!keys(m, ["version", "itemId", "projectId", "scopeRevision", "evidenceId", "subject", "criteria"])
        || m.version !== 1 || !Number.isSafeInteger(m.scopeRevision) || m.scopeRevision < 0
        || ![m.itemId, m.projectId, m.evidenceId].every(x => text(x, 200))
        || !text(m.subject, 500) || !Array.isArray(m.criteria)
        || m.criteria.length < 1 || m.criteria.length > 32
        || m.criteria.some(c => !keys(c, ["checklistId", "reason"])
            || !text(c.checklistId, 200) || !text(c.reason, 500))
        || new Set(m.criteria.map(c => c.checklistId)).size !== m.criteria.length) fail("repair_manifest_invalid");
    return { version: 1, itemId: m.itemId, projectId: m.projectId, scopeRevision: m.scopeRevision,
        evidenceId: m.evidenceId, subject: m.subject,
        criteria: m.criteria.map(c => ({ checklistId: c.checklistId, reason: c.reason.trim() }))
            .sort((a, b) => a.checklistId.localeCompare(b.checklistId)) };
}
function proofFields(m, criterion, digest) {
    return { operation: "record_evidence", itemId: m.itemId, kind: "verification", status: "passed",
        sourceId: "criterion-repair:" + hash([digest, criterion.checklistId]), subject: m.subject,
        checklistId: criterion.checklistId,
        summary: `Applicability attestation reusing retained verification ${m.evidenceId}; no new test execution or landing. ${criterion.reason}` };
}
export function planCriterionRepair(manifest, board) {
    const m = manifestValue(manifest), digest = hash(m), item = board?.item;
    if (board?.enabled !== true || board.available !== true || board.readState?.status !== "ready"
        || !Number.isSafeInteger(board.revision) || board.revision < 0
        || board.readState.revision !== board.revision) fail("repair_read_unavailable");
    if (!item || item.id !== m.itemId || item.projectId !== m.projectId
        || item.scopeRevision !== m.scopeRevision || item.currentEvidence?.scopeRevision !== m.scopeRevision
        || item.currentEvidence.subject !== m.subject) fail("repair_identity_changed");
    if (item.projection?.evidence?.omittedCount !== 0 || item.projection?.checklist?.omittedCount !== 0
        || !Array.isArray(item.verifications) || !Array.isArray(item.checklist)) fail("repair_incomplete_snapshot");
    const sources = item.verifications.filter(e => e.id === m.evidenceId);
    if (sources.length !== 1 || sources[0].kind !== "verification" || sources[0].status !== "passed"
        || sources[0].subject !== m.subject || sources[0].scopeRevision !== m.scopeRevision) fail("repair_proof_mismatch");
    const rows = m.criteria.map(c => {
        const checks = item.checklist.filter(x => x.id === c.checklistId);
        if (checks.length !== 1 || checks[0].status !== "passed") fail("repair_criterion_mismatch");
        const expected = proofFields(m, c, digest);
        const applied = item.verifications.filter(e => e.sourceId === expected.sourceId);
        if (applied.length > 1) fail("repair_ambiguous_proof");
        let state = "pending";
        if (applied.length === 1) {
            const e = applied[0];
            if (e.status !== "passed" || e.kind !== "verification" || e.subject !== m.subject
                || e.scopeRevision !== m.scopeRevision || e.checklistId !== c.checklistId
                || e.summary !== expected.summary || checks[0].evidenceId !== e.id) fail("repair_applied_mismatch");
            state = "observed";
        } else if (checks[0].evidenceId != null) fail("repair_existing_criterion_proof");
        return { checklistId: c.checklistId, state, sourceId: expected.sourceId, reason: c.reason };
    });
    return { version: 1, digest, itemId: m.itemId, scopeRevision: m.scopeRevision,
        evidenceId: m.evidenceId, subject: m.subject, rows };
}

export async function repairCriteria(manifest, io, applyDigest) {
    const m = manifestValue(manifest);
    let board = await io.read(), plan = planCriterionRepair(m, board);
    if (applyDigest === undefined) return plan;
    if (applyDigest !== plan.digest) fail("repair_preview_mismatch");
    let state = await io.loadState();
    if (state === null) state = { version: 1, digest: plan.digest, pending: null };
    if (!keys(state, ["version", "digest", "pending"]) || state.version !== 1
        || state.digest !== plan.digest) fail("repair_checkpoint_mismatch");
    const save = async () => { try { await io.saveState(state); } catch { fail("repair_checkpoint_unavailable"); } };
    const validatePending = () => {
        if (state.pending === null) return;
        const p = state.pending, c = m.criteria.find(x => x.checklistId === p?.checklistId);
        if (!c || !Number.isSafeInteger(p.expectedRevision) || p.expectedRevision < 0
            || typeof p.requestId !== "string" || !/^[a-f0-9-]{36}$/.test(p.requestId)) fail("repair_checkpoint_mismatch");
        const expected = { ...proofFields(m, c, plan.digest), requestId: p.requestId, expectedRevision: p.expectedRevision };
        if (!keys(p, Object.keys(expected)) || Object.keys(expected).some(k => p[k] !== expected[k])) fail("repair_checkpoint_mismatch");
    };
    validatePending();
    for (let index = 0; index <= m.criteria.length; index++) {
        // No cached preview authorizes a later write. Recheck every identity and scope first.
        board = await io.read(); plan = planCriterionRepair(m, board);
        if (state.pending && plan.rows.find(r => r.checklistId === state.pending.checklistId)?.state === "observed") {
            state.pending = null; await save();
        }
        const row = state.pending ? plan.rows.find(r => r.checklistId === state.pending.checklistId)
            : plan.rows.find(r => r.state === "pending");
        if (!row) return plan;
        if (index === m.criteria.length) fail("repair_observation_pending");
        if (!state.pending) {
            const criterion = m.criteria.find(c => c.checklistId === row.checklistId);
            state.pending = { ...proofFields(m, criterion, plan.digest), requestId: randomUUID(),
                expectedRevision: board.revision };
            await save(); // No request exists on the network before its exact body is durable.
        }
        let response;
        try { response = await io.write(state.pending); } catch { fail("repair_transport_unknown"); }
        if (response.status >= 200 && response.status < 300) continue;
        if (response.status >= 400 && response.status < 500 && !response.body?.commandApplied) {
            const code = response.body?.error?.code;
            state.pending = null; await save();
            fail("repair_refused_" + (/^[a-z0-9_]{1,80}$/.test(code ?? "") ? code : "request"));
        }
        fail("repair_transport_unknown");
    }
    fail("repair_observation_pending");
}

function readJSON(file, optional = false) {
    try {
        const stat = fs.lstatSync(file);
        if (!stat.isFile() || stat.size > 65536) fail("repair_file_invalid");
        return JSON.parse(fs.readFileSync(file, "utf8"));
    } catch (error) { if (optional && error.code === "ENOENT") return null; throw error; }
}
async function main(args) {
    if (!(args.length === 1 || (args.length === 5 && args[1] === "--apply" && args[3] === "--state"))) fail("repair_usage");
    const manifest = manifestValue(readJSON(args[0])), apply = args[2], statePath = args[4];
    const port = process.env.CLAWDLINE_PORT ?? "7717";
    if (!/^[0-9]{1,5}$/.test(port) || Number(port) < 1 || Number(port) > 65535) fail("repair_port_invalid");
    const tokenFile = process.env.CLAWDLINE_ORCHESTRATOR_TOKEN_FILE
        ?? path.join(process.env.HOME, ".config/clawdline/orchestrator-token");
    const tokenFD = fs.openSync(tokenFile, "r"), buffer = Buffer.alloc(65);
    let bytes;
    try { bytes = buffer.subarray(0, fs.readSync(tokenFD, buffer, 0, 65, 0)); }
    finally { fs.closeSync(tokenFD); }
    if (bytes.length > 64 || !/^(?:[a-f0-9]{64}|[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048])$/.test(bytes.toString("utf8"))) fail("repair_credential_unavailable");
    const request = async body => {
        const response = await fetch("http://127.0.0.1:" + port + "/v1/board"
            + (body ? "" : "?item=" + encodeURIComponent(manifest.itemId)), {
            method: body ? "POST" : "GET", redirect: "error", signal: AbortSignal.timeout(15000),
            headers: { "X-Clawdline-Orchestrator": bytes.toString("utf8"), "Content-Type": "application/json" },
            ...(body ? { body: JSON.stringify(body) } : {}) });
        return { status: response.status, body: await response.json() };
    };
    let lockFD;
    try {
        if (apply !== undefined) {
            if (path.resolve(statePath) === path.resolve(args[0])) fail("repair_state_is_manifest");
            try { lockFD = fs.openSync(statePath + ".lock", "wx", 0o600); }
            catch { fail("repair_checkpoint_busy"); }
        }
        const plan = await repairCriteria(manifest, {
            read: async () => { const r = await request(); if (r.status !== 200) fail("repair_read_refused"); return r.body.board; },
            write: request,
            loadState: async () => readJSON(statePath, true),
            saveState: async state => {
                const temporary = statePath + ".tmp-" + randomUUID();
                let fd;
                try {
                    fd = fs.openSync(temporary, "wx", 0o600);
                    fs.writeFileSync(fd, JSON.stringify(state)); fs.fsyncSync(fd); fs.closeSync(fd); fd = undefined;
                    fs.renameSync(temporary, statePath);
                    const directory = fs.openSync(path.dirname(statePath), "r");
                    try { fs.fsyncSync(directory); } finally { fs.closeSync(directory); }
                } finally { if (fd !== undefined) fs.closeSync(fd); if (fs.existsSync(temporary)) fs.unlinkSync(temporary); }
            }
        }, apply);
        process.stdout.write(JSON.stringify(plan, null, 2) + "\n");
    } finally {
        if (lockFD !== undefined) { fs.closeSync(lockFD); fs.unlinkSync(statePath + ".lock"); }
    }
}
if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
    main(process.argv.slice(2)).catch(error => {
        process.stderr.write((/^repair_[a-z0-9_]+$/.test(error.message) ? error.message : "repair_io_unavailable") + "\n");
        process.exitCode = 1;
    });
}
