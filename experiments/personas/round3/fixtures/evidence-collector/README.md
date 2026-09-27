# shop-cart

The cart page of a small shop front end: plain HTML, CSS and ES modules, no build step.

```sh
npm start                 # http://127.0.0.1:4173/  (PORT=... to change)
npm test                  # unit tests for public/cart.js (node --test)
node scripts/shot.mjs http://127.0.0.1:4173/ 375 cart-375.png   # full-page screenshot
```

`playwright-core` is installed in `node_modules` and a Chromium headless shell is installed on
this machine, so `scripts/shot.mjs` (or your own Playwright script) works offline.

Code: `public/cart.js` (pure cart logic, prices in integer cents), `public/app.js` (DOM),
`public/cart.css`. Tickets are in `docs/`.
