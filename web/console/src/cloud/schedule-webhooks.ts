import { InstaBoxRequestError, instaBoxRequest, instaBoxWakePath, readInstaBoxBinding } from "./instabox-wake.js"
import type { ScheduleClock, ScheduleClockRule } from "./schedule-clock.js"
import {
  ScheduleWebhookClient,
  type ScheduleWebhookClientAPI,
  type ScheduleWebhookHook,
} from "../legacy/schedules-bridge.js"
import { scheduleOwner } from "./schedule-machines.js"
import { hookMoveRequest } from "./schedule-move.js"

/** A hook as `read` answers it, whichever of its two shapes. */
function hookOf(answer: { hook?: ScheduleWebhookHook } | ScheduleWebhookHook): ScheduleWebhookHook {
  return "hook" in answer && answer.hook ? answer.hook : (answer as ScheduleWebhookHook)
}

/** The copied Cloud client method used by the old console's webhook binding flow. */
export interface ScheduleWebhookCommandClient {
  _publishCommand(
    machine: string,
    type: string,
    body: Record<string, unknown>,
    envelopeClass: "ctl",
  ): Promise<unknown>
}

export interface ScheduleWebhookManagement {
  client: ScheduleWebhookClientAPI
  machine(scheduleID: string | null): string
  bind(scheduleID: string, hookID: string, replaceHookID: string | null): Promise<ScheduleWebhookHook>
  /**
   * `bind` on a named machine: a schedule just created there is not in the list
   * yet. `hookRevision` is sent only by a move — the revision the hook is at,
   * null to read it from Cloud first; a new hook's bind leaves it out.
   */
  bindOn(
    machine: string,
    scheduleID: string,
    hookID: string,
    replaceHookID: string | null,
    hookRevision?: number | null,
  ): Promise<ScheduleWebhookHook>
  /** Cloud's move to `machine`; a null revision is read first. */
  move(hookID: string, machine: string, revision: number | null): Promise<ScheduleWebhookHook>
  copy(value: string): Promise<void>
  clockReady(machine: string): Promise<boolean>
  clocks(machine: string): Promise<ScheduleClock[]>
  clock(hook: string): Promise<ScheduleClock | null>
  clockBinding(machine: string, scheduleID: string, existing?: string): Promise<ScheduleWebhookHook>
  saveClock(hook: ScheduleWebhookHook, scheduleID: string, rule: ScheduleClockRule | null, enabled: boolean): Promise<void>
}

let installed: ScheduleWebhookManagement | null = null

/**
 * Install the hosted console's account-authenticated management seam.
 *
 * The public URL remains response-local inside ScheduleHistory. Binding follows
 * the old console exactly: publish one encrypted command to the selected Mac,
 * then read Cloud until the machine-credential activation is observable.
 */
export function installScheduleWebhookManagement(options: {
  apiOrigin: string
  machineID: string
  connected: () => ScheduleWebhookCommandClient
  copy?: (value: string) => Promise<void>
}): () => void {
  const client = new ScheduleWebhookClient({ origin: options.apiOrigin })
  const clockPath = (hook: string) => "/v1/schedule-webhooks/" + encodeURIComponent(hook) + "/clock"
  const management: ScheduleWebhookManagement = {
    async clockReady(machine) {
      return (await readInstaBoxBinding(options.apiOrigin, machine)).schedule_clock_ready === true
    },
    async clocks(machine) {
      return (await instaBoxRequest<{ clocks: ScheduleClock[] }>(
        options.apiOrigin, instaBoxWakePath(machine) + "/clocks",
      )).clocks
    },
    async clock(hook) {
      try {
        return (await instaBoxRequest<{ clock: ScheduleClock | null }>(options.apiOrigin, clockPath(hook))).clock
      } catch (error) {
        if (error instanceof InstaBoxRequestError && error.status === 404) return null
        throw error
      }
    },
    async clockBinding(machine, scheduleID, existing) {
      const current = existing ? hookOf(await client.read(existing)) : null
      if (current?.state === "active") return current
      const made = current?.state === "pending_binding" ? current : hookOf(await client.send(
        "POST", "/v1/schedule-webhooks", { machine_id: machine, clock_only: true }, true,
        "instabox-clock:" + machine + ":" + scheduleID + (current ? ":" + current.hook_id : ""),
      ))
      // Creation receipts remain pending even when the machine completed the binding.
      const observed = hookOf(await client.read(made.hook_id))
      if (observed.state === "active") return observed
      return management.bindOn(machine, scheduleID, observed.hook_id, existing === observed.hook_id ? null : existing ?? null)
    },
    async saveClock(hook, scheduleID, rule, enabled) {
      await instaBoxRequest(options.apiOrigin, clockPath(hook.hook_id), "PUT", {
        schedule_id: scheduleID, expected_revision: hook.revision, enabled, rule,
      })
    },
    client,
    // A schedule listed from another machine is bound on that machine: the
    // binding lives beside the schedule, in the store that fires it.
    machine: (scheduleID) => scheduleOwner(scheduleID) ?? options.machineID,
    bind: (scheduleID, hookID, replaceHookID) =>
      management.bindOn(scheduleOwner(scheduleID) ?? options.machineID, scheduleID, hookID, replaceHookID),
    bindOn: async (machine, scheduleID, hookID, replaceHookID, hookRevision) => {
      const requestID = crypto.randomUUID().toLowerCase()
      const body: Record<string, unknown> = {
        request_id: requestID,
        hook_id: hookID,
        schedule_id: scheduleID,
        replace_hook_id: replaceHookID,
      }
      if (hookRevision !== undefined) {
        body.hook_revision = hookRevision ?? hookOf(await client.read(hookID)).revision
      }
      await options.connected()._publishCommand(machine, "schedule-webhook-bind-v1", body, "ctl")
      for (let attempt = 0; attempt < 20; attempt += 1) {
        const current = hookOf(await client.read(hookID))
        if (current.state === "active") return current
        await new Promise((resolve) => setTimeout(resolve, 250))
      }
      throw new Error("webhook activation not observed")
    },
    move: async (hookID, machine, revision) => {
      const expected = revision ?? hookOf(await client.read(hookID)).revision
      const request = hookMoveRequest(hookID, machine, expected)
      return hookOf(await client.send(request.method, request.path, request.body, true))
    },
    copy: options.copy ?? ((value) => navigator.clipboard.writeText(value)),
  }
  installed = management
  return () => {
    if (installed === management) installed = null
  }
}

export function scheduleWebhookManagement(): ScheduleWebhookManagement | null {
  return installed
}
