/**
 * The one narrow view this feature promises: turns authored by the person, newest first. A
 * message that is still travelling to the Mac belongs at the top too; `Optimistic` has already
 * removed it by the time the persisted copy appears.
 */
export function userMessageEntries(entries, pending) {
    var saved = Array.isArray(entries) ? entries : [];
    var tail = Array.isArray(pending) ? pending : [];
    return saved.concat(tail).filter(function (entry) {
        return !!entry && entry.role === "user";
    }).reverse();
}

/**
 * The row occupied by one of those exact entry objects in the transcript. Text and timestamps
 * are deliberately not identities: a person can send the same sentence twice, including twice
 * in the same second, and picking either one must return to the turn they actually picked.
 */
export function userMessagePosition(entries, pending, selected, newestFirst) {
    var saved = Array.isArray(entries) ? entries : [];
    var tail = Array.isArray(pending) ? pending : [];
    var messages = saved.concat(tail).filter(function (entry) {
        return !!entry && entry.role === "user";
    });
    var position = messages.indexOf(selected);
    if (position < 0) return -1;
    return newestFirst ? messages.length - position - 1 : position;
}

/**
 * The words needed before the server's translated string payload has arrived. Traditional and
 * Simplified Chinese cover the request that introduced the view; English is the fallback for
 * every other locale.
 */
export function copyForUserMessages(language) {
    var lang = String(language || "").toLowerCase();
    if (lang.indexOf("zh-hant") === 0 || lang.indexOf("zh-tw") === 0 ||
        lang.indexOf("zh-hk") === 0 || lang.indexOf("zh-mo") === 0) {
        return {
            title: "我傳出的訊息",
            empty: "你還沒有在這個 session 傳出訊息。"
        };
    }
    if (lang.indexOf("zh") === 0) {
        return {
            title: "我发出的消息",
            empty: "你还没有在这个 session 发出消息。"
        };
    }
    return {
        title: "My messages",
        empty: "You have not sent a message in this session yet."
    };
}
