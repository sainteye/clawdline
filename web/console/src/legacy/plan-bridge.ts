// The Plan page's way into its copied module.
//
// `js/view/plan.js` is the Swift app's, byte for byte, and so is the one module
// it imports, `js/net/billing.js` (for the simulation panel's words). It is
// bound here as that app's `main.js` binds it on the page the Mac serves:
//
// - `billing` is null. That is the local transport's answer and not a failure:
//   this copy of the page has no Cloud account to charge, and the module draws
//   its `not_here` state — Free, a sentence saying so, and a link to the hosted
//   console — with no button that could start a payment.
// - `signInURL` answers the empty string, which the module takes as "nowhere
//   to go".
// - `consoleOrigin` is the hosted console's, as the original's is when it has no
//   Cloud configuration.
// - `navigate` is left to the module's default. With no billing client nothing
//   calls it.
import { bindPlanPage as bindPlanPageOriginal } from "./js/view/plan.js"

/** What `bindPlanPage` hands back. */
export interface PlanPage {
  enter(options?: { returning?: boolean }): Promise<void>
  leave(): void
  state(): string
  plan(): unknown
}

/** `main.js`'s element table for `bindPlanPage`, in its order. */
export const PLAN_ELEMENT_IDS = [
  "plan",
  "plan-title",
  "plan-lede",
  "plan-close",
  "plan-includes",
  "plan-tier",
  "plan-tier-note",
  "plan-say",
  "plan-alert",
  "plan-limits",
  "plan-fine",
  "plan-upgrade",
  "plan-portal",
  "plan-signin",
  "plan-retry",
  "plan-recheck",
  "plan-elsewhere",
  "plan-console",
  "plan-simulation",
  "plan-simulation-title",
  "plan-simulation-values",
  "plan-simulation-actual",
  "plan-simulation-free",
  "plan-simulation-pro",
] as const

/** `main.js`'s default when there is no Cloud configuration. */
export const PLAN_CONSOLE_ORIGIN = "https://app.clawdline.com"

/** Bind the page once its markup is in the document, with the local seams. */
export function bindPlan(doc: Document): PlanPage {
  const elements: Record<string, HTMLElement | null> = {}
  for (const id of PLAN_ELEMENT_IDS) elements[id] = doc.getElementById(id)
  return (bindPlanPageOriginal as (elements: Record<string, HTMLElement | null>, seams: Record<string, unknown>) => PlanPage)(
    elements,
    {
      billing: null,
      signInURL: () => "",
      consoleOrigin: PLAN_CONSOLE_ORIGIN,
    },
  )
}
