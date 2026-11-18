import { chevronPath, type ChevronDirection } from "./chevron.js"

export function ChevronIcon({ direction = "down", className = "" }: { direction?: ChevronDirection; className?: string }) {
  return <svg className={`control-chevron ${className}`} width="14" height="14" viewBox="0 0 16 16"
    fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"
    aria-hidden="true" focusable="false"><path d={chevronPath[direction]} /></svg>
}
