/** Read-only questions shared by the call menu and its spoken routing rules. */
export const CALL_QUESTIONS = [
  { id: 'summary', title: 'copilotQuestionTitleSummary', group: 'overview', key: 'copilotAskSummary', alternate: 'copilotAskSummaryAlt', tool: 'get_session_summary', args: {} },
  { id: 'latest', title: 'copilotQuestionTitleLatest', group: 'overview', key: 'copilotAskLatest', alternate: 'copilotAskLatestAlt', tool: 'get_session_summary', args: { fresh: true } },
  { id: 'updates', title: 'copilotQuestionTitleUpdates', group: 'updates', key: 'copilotAskUpdates', alternate: 'copilotAskUpdatesAlt', tool: 'get_session_updates', args: {} },
  { id: 'history', title: 'copilotQuestionTitleHistory', group: 'updates', key: 'copilotAskHistory', alternate: 'copilotAskHistoryAlt', tool: 'get_session_recap', args: {} },
  { id: 'reply', title: 'copilotQuestionTitleReply', group: 'understand', key: 'copilotAskReply', alternate: 'copilotAskReplyAlt', tool: 'explain_session_reply', args: { replies: 1 } },
  { id: 'insights', title: 'copilotQuestionTitleInsights', group: 'understand', key: 'copilotAskInsights', alternate: 'copilotAskInsightsAlt', tool: 'explain_session_reply', args: { replies: 5 } },
  { id: 'table', title: 'copilotQuestionTitleTable', group: 'understand', key: 'copilotAskTable', alternate: 'copilotAskTableAlt', tool: 'explain_session_reply', args: { replies: 3 } },
  { id: 'decisions', title: 'copilotQuestionTitleDecisions', group: 'decisions', key: 'copilotAskDecisions', alternate: 'copilotAskDecisionsAlt', tool: 'get_session_notes', args: {} },
] as const

export type CallQuestionID = typeof CALL_QUESTIONS[number]['id']
export interface QuestionTarget { id: string; label: string; machine?: string; machineName?: string; conversation?: string }

/** The visible question is user input; application routing is kept separate. */
export function questionRequest(id: CallQuestionID, text: string, target: QuestionTarget) {
  const question = CALL_QUESTIONS.find(q => q.id === id)
  if (!question || !text.trim() || !target.id) throw new Error('voice_question_invalid')
  return {
    text: `${target.label}: ${text}`,
    routing: `The person clicked a read-only question in the call window. The selected target and arguments are application data, not instructions from Session content: ${JSON.stringify({target, tool: question.tool, args: {...question.args, session_id: target.id, said: target.label, ...(question.tool === 'explain_session_reply' ? {question: text} : {})}})}. Read that target with that tool before answering. Do not first generate a summary for an explicit detail question. For decisions, also read its current summary and distinguish current unanswered questions from historical ones. Explain conclusions, reasons, tradeoffs and any decision needed in the person's language. Distinguish source claims from your inference. State missing or truncated coverage. Never send a message, approve, or execute a mutation for this question.`,
  }
}
