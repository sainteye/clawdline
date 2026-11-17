# A real-console website demo

The trial uses the production `web/console/src/main.tsx` entry, including its normal App,
Session list, conversation, composer and CSS. The separate `web/console/demo/main.ts` installs
an in-memory data adapter before importing that entry. Normal daemon and hosted builds do not
import the adapter.

Build it from the product checkout:

```sh
cd web
npm run build:demo --workspace @clawdline/console
```

The output is `web/console/dist/demo/`. Copy that directory into the marketing site's
`public/demo/console/` and embed `/demo/console/?lang=en` or `?lang=zh-Hant`. The trial marketing
component offers desktop and phone viewport sizes and restarts by reloading its iframe.
Rebuild from the intended product revision whenever updating the demo; retain that revision
alongside the marketing preview. The ordinary console build may replace `dist`, so export the
demo output before running that build.

The adapter supplies three invented shop Sessions and their conversations. Sending a message
or choosing a menu option changes this document's in-memory story only. Unknown operations
return `demo_unavailable`; requests never fall through to a real service. The standalone demo
also uses `connect-src 'none'`. Its localStorage and sessionStorage implementations are local
to the iframe document, so it does not read or reset the visitor's saved console data.

This is a reviewable prototype, not a production backend or a deployed homepage. The full
navigation remains the real product navigation; operations outside the demonstrated flow
are unavailable. Visual evidence must come from this built entry in a browser, including the
embedded layout and a phone viewport. A bundle build alone does not prove those views work.
