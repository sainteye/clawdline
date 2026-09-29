import "./project-tree-icon.css"

type IconKind = "folder" | "file" | "link" | "other"

/** Familiar silhouettes keep folders and documents distinct at small sizes. */
export function ProjectTreeIcon({ kind }: { kind: IconKind }) {
  return <svg className={`project-tree-icon project-tree-icon--${kind}`} viewBox="0 0 24 24"
    aria-hidden="true" focusable="false">
    {kind === "folder" && <>
      <path className="project-tree-icon-fill" d="M3 7V5.5C3 4.7 3.7 4 4.5 4h4l2 2h8c.8 0 1.5.7 1.5 1.5v11c0 .8-.7 1.5-1.5 1.5h-14C3.7 20 3 19.3 3 18.5Z" />
      <path d="M3 7V5.5C3 4.7 3.7 4 4.5 4h4l2 2h8c.8 0 1.5.7 1.5 1.5v11c0 .8-.7 1.5-1.5 1.5h-14C3.7 20 3 19.3 3 18.5Z" />
      <path d="M3 8.5h17" />
    </>}
    {(kind === "file" || kind === "other") && <>
      <path d="M6 2.75h7l4.25 4.25v12.25c0 1.1-.9 2-2 2H6c-1.1 0-2-.9-2-2V4.75c0-1.1.9-2 2-2Z" />
      <path d="M13 2.75V7h4.25M8 11h5.5M8 14.5h5.5" />
    </>}
    {kind === "link" && <>
      <path d="m10 14 4-4M8.25 16.25l-1.5 1.5a3.5 3.5 0 0 1-5-5l4-4a3.5 3.5 0 0 1 5 0M15.75 7.75l1.5-1.5a3.5 3.5 0 0 1 5 5l-4 4a3.5 3.5 0 0 1-5 0" />
    </>}
  </svg>
}
