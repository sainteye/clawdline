// The half of the console that is not a screen.
//
// Everything here runs unchanged in a browser and in React Native: no DOM, no
// React, no bundler-specific import. What a host must supply is small and
// explicit — fetch, and a stream transport if it has one — so a second front
// end costs a view layer rather than a second implementation of the rules.
export * from "./client.js"
export * from "./fleet.js"
export * from "./refusal.js"
export * from "./routes.js"
export * from "./stream.js"
