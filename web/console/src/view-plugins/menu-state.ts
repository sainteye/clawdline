/** The Manage menu: closed, showing its options, or asking before Remove. */
export type MenuState = "closed" | "open" | "confirm"
export type MenuEvent = "toggle" | "remove" | "dismiss"

/** Remove never deletes on its first press: it only moves the menu to the question. */
export function nextMenuState(state: MenuState, event: MenuEvent): MenuState {
  if (event === "dismiss") return "closed"
  if (event === "toggle") return state === "closed" ? "open" : "closed"
  return state === "open" ? "confirm" : state
}
