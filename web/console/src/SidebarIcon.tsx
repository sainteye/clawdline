export type SidebarIconName = "sessions" | "devices" | "projects" | "plan" | "settings" | "work" | "now" | "verify"

/**
 * One visual language for the drawer, with a distinct object for each place:
 * terminal, devices, folder, payment card, settings gear, board, clock, and a
 * checked list.
 * The label beside the mark remains the accessible name, so these are
 * deliberately decorative rather than eight repeated announcements.
 */
export function SidebarIcon({ name }: { name: SidebarIconName }) {
  return (
    <svg
      className="sidebar-icon"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {name === "sessions" && (
        <>
          <rect x="2.75" y="4" width="18.5" height="16" rx="2.25" />
          <path d="m7 9 2.25 2L7 13" />
          <path d="M12.5 14h4.5" />
        </>
      )}
      {name === "devices" && (
        <>
          <rect x="2.75" y="4" width="14" height="11" rx="2" />
          <path d="M7.5 19h4.5M9.75 15v4" />
          <rect x="17.25" y="8" width="4" height="11" rx="1.25" />
        </>
      )}
      {name === "projects" && (
        <path d="M3 7.25A2.25 2.25 0 0 1 5.25 5h4l2 2h7.5A2.25 2.25 0 0 1 21 9.25v7.5A2.25 2.25 0 0 1 18.75 19H5.25A2.25 2.25 0 0 1 3 16.75Z" />
      )}
      {name === "plan" && (
        <>
          <rect x="3" y="5" width="18" height="14" rx="2.25" />
          <path d="M3 9.25h18M7 14h3" />
        </>
      )}
      {name === "settings" && (
        <>
          <path d="M10.09 4.54h3.82l.54 1.66 1.35.77 1.71-.35 1.9 3.3-1.16 1.3v1.56l1.16 1.3-1.9 3.3-1.71-.35-1.35.77-.54 1.66h-3.82l-.54-1.66-1.35-.77-1.71.35-1.9-3.3 1.16-1.3v-1.56l-1.16-1.3 1.9-3.3 1.71.35 1.35-.77Z" />
          <circle cx="12" cy="12" r="2.5" />
        </>
      )}
      {name === "work" && (
        <>
          <rect x="3" y="4" width="5" height="16" rx="1.25" />
          <rect x="9.5" y="4" width="5" height="10.5" rx="1.25" />
          <rect x="16" y="4" width="5" height="13.5" rx="1.25" />
        </>
      )}
      {name === "verify" && (
        <>
          <rect x="4" y="3.5" width="16" height="17" rx="2.25" />
          <path d="m8 9 1.5 1.5L12.5 7.5M8 15l1.5 1.5 3-3M15 9.5h1.5M15 15.5h1.5" />
        </>
      )}
      {name === "now" && (
        <>
          <circle cx="12" cy="12" r="9" />
          <path d="M12 7v5l3.25 2" />
        </>
      )}
    </svg>
  )
}
