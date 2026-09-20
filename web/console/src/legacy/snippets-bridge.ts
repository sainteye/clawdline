// Snippets — the copied data module, the press that opens the sheet, and the
// one way text reaches the composer.
//
// `js/view/snippets-data.js` is the Swift app's, byte for byte, and it is the
// whole of what this feature decides: which rows a session sees, in which two
// groups, what a row's line says, which controls may be drawn at all, what each
// request's body is, and the markup of the list itself. `input/snippets.js` is
// not copied — it builds its own DOM island on the body at import, through ids
// React has not drawn yet — so `session/Snippets.tsx` owns the markup around the
// list and the presses, and takes every answer from here.
import {
  byteLength as byteLengthOriginal,
  rememberSnippetProject as rememberSnippetProjectOriginal,
  snippetActions as snippetActionsOriginal,
  snippetCreateBody as snippetCreateBodyOriginal,
  snippetDraft as snippetDraftOriginal,
  snippetDraftFromText as snippetDraftFromTextOriginal,
  snippetDraftProblem as snippetDraftProblemOriginal,
  snippetGroups as snippetGroupsOriginal,
  snippetOrder as snippetOrderOriginal,
  snippetOrderBody as snippetOrderBodyOriginal,
  snippetPatchBody as snippetPatchBodyOriginal,
  snippetProjectFor as snippetProjectForOriginal,
  snippetReorder as snippetReorderOriginal,
  snippetScopeSwap as snippetScopeSwapOriginal,
  snippetStarters as snippetStartersOriginal,
  snippetSummary as snippetSummaryOriginal,
  snippetsListHTML as snippetsListHTMLOriginal,
  starterPress as starterPressOriginal,
} from "./js/view/snippets-data.js"
import { failureSentence as failureSentenceOriginal } from "./js/core/failure-text.js"

/** One stored snippet, as `GET /v1/snippets` answers it. */
export interface Snippet {
  id: string
  title: string
  body: string
  scope: "global" | "project"
  /** Absent on a global snippet — never null; see `snippetCreateBody`. */
  project?: string
  position: number
  created_at: number
  updated_at: number
}

/** The project the machine resolved for one session, beside its list. */
export interface SnippetProject {
  key: string
  label: string
}

export interface SnippetAnswer {
  project?: SnippetProject
  snippets: Snippet[]
}

/** The two groups the sheet draws, and what it calls the project. */
export interface SnippetModel {
  project: SnippetProject | null
  groups: { scope: "project" | "global"; rows: Snippet[] }[]
  count: number
}

/** A snippet being written: an existing row's fields, or a blank one's. */
export interface SnippetDraft {
  id: string
  title: string
  body: string
  scope: "global" | "project"
  project: string
}

/** Which of the five routes a transport has. This console has all five. */
export interface SnippetControls {
  read: boolean
  create: boolean
  update: boolean
  remove: boolean
  order: boolean
}

/** What this device and this transport may do, together. */
export interface SnippetMay {
  create: boolean
  update: boolean
  remove: boolean
  order: boolean
  menu: boolean
}

/** How the `⋯` menu and the header's mark ask for the sheet (`overlays/events.ts`'s idiom). */
export const OPEN_SNIPPETS = "clawdline:open-snippets"

/** How the sheet puts text in the composer without owning it (`session/Composer.tsx` listens). */
export const COMPOSE_APPEND = "clawdline:compose-append"

/**
 * The machine has said which project a session is in.
 *
 * The original redraws the session header from inside the sheet
 * (`renderDetailHead`), because the header's mark is keyed by the *resolved*
 * project and only the read knows it. Here the header listens instead, so the
 * sheet still does not have to know how a header is built.
 */
export const SNIPPET_PROJECT_KNOWN = "clawdline:snippet-project"

/** Say that `snippetProjectFor` has a new answer for the session on screen. */
export function announceSnippetProject(): void {
  document.dispatchEvent(new CustomEvent(SNIPPET_PROJECT_KNOWN))
}

/** `#session-snippets` and `#detail-snippets`: open the sheet on the session on screen. */
export function requestSnippets(): void {
  document.dispatchEvent(new CustomEvent(OPEN_SNIPPETS))
}

/**
 * Put text on the end of what is in the composer, and nothing else.
 *
 * **It never sends.** `input/snippets.js` says why, and it is the same reason
 * dictation stops in the box: words put there by a thumb in a pocket are a typo,
 * and the same words already posted to a terminal are an incident. The send
 * button stays the only thing that sends.
 */
export function appendToComposer(text: string): void {
  if (!text) return
  document.dispatchEvent(new CustomEvent(COMPOSE_APPEND, { detail: { text } }))
}

export const snippetGroups = snippetGroupsOriginal as (
  answer: SnippetAnswer | null,
  context: { machine?: string | null; project?: string | null },
) => SnippetModel
export const snippetOrder = snippetOrderOriginal as (model: SnippetModel | null) => Snippet[]
export const snippetsListHTML = snippetsListHTMLOriginal as (
  model: SnippetModel | null,
  options: {
    controls?: SnippetControls
    readOnly?: boolean
    loading?: boolean
    error?: string
    menuFor?: number
  },
) => string
export const snippetActions = snippetActionsOriginal as (
  controls: SnippetControls,
  readOnly: boolean,
) => SnippetMay
export const snippetSummary = snippetSummaryOriginal as (body: string | undefined) => string
export const snippetStarters = snippetStartersOriginal as () => {
  key: string
  title: string
  body: string
  scope: "global"
}[]
export const starterPress = starterPressOriginal as (
  starter: { title: string; body: string; scope: "global" },
  options: { mayCreate: boolean },
) => { body: string; create: Record<string, string> | null } | null
export const snippetDraft = snippetDraftOriginal as (
  row: Snippet | null,
  context?: { title?: string; body?: string; scope?: string; project?: string },
) => SnippetDraft
export const snippetDraftFromText = snippetDraftFromTextOriginal as (text: string) => SnippetDraft
export const snippetDraftProblem = snippetDraftProblemOriginal as (
  draft: SnippetDraft,
) => "" | "empty" | "long" | "scope"
export const snippetCreateBody = snippetCreateBodyOriginal as (
  draft: SnippetDraft,
) => Record<string, string> | null
export const snippetPatchBody = snippetPatchBodyOriginal as (
  draft: SnippetDraft,
  row: Snippet,
) => Record<string, string> | null
export const snippetScopeSwap = snippetScopeSwapOriginal as (
  row: Snippet,
  projectKey: string,
) => { scope: "global" | "project"; project?: string } | null
export const snippetReorder = snippetReorderOriginal as (
  rows: Snippet[],
  id: string,
  delta: number,
) => Snippet[] | null
export const snippetOrderBody = snippetOrderBodyOriginal as (
  scope: "global" | "project",
  projectKey: string,
  rows: Snippet[],
) => { scope: string; project?: string; order: string[] } | null
export const byteLength = byteLengthOriginal as (text: string) => number

/** What the machine last said one session's project is; null while nothing has. */
export const snippetProjectFor = snippetProjectForOriginal as (
  sessionRowID: string,
) => SnippetProject | null
export const rememberSnippetProject = rememberSnippetProjectOriginal as (
  sessionRowID: string,
  project: SnippetProject | null | undefined,
) => void

/**
 * One failure as a line of text, by its code and never by its message
 * (`core/failure-text.js`).
 *
 * The sheet says what the machine refused: `snippet_limit_reached` carries how
 * many there are and no string of ours knows that, so the code rides on the end
 * of the sentence where somebody can read it out.
 */
export const failureSentence = failureSentenceOriginal as (
  error: unknown,
  fallback?: string | { sentence?: string; fallback?: string },
) => string
