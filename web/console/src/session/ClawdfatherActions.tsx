import { catalogWord } from "../catalog.js"
import { SidebarIcon, type SidebarIconName } from "../SidebarIcon.js"
import type { Snippet } from "../legacy/snippets-bridge.js"
import "./clawdfather-suggestions.css"

/**
 * Clawdfather's tab in the Snippet / Playbook sheet.
 *
 * The shipped actions come first and stay: they are the console's, so they
 * carry no menu and cannot be removed. The person's own actions follow them,
 * stored as `clawdfather` reusable text on this machine, and only those carry
 * the `···` that opens their editor. Pressing either kind puts its words in the
 * composer and sends nothing, as a snippet does.
 */
const builtins = [
  { title: "4c4eebeb189f", prompt: "d3167f38f5fb", icon: "verify" },
  { title: "4fb4ad1b7401", prompt: "365604b32858", icon: "devices" },
  { title: "3d37fb3af4ee", prompt: "6c7e323f158d", icon: "settings" },
  { title: "98091e56303b", prompt: "673f223fa434", icon: "transfer" },
  { title: "a956b8eeb16c", prompt: "3e8c30b7e30d", icon: "work" },
] as const satisfies readonly { title: string; prompt: string; icon: SidebarIconName }[]

export const clawdfatherWord = (key: string) => catalogWord("clawdfatherMenu", key)

export function ClawdfatherActions({ customs, onPick, onCoordination, onEdit, mayEdit }: {
  customs: Snippet[]
  onPick: (prompt: string) => void
  onCoordination: () => void
  onEdit: (custom: Snippet) => void
  mayEdit: boolean
}) {
  return (
    <div className="snippet-list clawdfather-actions" id="clawdfather-actions" role="tabpanel" aria-labelledby="clawdfather-tab">
      <div className="clawdfather-suggestions-list">
        <button type="button" onClick={onCoordination}>
          <SidebarIcon name="work" />
          <span>{catalogWord("resourceCoordination", "title")}</span>
        </button>
        {builtins.map((item) => (
          <button key={item.title} type="button" onClick={() => onPick(catalogWord("literal", item.prompt))}>
            <SidebarIcon name={item.icon} />
            <span>{catalogWord("literal", item.title)}</span>
          </button>
        ))}
      </div>
      {customs.length ? <div className="snippet-group clawdfather-custom-group">
        <h3 className="snippet-group-title">{clawdfatherWord("mine")}</h3>
        <div className="clawdfather-suggestions-list">
          {customs.map((custom) => (
            <div className="clawdfather-custom" key={custom.id}>
              <button type="button" className="clawdfather-custom-pick" onClick={() => onPick(custom.body)}>
                <SidebarIcon name="playbook" />
                <span>{custom.title}</span>
              </button>
              {mayEdit ? <button type="button" className="snippet-more clawdfather-custom-more"
                aria-label={clawdfatherWord("edit") + " " + custom.title} title={clawdfatherWord("edit")}
                onClick={() => onEdit(custom)}>···</button> : null}
            </div>
          ))}
        </div>
      </div> : null}
    </div>
  )
}
