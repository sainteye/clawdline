# Bot Webhooks

A Bot Webhook runs a saved routine for a specific Bot when GitHub, CI or another service sends an event. To start a scheduled task from the Session list, use [Schedule webhooks](schedules.md#schedule-webhooks-cloud).

## Ask the Bot to set it up

In the Bot’s chat, tell it what to do when a webhook arrives. For example, “Set up a webhook that asks you to summarize each CI failure I send you.” The Bot saves and enables the routine and reports the public Cloud address when Cloud confirms registration. If registration is still pending, the Bot can check the saved routine again.

## Set up a Bot Webhook in the console

1. Open the Bot’s chat and click its name at the top. In **Details**, find **Routines** and click **+**.
2. Enter a name and what the Bot should do. Under **When to run**, choose **Webhook**. To combine it with other triggers, open **Run settings** → **Advanced triggers** and choose **Webhook** as one of the trigger types.
3. Create the routine and leave it enabled. Reopen the saved routine and choose **Manage webhook key** → **Retrieve key**.

## Find the public Cloud address and key

The panel shows a **Local relative address** for the machine that runs the Bot and a **Webhook key**. For GitHub or another service outside that machine, use the **Public Cloud address**. It appears after Cloud confirms the current routine and key registration. If you only see the local address, check that the routine is enabled and its source machine is online and connected to Clawdline Cloud, then retrieve the key again.

In GitHub’s Webhook settings, use the public address as the **Payload URL**, select `application/json`, and set **Secret** to the Webhook key. A sender that uses bearer authentication sends the key in the `Authorization: Bearer <key>` header. Keep the key in the sender’s secret settings. [GitHub’s repository Webhook instructions](https://docs.github.com/en/webhooks/using-webhooks/creating-webhooks#creating-a-repository-webhook)

Open **Run history and results** in the routine to check whether the event started a run and what the Bot did. An accepted delivery means the event was received. The history shows the run’s outcome.

[繁體中文](bot-webhooks.zh-TW.md)
