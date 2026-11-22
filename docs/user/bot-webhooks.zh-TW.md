# Bot Webhook

GitHub、CI 或其他服務送出事件時，可以用 Bot Webhook 觸發指定 Bot 的例行任務。若要從工作階段清單啟動排程任務，請使用[一般排程的 Webhook](schedules.md#schedule-webhooks-cloud)。

## 請 Bot 設定

在 Bot 對話中說明收到 Webhook 時要做什麼。例如「幫我設定一個 Webhook，每次收到我送來的 CI 失敗事件，就整理失敗原因」。Bot 會儲存並啟用例行任務，等 Cloud 確認登錄後回報公開位址。如果登錄還沒完成，可以請 Bot 再查一次已儲存的任務。

## 在介面中設定 Bot Webhook

1. 開啟 Bot 對話，點上方的 Bot 名稱。在「詳情」找到「例行任務」，點「＋」。
2. 輸入名稱與要交給 Bot 的工作，在「何時執行」選「Webhook」。若要搭配其他觸發條件，開啟「執行設定 → 進階觸發條件」，將「Webhook」加入其中。
3. 建立例行任務並保持啟用。重新開啟已儲存的任務，選「管理 Webhook 金鑰 → 取得金鑰」。

## 取得公開 Cloud 位址與金鑰

面板會顯示 Bot 來源機器的「本機相對位址」及「Webhook 金鑰」。GitHub 或其他外部服務請使用「公開 Cloud 位址」，Cloud 確認目前的例行任務與金鑰登錄後才會顯示。如果只看到本機位址，請確認任務已啟用、來源機器在線且已連線至 Clawdline Cloud，再取得一次金鑰。

在 GitHub 的 Webhook 設定中，把公開位址填入 **Payload URL**，選擇 `application/json`，再把 Webhook 金鑰填入 **Secret**。使用 Bearer 驗證的發送端，請在 `Authorization: Bearer <key>` 標頭帶上金鑰。請把金鑰存放在發送端的密鑰設定中。 [GitHub 的存放庫 Webhook 設定說明](https://docs.github.com/en/webhooks/using-webhooks/creating-webhooks#creating-a-repository-webhook)

要確認事件是否觸發 Bot，以及 Bot 執行了什麼，請開啟該任務的「執行紀錄與結果」。傳遞獲接受代表事件已收到，執行結果以紀錄為準。

[English](bot-webhooks.md)
