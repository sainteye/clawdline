#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

let checks = 0;
let failed = 0;

function check(label, condition) {
    checks += 1;
    if (condition) {
        console.log(`  ✓ ${label}`);
    } else {
        failed += 1;
        console.error(`  ✗ ${label}`);
    }
}

function functionBody(source, signature) {
    const start = source.indexOf(signature);
    if (start < 0) return null;
    const open = source.indexOf("{", start + signature.length);
    if (open < 0) return null;
    let depth = 0;
    for (let i = open; i < source.length; i += 1) {
        if (source[i] === "{") depth += 1;
        if (source[i] === "}") {
            depth -= 1;
            if (depth === 0) return source.slice(open + 1, i);
        }
    }
    return null;
}

const source = readFileSync(resolve(process.env.CLAWDLINE_CODEX_NAMING_SOURCE
                                    || "Sources/CodexNaming.swift"), "utf8");
const body = functionBody(source, "private func considerCodex(_ target: TargetSession)");

console.log("Codex naming reads only after admission");
check("considerCodex still exists as the production boundary", body !== null);

if (body !== null) {
    const reserve = body.indexOf("guard reserve(key)");
    const readThread = body.indexOf("server.thread(id: head.id)");
    const readOpening = body.indexOf("Codex.firstUserMessage(of: record.url)");
    const nativeBranch = functionBody(
        body, "if let title = Self.threadName(in: before)");
    const emptyOpening = functionBody(
        body, "guard let request = Codex.firstUserMessage(of: record.url)");
    check("a finished or backed-off thread is refused before app-server work",
          reserve >= 0 && readThread >= 0 && reserve < readThread);
    check("a finished or backed-off thread is refused before its 2 MB opening read",
          reserve >= 0 && readOpening >= 0 && reserve < readOpening);
    check("an existing native title is read before the opening is considered",
          readThread >= 0 && readOpening >= 0 && readThread < readOpening);
    check("an existing native title returns without reading the opening",
          nativeBranch !== null && /\breturn\b/.test(nativeBranch));
    check("a rollout discovered before its first request gets a short startup retry",
          emptyOpening !== null && /retryDelay\s*=\s*2/.test(emptyOpening));
}

console.log(`${checks - failed}/${checks} codex naming order checks passed`);
if (failed > 0) process.exit(1);
