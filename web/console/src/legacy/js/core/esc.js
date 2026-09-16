/* --------------------------------------------------------------------------
   Escaping
   One function, in a file of its own. Everything on this page writes markup, so
   everything needs it — including the words next door, which is the reason it is
   not in `util.js` with the other helpers: `util.js` reads `T`, and the escaping
   the language file needs cannot be downstream of the language file.
   -------------------------------------------------------------------------- */
export function esc(s) {
    return String(s == null ? "" : s)
        .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}
