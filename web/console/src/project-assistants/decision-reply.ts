import type * as W from "@clawdline/contract"

/** What a person's answer to a question card shows instead of its full prompt. */
export type DecisionReplyView = { question: string; answer: string }

const REQUEST_HEADER = "> Request from another Bot:"
const QUESTION_HEADER = "> Your question for me:"

/**
 * The card answer a person's message carries. Newer messages name it in
 * `decision_reply`; earlier ones are recognized by the daemon's own prompt
 * shape (DecisionPrompt): quoted headers, a blank line, then the answer.
 */
export function decisionReplyOf(message: Pick<W.ProjectAssistantMessage, "kind" | "text" | "decision_reply">): DecisionReplyView | null {
  if (message.kind !== "user") return null
  const d = message.decision_reply
  if (d) return { question: d.question, answer: d.answer }
  const text = message.text
  if (!text.startsWith(REQUEST_HEADER + "\n") && !text.startsWith(QUESTION_HEADER + "\n")) return null
  const lines = text.split("\n")
  let i = 0, inQuestion = false
  const question: string[] = []
  for (; i < lines.length && lines[i].startsWith(">"); i++) {
    if (lines[i] === QUESTION_HEADER) { inQuestion = true; continue }
    if (inQuestion) question.push(lines[i].replace(/^> ?/u, ""))
  }
  if (!inQuestion || lines[i] !== "") return null
  const answer = lines.slice(i + 1).join("\n").trim()
  return answer ? { question: question.join("\n").trim(), answer } : null
}
