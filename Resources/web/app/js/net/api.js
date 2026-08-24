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

export function useApi(implementation) { api = implementation; }
