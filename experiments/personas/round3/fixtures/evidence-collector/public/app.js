import { DEFAULT_ITEMS, MAX_QTY, MIN_QTY, formatMoney, lineTotal, subtotal, increment, decrement, promoRate, discountFor } from './cart.js';

const STORAGE_KEY = 'cart:v1';

const state = {
  items: load(),
  discount: 0, // cents, set when a promo code is applied
  promoError: '',
};

function load() {
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY));
    if (Array.isArray(saved)) return saved;
  } catch {}
  return structuredClone(DEFAULT_ITEMS);
}

function save() {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(state.items)); } catch {}
}

const $ = (sel) => document.querySelector(sel);

function renderLine(item) {
  const li = document.createElement('li');
  li.className = 'line';
  li.innerHTML = `
    <div class="line-name">${item.name}<span class="sku">${item.sku}</span></div>
    <div class="stepper">
      <button type="button" class="dec" aria-label="Decrease quantity" ${item.qty < MIN_QTY ? 'disabled' : ''}>−</button>
      <output class="qty" aria-live="polite">${item.qty}</output>
      <button type="button" class="inc" aria-label="Increase quantity" ${item.qty > MAX_QTY ? 'disabled' : ''}>+</button>
    </div>
    <div class="line-total">${formatMoney(lineTotal(item))}</div>
    <button type="button" class="remove">Remove</button>`;
  li.querySelector('.inc').addEventListener('click', () => { increment(item); render(); });
  li.querySelector('.dec').addEventListener('click', () => { decrement(item); render(); });
  li.querySelector('.remove').addEventListener('click', () => {
    state.items = state.items.filter((x) => x !== item);
    save();
    render();
  });
  return li;
}

function render() {
  const list = $('#lines');
  list.replaceChildren(...state.items.map(renderLine));
  const empty = state.items.length === 0;
  $('#empty').hidden = !empty;
  $('#summary').classList.toggle('is-hidden', empty);

  const sub = subtotal(state.items);
  $('#subtotal').textContent = formatMoney(sub);
  $('#discount-row').hidden = state.discount === 0;
  $('#discount').textContent = '−' + formatMoney(state.discount);
  $('#total').textContent = formatMoney(sub - state.discount);
  $('#promo-error').textContent = state.promoError;
  $('#promo-error').hidden = !state.promoError;
}

$('#promo-form').addEventListener('submit', (e) => {
  e.preventDefault();
  const rate = promoRate($('#promo').value);
  if (rate === null) {
    state.promoError = 'Code not recognised';
  } else {
    state.discount = discountFor(state.items, rate);
  }
  render();
});

render();
