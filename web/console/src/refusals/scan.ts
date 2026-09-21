// The guard for a refusal that arrives with a name and leaves without one.
//
// Four times in one day this console was found doing the same thing: the
// machine refused with a code — `not_a_repo`, `rate_limited`, `write_disabled`,
// `cloud_not_carried` — and the screen replaced it with one sentence that has
// no subject. "無法讀取 Git 變更." "請求失敗." The person is told that something
// did not work and nothing about which thing, so they cannot act and cannot
// report it either; `docs/cloud-error-transparency.md` §5 already says the
// words are chosen by the code and that the `code · ref` pair goes after them,
// and `core/failure-text.js` already implements exactly that. The shape is not
// a missing decision, it is a decision nothing enforces.
//
// So this file enforces it. It parses the console's own TypeScript — not the
// copied Swift modules under `legacy/js/`, which are byte for byte theirs
// (`tools/check-legacy-css.sh`) and can only be corrected at their call sites —
// and reports three ways a named refusal stops being named:
//
//   `fixed_catch_all`  a chain that branches on `.code`/`.reason` and ends in a
//                      sentence, instead of ending in the one formatter or in
//                      "" for a caller to finish.
//   `refusal_dropped`  a rejection handler that says words to a person without
//                      ever looking at the refusal it was handed.
//   `reason_dropped`   a `Promise.allSettled` result whose `.status` is read and
//                      whose `.reason` is read nowhere, so the refused half of
//                      the screen draws as an empty one.
//
// A site that is deliberate says so on its own line — `// refusal-ok: <why>` —
// and the why has to be a sentence, not a word.
//
// **It reports "cannot tell" as a failure.** A scan that parsed nothing, found
// no chains at all, or cannot still catch the shape it was written for is
// `indeterminate`, which is not green. `scan.test.ts` feeds it `GitPanel.tsx`
// as that file read before `gitSentence()` existed and requires a violation
// back; `selfCheck()` runs the same fixture so the command-line guard cannot
// pass by having quietly stopped working.
//
// This command also coordinates `audit.ts`, whose source contracts cross the
// TypeScript/HTML/Go boundary. Keeping the two detectors behind one report is
// deliberate: the AST pass finds new local spellings, while the cross-language
// pass can prove that a reason was already lost before this parser could see it.
//
// Nothing here is imported by the console at run time, so `node --test` loads
// it as it is:  `node --test web/console/src/refusals/scan.test.ts`
// and, for the list:  `node web/console/src/refusals/scan.ts`
import * as ts from "typescript"
import { readdirSync, readFileSync, statSync } from "node:fs"
import { dirname, join, relative, resolve, sep } from "node:path"
import { fileURLToPath } from "node:url"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { scanAudits, selfCheckAudits, type AuditReport } from "./audit.ts"

/** One place where a named refusal is turned into something a person sees. */
export interface Site {
  /** Path from the repository root, with forward slashes. */
  file: string
  /** 1-based line of the decision, as an editor counts. */
  line: number
  kind: "fixed_catch_all" | "refusal_dropped" | "reason_dropped" | "code_unspent" | "uncertainty_dropped"
  /** The codes this site decides on, in source order; empty where it looks at none. */
  codes: string[]
  /** The catch-all as written, cut to one line. */
  terminal: string
  /** The `refusal-ok:` reason, when the site carries one. */
  allowed: string | null
  /** Character span in the file, used to drop a handler that already reports inside. */
  span: [number, number]
}

/** What one scan found. `indeterminate` is a failure and never a pass. */
export interface Report {
  files: number
  chains: number
  sites: Site[]
  violations: Site[]
  allowed: Site[]
  /** Cross-language product issues that the original TypeScript-only pass could not see. */
  audits: AuditReport
  indeterminate: string | null
}

/* ---- what counts as words, and what counts as saying them properly -------- */

/**
 * The one place a refusal becomes words (`core/failure-text.js`), and the two
 * wrappers that are it: `toastFailure` hands it a toast, `sentenceFor` is
 * `voice-bridge.ts`'s local alias. A catch-all that reaches any of these has
 * kept the code; a catch-all that does not has thrown it away.
 */
const FORMATTERS = new Set([
  "failureSentence",
  "describeFailure",
  "renderFailure",
  "toastFailure",
  "sentenceFor",
  "refusalSentence",
])

/** No local wrappers: the default for a call that has not collected a file's own. */
const EMPTY: ReadonlySet<string> = new Set<string>()

/** Word tables. A member of one of these is a sentence for a person. */
const CATALOGS = new Set(["T", "S", "w", "words", "strings", "copy"])

/** Calls that produce a sentence out of the catalogs. */
const WORD_CALLS = new Set(["nextWord", "workWord", "nowWord", "fill", "fillString", "scheduleRunCopy"])

/** A setter whose argument is what the screen will show. */
function isSayingCall(node: ts.Node): boolean {
  if (!ts.isCallExpression(node)) return false
  const name = calleeName(node)
  return /^set[A-Z]/.test(name) || ["say", "said", "toast", "publish", "complain"].includes(name)
}

function calleeName(call: ts.CallExpression): string {
  const target = call.expression
  if (ts.isIdentifier(target)) return target.text
  if (ts.isPropertyAccessExpression(target)) return target.name.text
  return ""
}

/** `T.webGitFailed`, `T().webRequestFailed`, `L.strings.webOffline`, `w.failed`. */
function isCatalogWord(node: ts.Node): boolean {
  if (!ts.isPropertyAccessExpression(node)) return false
  if (!/^[a-z]/.test(node.name.text)) return false
  const owner = node.expression
  if (ts.isIdentifier(owner)) return CATALOGS.has(owner.text)
  if (ts.isCallExpression(owner) && ts.isIdentifier(owner.expression)) return CATALOGS.has(owner.expression.text)
  if (ts.isPropertyAccessExpression(owner)) return CATALOGS.has(owner.name.text)
  return false
}

/** Prose rather than a token: it has a space or a character outside ASCII. */
function isProse(node: ts.Node): boolean {
  if (!ts.isStringLiteral(node) && !ts.isNoSubstitutionTemplateLiteral(node)) return false
  const text = node.text
  return text.length >= 4 && (/\s/.test(text) || /[^\x20-\x7e]/.test(text))
}

/** Whether anything inside `node` is a sentence a person reads. */
function saysWords(node: ts.Node): boolean {
  let found = false
  const walk = (n: ts.Node): void => {
    if (found) return
    if (isCatalogWord(n) || isProse(n)) {
      found = true
      return
    }
    if (ts.isCallExpression(n) && WORD_CALLS.has(calleeName(n))) {
      found = true
      return
    }
    ts.forEachChild(n, walk)
  }
  walk(node)
  return found
}

/**
 * Whether anything inside `node` hands the refusal to the formatter, or passes
 * it on as a refusal rather than as a sentence — a `throw`, or a rejection
 * carrying a code, both of which leave the naming to whoever draws it.
 */
function keepsTheCode(node: ts.Node, local: ReadonlySet<string> = EMPTY): boolean {
  let found = false
  const walk = (n: ts.Node): void => {
    if (found) return
    if (ts.isCallExpression(n) && (FORMATTERS.has(calleeName(n)) || local.has(calleeName(n)))) {
      found = true
      return
    }
    if (ts.isCallExpression(n) && calleeName(n) === "reject") {
      found = true
      return
    }
    if (ts.isThrowStatement(n)) {
      found = true
      return
    }
    ts.forEachChild(n, walk)
  }
  walk(node)
  return found
}

/** `""`, `null`, `undefined`: no words of its own, so the caller still decides. */
function defersUpwards(node: ts.Node): boolean {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text === ""
  if (node.kind === ts.SyntaxKind.NullKeyword) return true
  if (ts.isIdentifier(node) && node.text === "undefined") return true
  if (ts.isReturnStatement(node)) return !node.expression || defersUpwards(node.expression)
  if (ts.isBlock(node)) {
    const kept = node.statements.filter((s) => !ts.isEmptyStatement(s))
    return kept.length === 1 && defersUpwards(kept[0]!)
  }
  return false
}

/* ---- the comparison that makes a site a refusal site ---------------------- */

const NAMED_FIELDS = new Set(["code", "reason"])

/** `x.code === "busy"`, `code === "busy"`, and their `!==` forms. */
function namedCode(node: ts.Node): string | null {
  if (!ts.isBinaryExpression(node)) return null
  const op = node.operatorToken.kind
  const compares =
    op === ts.SyntaxKind.EqualsEqualsEqualsToken ||
    op === ts.SyntaxKind.EqualsEqualsToken ||
    op === ts.SyntaxKind.ExclamationEqualsEqualsToken ||
    op === ts.SyntaxKind.ExclamationEqualsToken
  if (!compares) return null
  const pair: [ts.Expression, ts.Expression][] = [
    [node.left, node.right],
    [node.right, node.left],
  ]
  for (const [side, other] of pair) {
    if (!ts.isStringLiteral(other)) continue
    if (ts.isPropertyAccessExpression(side) && NAMED_FIELDS.has(side.name.text)) return other.text
    if (ts.isIdentifier(side) && NAMED_FIELDS.has(side.text)) return other.text
  }
  return null
}

/** Every code named anywhere inside `node`, in source order and without repeats. */
function codesIn(node: ts.Node): string[] {
  const out: string[] = []
  const walk = (n: ts.Node): void => {
    const code = namedCode(n)
    if (code && !out.includes(code)) out.push(code)
    ts.forEachChild(n, walk)
  }
  walk(node)
  return out
}

/* ---- the chain ------------------------------------------------------------ */

type Chain =
  | { kind: "ternary"; head: ts.ConditionalExpression }
  | { kind: "if"; head: ts.IfStatement }

/** The whole `if`/`else if`/`else` or `? :` ladder this comparison decides one step of. */
function chainOf(comparison: ts.Node): Chain | null {
  let node: ts.Node = comparison
  let found: Chain | null = null
  while (node.parent) {
    const parent = node.parent
    if (ts.isIfStatement(parent) && within(comparison, parent.expression)) {
      found = { kind: "if", head: parent }
      break
    }
    if (ts.isConditionalExpression(parent) && within(comparison, parent.condition)) {
      found = { kind: "ternary", head: parent }
      break
    }
    node = parent
  }
  if (!found) return null
  if (found.kind === "if") {
    let head = found.head
    while (head.parent && ts.isIfStatement(head.parent) && head.parent.elseStatement === head) head = head.parent
    return { kind: "if", head }
  }
  let head = found.head
  while (head.parent && ts.isConditionalExpression(head.parent) && head.parent.whenFalse === head) head = head.parent
  return { kind: "ternary", head }
}

function within(inner: ts.Node, outer: ts.Node): boolean {
  return inner.getStart() >= outer.getStart() && inner.getEnd() <= outer.getEnd()
}

/** Each step's answer — the `then` of every rung — without the final `else`. */
function armsOf(chain: Chain): ts.Node[] {
  const arms: ts.Node[] = []
  if (chain.kind === "ternary") {
    let node: ts.ConditionalExpression | null = chain.head
    while (node) {
      arms.push(node.whenTrue)
      node = ts.isConditionalExpression(node.whenFalse) ? node.whenFalse : null
    }
    return arms
  }
  let node: ts.IfStatement | null = chain.head
  while (node) {
    arms.push(node.thenStatement)
    const next: ts.Statement | undefined = node.elseStatement
    node = next && ts.isIfStatement(next) ? next : null
  }
  return arms
}

/** Whether a statement exits its function rather than falling through. */
function exits(statement: ts.Statement): boolean {
  if (ts.isReturnStatement(statement) || ts.isThrowStatement(statement)) return true
  if (ts.isBlock(statement)) {
    const last = statement.statements[statement.statements.length - 1]
    return !!last && exits(last)
  }
  return false
}

/**
 * `if (code === "…") return …` written one under another is the same ladder as
 * `else if`, and the console writes it both ways. A guard rung is an `if` on a
 * named code with no `else` whose answer leaves; the rung after it is only
 * reached when the code was none of those.
 */
function isGuardRung(statement: ts.Statement): statement is ts.IfStatement {
  if (!ts.isIfStatement(statement) || statement.elseStatement) return false
  if (!codesIn(statement.expression).length) return false
  return exits(statement.thenStatement)
}

/**
 * What happens when no rung matched: the final `else`, or — for a ladder
 * written as guards — the first statement after the last of them. `stepped`
 * names the rungs walked over, so they are not each reported as a ladder of
 * their own.
 */
function catchAllOf(chain: Chain): {
  nodes: ts.Node[]
  kind: "else" | "after" | "implicit"
  stepped: ts.Node[]
} {
  if (chain.kind === "ternary") {
    let node: ts.ConditionalExpression = chain.head
    while (ts.isConditionalExpression(node.whenFalse)) node = node.whenFalse
    return { nodes: [node.whenFalse], kind: "else", stepped: [] }
  }
  let node: ts.IfStatement = chain.head
  while (node.elseStatement && ts.isIfStatement(node.elseStatement)) node = node.elseStatement
  if (node.elseStatement) return { nodes: [node.elseStatement], kind: "else", stepped: [] }
  const block = chain.head.parent
  if (block && (ts.isBlock(block) || ts.isSourceFile(block) || ts.isCaseClause(block))) {
    const statements = (block as ts.Block).statements
    const index = statements.findIndex((s) => s === (chain.head as ts.Node))
    if (index >= 0) {
      let next = index + 1
      const stepped: ts.Node[] = []
      while (next < statements.length && isGuardRung(statements[next]!)) {
        stepped.push(statements[next]!)
        next += 1
      }
      const after = statements[next]
      if (after) return { nodes: [after], kind: "after", stepped }
      return { nodes: [], kind: "implicit", stepped }
    }
  }
  return { nodes: [], kind: "implicit", stepped: [] }
}

/** Whether this ladder is choosing what a person is told. */
function decidesWords(chain: Chain, arms: ts.Node[]): boolean {
  if (arms.some(saysWords)) return true
  // A code collapsed into a private word — `"unknown"` / `"unreadable"` — that
  // is handed straight to a setter is still the screen's sentence, one step
  // removed; `session/Todos.tsx` is the case this clause is here for.
  const literalArms = arms.every((a) => ts.isStringLiteral(a) || ts.isNoSubstitutionTemplateLiteral(a))
  if (!literalArms) return false
  let node: ts.Node = chain.kind === "ternary" ? chain.head : chain.head
  for (let up = 0; up < 4 && node.parent; up += 1) {
    node = node.parent
    if (isSayingCall(node)) return true
  }
  return false
}

/* ---- the two handler shapes ----------------------------------------------- */

/** Whether `name` is mentioned anywhere inside `scope`. */
function mentions(scope: ts.Node, name: string): boolean {
  let found = false
  const walk = (n: ts.Node): void => {
    if (found) return
    if (ts.isIdentifier(n) && n.text === name) {
      found = true
      return
    }
    ts.forEachChild(n, walk)
  }
  ts.forEachChild(scope, walk)
  return found
}

/**
 * Whether this handler has the refusal in its hand: it reads a `code`, a
 * `detail`, or asks whether it is a `RefusalError`. Words chosen after that
 * and without the formatter are words chosen *instead of* the code.
 */
function holdsTheCode(node: ts.Node): boolean {
  let found = false
  const walk = (n: ts.Node): void => {
    if (found) return
    if (ts.isPropertyAccessExpression(n) && (n.name.text === "code" || n.name.text === "detail" || n.name.text === "message")) {
      found = true
      return
    }
    if (ts.isBinaryExpression(n) && n.operatorToken.kind === ts.SyntaxKind.InstanceOfKeyword) {
      if (ts.isIdentifier(n.right) && /Refusal|Failure/.test(n.right.text)) {
        found = true
        return
      }
    }
    ts.forEachChild(n, walk)
  }
  walk(node)
  return found
}

/** A rejection handler, and whether it ever looks at what it was handed. */
interface Handler {
  body: ts.Node
  at: ts.Node
  looked: boolean
}

/**
 * A refusal-free handler that replaces the missing answer with a value a
 * screen can mistake for evidence. Cleanup such as `setBusy(false)` is not a
 * finding; an empty collection/value is, as is a boolean explicitly named as
 * failure state.
 */
function dropsIntoPlaceholder(node: ts.Node): boolean {
  let found = false
  const placeholder = (value: ts.Expression): boolean =>
    (ts.isNumericLiteral(value) && value.text === "0") ||
    (ts.isArrayLiteralExpression(value) && value.elements.length === 0) ||
    (ts.isObjectLiteralExpression(value) && value.properties.length === 0)
  const walk = (n: ts.Node): void => {
    if (found) return
    if (ts.isCallExpression(n) && /^set[A-Z]/.test(calleeName(n))) {
      const value = n.arguments[0]
      if (value && (placeholder(value) || (/Failed|Error|Unavailable|Missing/.test(calleeName(n)) && value.kind === ts.SyntaxKind.TrueKeyword))) {
        found = true
        return
      }
    }
    if (ts.isBinaryExpression(n) && n.operatorToken.kind === ts.SyntaxKind.EqualsToken) {
      const left = n.left.getText()
      if (placeholder(n.right) || (/failed|error|unavailable|missing/i.test(left) && n.right.kind === ts.SyntaxKind.TrueKeyword)) {
        found = true
        return
      }
    }
    ts.forEachChild(n, walk)
  }
  walk(node)
  return found
}

function handlerOf(fn: ts.Node): Handler | null {
  if (!ts.isArrowFunction(fn) && !ts.isFunctionExpression(fn)) return null
  const parameter = fn.parameters[0]
  if (!parameter) return { body: fn.body, at: fn, looked: false }
  if (!ts.isIdentifier(parameter.name)) return { body: fn.body, at: fn, looked: true }
  return { body: fn.body, at: fn, looked: mentions(fn.body, parameter.name.text) }
}

/**
 * A file's own names for the formatter: `function why(e) { return
 * failureSentence(e, …) }` is the formatter to everything that calls `why`.
 * One hop only — a wrapper around a wrapper is rare and the hop that matters
 * is the one the console actually writes.
 */
function localWrappers(source: ts.SourceFile): ReadonlySet<string> {
  const names = new Set<string>()
  const keep = (name: string | undefined, body: ts.Node | undefined): void => {
    if (!name || !body) return
    if (keepsTheCode(body)) names.add(name)
  }
  const walk = (node: ts.Node): void => {
    if (ts.isFunctionDeclaration(node) && node.name && node.body) keep(node.name.text, node.body)
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) {
      const value = node.initializer
      if (ts.isArrowFunction(value) || ts.isFunctionExpression(value)) keep(node.name.text, value.body)
    }
    if (ts.isMethodDeclaration(node) && ts.isIdentifier(node.name) && node.body) keep(node.name.text, node.body)
    ts.forEachChild(node, walk)
  }
  walk(source)
  return names
}

/* ---- scanning -------------------------------------------------------------- */

/** Whether the site's own lines carry `// refusal-ok: <why>`; returns the why. */
function allowance(source: ts.SourceFile, node: ts.Node, also?: ts.Node): string | null {
  const text = source.getFullText()
  const first = source.getLineAndCharacterOfPosition(node.getStart()).line
  const last = source.getLineAndCharacterOfPosition((also ?? node).getEnd()).line
  const lines = text.split("\n")
  for (let line = Math.max(0, first - 1); line <= Math.min(lines.length - 1, last); line += 1) {
    const found = /\/\/\s*refusal-ok:\s*(.+?)\s*$/.exec(lines[line] ?? "")
    if (found && found[1] && found[1].length >= 8) return found[1]
  }
  return null
}

function lineOf(source: ts.SourceFile, node: ts.Node): number {
  return source.getLineAndCharacterOfPosition(node.getStart()).line + 1
}

/** The catch-all as one line, for a report somebody reads. */
function oneLine(node: ts.Node | undefined): string {
  if (!node) return "(nothing)"
  const text = node.getText().replace(/\s+/g, " ").trim()
  return text.length > 96 ? text.slice(0, 93) + "…" : text
}

/** One file. `file` is the name the report prints; `text` is its source. */
export function scanSource(file: string, text: string): { sites: Site[]; chains: number } {
  const kind = file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.ES2022, true, kind)
  const sites: Site[] = []
  const seen = new Set<ts.Node>()
  const wrappers = localWrappers(source)
  let chains = 0

  const add = (node: ts.Node, site: Omit<Site, "file" | "line" | "allowed" | "span">, also?: ts.Node): void => {
    sites.push({
      ...site,
      file,
      line: lineOf(source, node),
      allowed: allowance(source, node, also),
      span: [node.getStart(), node.getEnd()],
    })
  }

  /**
   * One rejection handler, judged by what it did with what it was handed.
   * Words and no formatter is a swallow either way; which one it is depends on
   * whether the refusal was never looked at, or looked at and spent on nothing.
   */
  const report = (at: ts.Node, handler: Handler, terminal: string): void => {
    if (keepsTheCode(handler.body, wrappers)) return
    if (!handler.looked && dropsIntoPlaceholder(handler.body)) {
      add(at, { kind: "uncertainty_dropped", codes: [], terminal })
      return
    }
    if (!saysWords(handler.body)) return
    if (!handler.looked) {
      add(at, { kind: "refusal_dropped", codes: [], terminal })
      return
    }
    if (holdsTheCode(handler.body)) {
      add(at, { kind: "code_unspent", codes: codesIn(handler.body), terminal })
    }
  }

  const walk = (node: ts.Node): void => {
    // A ladder that branches on a named refusal and then says something.
    if (namedCode(node)) {
      const chain = chainOf(node)
      if (chain) {
        const head: ts.Node = chain.head
        if (!seen.has(head)) {
          seen.add(head)
          chains += 1
          const tail = catchAllOf(chain)
          for (const rung of tail.stepped) seen.add(rung)
          const arms = [...armsOf(chain), ...tail.stepped.flatMap((r) => armsOf({ kind: "if", head: r as ts.IfStatement }))]
          if (decidesWords(chain, arms)) {
            const settled =
              tail.kind === "implicit" ||
              tail.nodes.some((n) => keepsTheCode(n, wrappers)) ||
              tail.nodes.every(defersUpwards) ||
              (tail.kind === "after" && !tail.nodes.some(saysWords))
            if (!settled) {
              add(
                head,
                {
                  kind: "fixed_catch_all",
                  codes: [...new Set([...codesIn(head), ...tail.stepped.flatMap(codesIn)])],
                  terminal: oneLine(tail.nodes[0]),
                },
                tail.nodes[0],
              )
            }
          }
        }
      }
    }

    // A handler that was given the refusal and says words without reading it.
    if (ts.isCatchClause(node)) {
      const bound = node.variableDeclaration
      const looked = !!bound && ts.isIdentifier(bound.name) && mentions(node.block, bound.name.text)
      report(node, { body: node.block, at: node, looked }, oneLine(node.block.statements[0]))
    }
    if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression)) {
      const called = node.expression.name.text
      const fn = called === "catch" ? node.arguments[0] : called === "then" ? node.arguments[1] : undefined
      const handler = fn ? handlerOf(fn) : null
      if (handler) report(handler.at, handler, oneLine(handler.body))
    }

    // `Promise.allSettled`: a result whose `.status` decides the screen and
    // whose `.reason` nothing ever reads, so the refused half draws as empty.
    if (
      ts.isCallExpression(node) &&
      ts.isPropertyAccessExpression(node.expression) &&
      node.expression.name.text === "allSettled"
    ) {
      const declaration = settledBinding(node)
      if (declaration && ts.isArrayBindingPattern(declaration.name)) {
        const scope = enclosingBody(declaration) ?? source
        for (const element of declaration.name.elements) {
          if (!ts.isBindingElement(element) || !ts.isIdentifier(element.name)) continue
          const name = element.name.text
          if (!reads(scope, name, "status")) continue
          if (reads(scope, name, "reason")) continue
          add(element, { kind: "reason_dropped", codes: [], terminal: name + ".reason" })
        }
      }
    }

    ts.forEachChild(node, walk)
  }

  walk(source)
  // A handler whose ladder is already named is not a second finding: the
  // ladder is where the sentence is chosen, and the handler only holds it.
  const named = sites.filter((s) => s.kind === "fixed_catch_all")
  const kept = sites.filter(
    (s) =>
      s.kind !== "code_unspent" ||
      !named.some((inner) => inner.span[0] >= s.span[0] && inner.span[1] <= s.span[1]),
  )
  kept.sort((a, b) => a.line - b.line)
  return { sites: kept, chains }
}

/** The `const [a, b] = await Promise.allSettled(...)` this call belongs to. */
function settledBinding(call: ts.CallExpression): ts.VariableDeclaration | null {
  let node: ts.Node = call
  for (let up = 0; up < 3 && node.parent; up += 1) {
    node = node.parent
    if (ts.isVariableDeclaration(node)) return node
  }
  return null
}

function enclosingBody(node: ts.Node): ts.Node | null {
  let current: ts.Node | undefined = node.parent
  while (current) {
    if (ts.isBlock(current) || ts.isSourceFile(current)) return current
    current = current.parent
  }
  return null
}

function reads(scope: ts.Node, owner: string, field: string): boolean {
  let found = false
  const walk = (n: ts.Node): void => {
    if (found) return
    if (
      ts.isPropertyAccessExpression(n) &&
      n.name.text === field &&
      ts.isIdentifier(n.expression) &&
      n.expression.text === owner
    ) {
      found = true
      return
    }
    ts.forEachChild(n, walk)
  }
  walk(scope)
  return found
}

/* ---- the tree -------------------------------------------------------------- */

/** Copied Swift modules, tests and generated files: read, never corrected here. */
function skipped(path: string): boolean {
  return (
    path.includes("/legacy/js/") ||
    path.endsWith(".test.ts") ||
    path.endsWith(".e2e.ts") ||
    path.endsWith(".d.ts") ||
    path.includes("/node_modules/")
  )
}

function sources(root: string): string[] {
  const out: string[] = []
  const walk = (directory: string): void => {
    for (const entry of readdirSync(directory).sort()) {
      const path = join(directory, entry)
      if (statSync(path).isDirectory()) {
        walk(path)
        continue
      }
      if (!path.endsWith(".ts") && !path.endsWith(".tsx")) continue
      if (skipped(path.split(sep).join("/"))) continue
      out.push(path)
    }
  }
  walk(root)
  return out
}

/** How few files or ladders mean the scan did not happen rather than found nothing. */
const FEWEST_FILES = 40
const FEWEST_CHAINS = 8

/**
 * Scan `web/console/src` under `repo`. The report is a failure when anything
 * violates, and a failure when it cannot tell — a tree it could not walk, a
 * parser that returned nothing, or a scan that no longer catches the shape it
 * was written for are all `indeterminate`, never green.
 */
export function scanConsole(repo: string): Report {
  const root = resolve(repo, "web/console/src")
  const audits = scanAudits(repo)
  let files: string[]
  try {
    files = sources(root)
  } catch (error) {
    return {
      files: 0,
      chains: 0,
      sites: [],
      violations: [],
      allowed: [],
      audits,
      indeterminate: "cannot read " + root + ": " + String(error),
    }
  }
  const sites: Site[] = []
  let chains = 0
  for (const path of files) {
    const name = relative(repo, path).split(sep).join("/")
    const found = scanSource(name, readFileSync(path, "utf8"))
    sites.push(...found.sites)
    chains += found.chains
  }
  sites.sort((a, b) => (a.file === b.file ? a.line - b.line : a.file < b.file ? -1 : 1))
  const report: Report = {
    files: files.length,
    chains,
    sites,
    violations: sites.filter((s) => !s.allowed),
    allowed: sites.filter((s) => !!s.allowed),
    audits,
    indeterminate: null,
  }
  if (files.length < FEWEST_FILES) report.indeterminate = `only ${files.length} files under ${root}`
  else if (chains < FEWEST_CHAINS) report.indeterminate = `only ${chains} refusal ladders found; the scan is not reading this tree`
  else report.indeterminate = selfCheck() ?? selfCheckAudits() ?? audits.indeterminate
  return report
}

/* ---- the fixture that keeps the guard honest ------------------------------- */

/**
 * `session/GitPanel.tsx` as it read before `gitSentence()` — the shape this
 * guard exists for, kept here as the one positive it must still catch. A scan
 * that does not go red on this is broken, not clean.
 */
export const KNOWN_POSITIVE = `
import { T } from "../legacy/bridge.js"
export function panel(setState: (s: unknown) => void) {
  readGit(id).then(
    (answer) => setState({ loading: false, error: null, snapshot: answer.git ?? null }),
    (e: unknown) => {
      setState({
        loading: false,
        error: (e as { code?: string })?.code === "not_a_repo" ? T.webGitNotRepo : T.webGitFailed,
        snapshot: null,
      })
    },
  )
}
`

/** The same panel once the refusal reached `gitSentence`, which must stay green. */
export const KNOWN_NEGATIVE = `
import { gitSentence } from "../legacy/git-bridge.js"
import { T } from "../legacy/bridge.js"
export function panel(setState: (s: unknown) => void) {
  readGit(id).then(
    (answer) => setState({ loading: false, error: null, snapshot: answer.git ?? null }),
    (e: unknown) => {
      setState({ loading: false, error: gitSentence(e, T.webGitFailed), snapshot: null })
    },
  )
}
`

/** "" when the guard still catches its own fixture, and why not when it does not. */
export function selfCheck(): string | null {
  const positive = scanSource("fixture/GitPanel.tsx", KNOWN_POSITIVE).sites
  if (!positive.some((s) => s.kind === "fixed_catch_all")) {
    return "the guard no longer catches GitPanel.tsx as it read before gitSentence()"
  }
  const negative = scanSource("fixture/GitPanelFixed.tsx", KNOWN_NEGATIVE).sites
  if (negative.length) {
    return "the guard now refuses GitPanel.tsx as it reads after gitSentence(): " + negative[0]!.terminal
  }
  return null
}

/* ---- the command line ------------------------------------------------------ */

function repoRoot(): string {
  return resolve(dirname(fileURLToPath(import.meta.url)), "../../../..")
}

function main(): void {
  const report = scanConsole(repoRoot())
  for (const site of report.sites) {
    const mark = site.allowed ? "ok " : "RED"
    const codes = site.codes.length ? " [" + site.codes.join(", ") + "]" : ""
    process.stdout.write(`${mark} ${site.file}:${site.line} ${site.kind}${codes} → ${site.terminal}\n`)
    if (site.allowed) process.stdout.write(`      refusal-ok: ${site.allowed}\n`)
  }
  for (const issue of report.audits.findings) {
    const mark = issue.disposition === "locked" ? "LOCK" : "OPEN"
    const where = issue.evidence.map((item) => `${item.file}:${item.line}`).join(", ")
    process.stdout.write(`${mark} ${issue.id} ${issue.family}/${issue.cause} — ${issue.title}\n`)
    process.stdout.write(`     ${where}\n`)
  }
  process.stdout.write(
    `\n${report.files} files, ${report.chains} refusal ladders, ` +
      `${report.violations.length} unanswered, ${report.allowed.length} allowed; ` +
      `${report.audits.rules} cross-language rules over ${report.audits.files} files, ` +
      `${report.audits.open.length} open, ${report.audits.locked.length} locked\n`,
  )
  if (report.indeterminate) {
    process.stdout.write("indeterminate: " + report.indeterminate + "\n")
    process.exit(3)
  }
  process.exit(report.violations.length || report.audits.open.length ? 1 : 0)
}

if (process.argv[1] && process.argv[1].endsWith("refusals/scan.ts")) main()
