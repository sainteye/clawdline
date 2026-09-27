#!/usr/bin/env node
// Network audit for round 3 (see EXPERIMENT.md, "Tools and isolation"): lists every Bash command
// in the transcripts that names a non-localhost URL or host, installs packages, or could download
// (npx, npm/pip/go install or get, git clone, curl/wget to a remote host).
//   node audit-network.mjs [transcripts dir ...]      (default: transcripts pilot/*)
import fs from 'node:fs';
import path from 'node:path';

const HERE = path.dirname(new URL(import.meta.url).pathname);
process.chdir(HERE);

const dirs = process.argv.slice(2);
const files = [];
if (!dirs.length) {
  if (fs.existsSync('transcripts')) for (const f of fs.readdirSync('transcripts')) files.push(path.join('transcripts', f));
  if (fs.existsSync('pilot')) for (const d of fs.readdirSync('pilot')) files.push(path.join('pilot', d, 'transcript.jsonl'));
} else for (const d of dirs) for (const f of fs.readdirSync(d)) files.push(path.join(d, f));

const LOCAL = /^(localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(:\d+)?$/;
const risky = [/\bnpx\b/, /\bnpm (i|install|add|ci)\b/, /\b(pip3?|pipx) install\b/, /\bgo (install|get)\b/, /\bgit clone\b/, /\bwget\b/, /\bbrew install\b/, /playwright install/];

let total = 0, flagged = 0;
for (const f of files.filter((x) => x.endsWith('.jsonl') && fs.existsSync(x))) {
  for (const line of fs.readFileSync(f, 'utf8').split('\n').filter(Boolean)) {
    const e = JSON.parse(line);
    if (e.type !== 'assistant') continue;
    for (const c of e.content) {
      if (c.type !== 'tool_use' || c.name !== 'Bash') continue;
      total++;
      const cmd = c.input.command;
      const hosts = [...cmd.matchAll(/https?:\/\/([^/\s'"`)]+)/g)].map((m) => m[1]).filter((h) => !LOCAL.test(h));
      const hits = risky.filter((re) => re.test(cmd)).map(String);
      if (hosts.length || hits.length) {
        flagged++;
        console.log(`${f}: hosts=${JSON.stringify(hosts)} patterns=${JSON.stringify(hits)}\n  ${cmd.slice(0, 300).replace(/\n/g, '\n  ')}`);
      }
    }
  }
}
console.log(`\n${files.length} transcripts, ${total} Bash commands, ${flagged} flagged`);
