import {
  ScheduleWebhookClient,
  type ScheduleWebhookClientAPI,
  type ScheduleWebhookHook,
} from "../legacy/schedules-bridge.js"

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
    machine: () => options.machineID,
    bind: async (scheduleID, hookID, replaceHookID) => {
      const requestID = crypto.randomUUID().toLowerCase()
      await options.connected()._publishCommand(
        options.machineID,
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
        const answer = await client.read(hookID)
        const current = "hook" in answer && answer.hook ? answer.hook : (answer as ScheduleWebhookHook)
        if (current.state === "active") return current
        await new Promise((resolve) => setTimeout(resolve, 250))
      }
      throw new Error("webhook activation not observed")
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
