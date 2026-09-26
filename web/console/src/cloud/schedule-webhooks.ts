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
  /** `bind` on a named machine: a schedule just created there is not in the list yet. */
  bindOn(machine: string, scheduleID: string, hookID: string, replaceHookID: string | null): Promise<ScheduleWebhookHook>
  /** Cloud's move to `machine`; a null revision is read first. */
  move(hookID: string, machine: string, revision: number | null): Promise<ScheduleWebhookHook>
  copy(value: string): Promise<void>
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
  const management: ScheduleWebhookManagement = {
    client,
    // A schedule listed from another machine is bound on that machine: the
    // binding lives beside the schedule, in the store that fires it.
    machine: (scheduleID) => scheduleOwner(scheduleID) ?? options.machineID,
    bind: (scheduleID, hookID, replaceHookID) =>
      management.bindOn(scheduleOwner(scheduleID) ?? options.machineID, scheduleID, hookID, replaceHookID),
    bindOn: async (machine, scheduleID, hookID, replaceHookID) => {
      const requestID = crypto.randomUUID().toLowerCase()
      await options.connected()._publishCommand(
        machine,
        "schedule-webhook-bind-v1",
        {
          request_id: requestID,
          hook_id: hookID,
          schedule_id: scheduleID,
          replace_hook_id: replaceHookID,
        },
        "ctl",
      )
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
