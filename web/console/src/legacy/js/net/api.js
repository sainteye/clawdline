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

   **And nothing is added to what was handed in.** `title` was, for one release:
   it was defined here and grafted onto whichever implementation arrived, so the
   fixtures grew a method that went straight to the network and the offline flow
   answered a rename with a static file server's `Unsupported method ('POST')`.
   A transport that cannot do something says so in its own words — see the local
   client, the fixtures and the cloud client, each of which now carries its own
   `title`.
   -------------------------------------------------------------------------- */
import { SessionSelection } from "../session/selection.js";

export let api = null;
var selectedTransportIdentity = null;
// The new name is an alias of the same live binding. Existing call sites can
// migrate one module at a time without creating a second selected transport.
export { api as client };

function transportIdentity(implementation) {
    if (implementation && typeof implementation.selectionTransportIdentity === "string" &&
        implementation.selectionTransportIdentity) return implementation.selectionTransportIdentity;
    return implementation;
}

export function useClient(implementation) {
    var nextIdentity = transportIdentity(implementation);
    // A Cloud token rotation replaces the socket/client object while retaining the authenticated
    // account, viewer device and relay route. That is credential maintenance, not a selection
    // boundary. A different local/mock/Cloud kind or Cloud route still invalidates every effect.
    if (api !== null && selectedTransportIdentity !== nextIdentity) {
        SessionSelection.replaceTransport();
    }
    api = implementation;
    selectedTransportIdentity = nextIdentity;
}
export function useApi(implementation) { useClient(implementation); }
