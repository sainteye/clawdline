const english = {
    button: "Run now",
    running: "Starting…",
    confirm: "Run {title} now? This starts real work on your Mac. If a scheduled occurrence is currently due, this run counts as that occurrence.",
    accepted: "Run accepted. Refreshing its execution history…",
    active: "This schedule already has a run in progress.",
    spent: "This one-time schedule has already run. Create a new schedule to run it again.",
    dispatchOff: "Task dispatch is switched off. Turn on Settings → Remote → Agent tasks on the Mac.",
    writeOff: "Remote writes are switched off. Turn on Settings → Remote on the Mac.",
    gone: "This schedule is no longer on the Mac.",
    failed: "The schedule could not be started."
};

const traditional = {
    button: "立即執行",
    running: "正在啟動…",
    confirm: "要立即執行「{title}」嗎？這會在你的 Mac 啟動真實工作。若目前已有到期的排程時段，這次執行會算作該次排程。",
    accepted: "已接受立即執行；正在重新讀取執行紀錄…",
    active: "此排程已有一個執行中的工作。",
    spent: "這個單次排程已經執行過；若要再次執行，請建立新排程。",
    dispatchOff: "工作派送已關閉；請在 Mac 的「設定 → 遠端 → Agent 工作」開啟。",
    writeOff: "遠端寫入已關閉；請在 Mac 的「設定 → 遠端」開啟。",
    gone: "Mac 上已經沒有這個排程。",
    failed: "無法啟動這個排程。"
};

export function scheduleRunCopy(language) {
    var normalized = String(language || "").toLowerCase();
    return normalized === "zh-hant" || normalized.indexOf("zh-tw") === 0
        || normalized.indexOf("zh-hk") === 0 ? traditional : english;
}

export function scheduleRunMessage(error, language) {
    var words = scheduleRunCopy(language);
    if (error && error.code === "schedule_active") return words.active;
    if (error && error.code === "schedule_spent") return words.spent;
    if (error && error.code === "orchestrator_disabled") return words.dispatchOff;
    if (error && error.code === "write_disabled") return words.writeOff;
    if (error && error.code === "not_found") return words.gone;
    return words.failed;
}

export function scheduleRunConfirmation(title, language) {
    return scheduleRunCopy(language).confirm.replace("{title}", String(title || "Schedule"));
}
