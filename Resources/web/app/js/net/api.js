import { LOCAL_MACHINE, sessionIdentity } from "./client.js";
import { jsonFetch, post } from "./fetch.js";
import { uuid } from "../core/util.js";

/* --------------------------------------------------------------------------
   The API, as a name
   There is one API on this page and it is either the real one or the fixtures.
   What decides that — `MOCK` — is at one end of the page and the thirty-odd
   places that call it are at the other, with the whole render layer in between.
   So this file holds the name and nothing else. The entry point puts the
   implementation in before anything can call it, and an exported `let` is a live
   binding: every importer reads what was put in, not the null it started as.

   Not a fixed object forwarding the methods, either. `Mock` is missing `pushKey`,
   `places` and `startPlace` on purpose, and six places ask `typeof api.X !==
   "function"` to find out whether this mode has that at all.
   -------------------------------------------------------------------------- */
export let api = null;
// The new name is an alias of the same live binding. Existing call sites can
// migrate one module at a time without creating a second selected transport.
export { api as client };

export function useClient(implementation) {
    api = implementation;
    // This card remains a local HTTP surface. Keep the write here so the small transport seam
    // does not grow a fleet command until the cloud protocol has a matching typed operation.
    if (typeof api.title !== "function") {
        api.title = function (value, title) {
            var identity = sessionIdentity(value, LOCAL_MACHINE);
            if (identity.machine !== LOCAL_MACHINE) {
                return Promise.reject(Object.assign(
                    new Error("Session titles are not available through the cloud transport."),
                    { code: "unsupported" }));
            }
            return jsonFetch("/v1/sessions/" + encodeURIComponent(identity.session) + "/title",
                             post({ title: title }, { "Idempotency-Key": uuid() }));
        };
    }
}
export function useApi(implementation) { useClient(implementation); }
