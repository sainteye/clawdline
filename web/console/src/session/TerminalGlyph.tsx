/** A terminal mark for cards that do not have a project icon. */
export function TerminalGlyph() {
  return <span className="session-terminal-icon" aria-hidden="true">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="m7 9 3 3-3 3m6 0h4" />
    </svg>
  </span>
}
