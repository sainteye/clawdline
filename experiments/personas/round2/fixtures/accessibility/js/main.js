import { renderClinicians } from './views/clinicians.js';
import { renderSlots } from './views/slots.js';
import { renderCheckout } from './views/checkout.js';
import { renderConfirmation } from './views/confirmation.js';
import { startTipsCarousel } from './components/tips-carousel.js';

const routes = [
  { pattern: /^#\/?$/, view: renderClinicians },
  { pattern: /^#\/clinician\/([^/]+)$/, view: renderSlots, params: ['clinicianId'] },
  { pattern: /^#\/checkout$/, view: renderCheckout },
  { pattern: /^#\/done$/, view: renderConfirmation },
];

let viewRoot;
let cleanup = null;
let renderToken = 0;

function match(hash) {
  for (const route of routes) {
    const m = hash.match(route.pattern);
    if (m) {
      const params = {};
      (route.params || []).forEach((name, i) => {
        params[name] = decodeURIComponent(m[i + 1]);
      });
      return { route, params };
    }
  }
  return null;
}

export function navigate(hash) {
  if (location.hash === hash) {
    render();
  } else {
    location.hash = hash;
  }
}

async function render() {
  const token = ++renderToken;
  if (typeof cleanup === 'function') {
    cleanup();
  }
  cleanup = null;

  const hash = location.hash || '#/';
  const found = match(hash);
  const container = document.createElement('div');
  container.className = 'view';
  viewRoot.replaceChildren(container);
  window.scrollTo(0, 0);

  if (!found) {
    container.innerHTML = '<h2>Page not found</h2><p><a href="#/">Go to the clinician list</a></p>';
    return;
  }

  const result = await found.route.view(container, { ...found.params, navigate });
  if (token === renderToken) {
    cleanup = result || null;
  } else if (typeof result === 'function') {
    result();
  }
}

function markCurrentNav() {
  const hash = location.hash || '#/';
  for (const link of document.querySelectorAll('.nav-list a')) {
    const target = link.getAttribute('href');
    if (target === hash || (target === '#/' && hash.startsWith('#/clinician'))) {
      link.setAttribute('aria-current', 'page');
    } else {
      link.removeAttribute('aria-current');
    }
  }
}

function start() {
  viewRoot = document.getElementById('view');
  window.addEventListener('hashchange', () => {
    markCurrentNav();
    render();
  });
  markCurrentNav();
  render();
  startTipsCarousel(document.getElementById('tips-track'));
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', start);
} else {
  start();
}
