// Pure cart logic, shared by the page (app.js) and the unit tests. Prices are integer cents.

export const MAX_QTY = 10;
export const MIN_QTY = 1;
export const PROMOS = { SAVE10: 0.10 };

export const DEFAULT_ITEMS = [
  { sku: 'KB-200', name: 'Ergonomic keyboard', price: 12900, qty: 1 },
  { sku: 'MN-27', name: '27-inch monitor', price: 89900, qty: 1 },
  { sku: 'CB-C2', name: 'USB-C cable (2 m)', price: 1250, qty: 2 },
];

export function formatMoney(cents) {
  return '$' + (cents / 100).toFixed(2);
}

export function lineTotal(item) {
  return item.price * item.qty;
}

export function subtotal(items) {
  return items.reduce((s, it) => s + lineTotal(it), 0);
}

export function increment(item) {
  if (item.qty <= MAX_QTY) item.qty += 1;
  return item;
}

export function decrement(item) {
  if (item.qty >= MIN_QTY) item.qty -= 1;
  return item;
}

// Returns the promo rate for a code, or null if the code is unknown. Codes are case-insensitive.
export function promoRate(code) {
  const rate = PROMOS[String(code).trim().toUpperCase()];
  return rate === undefined ? null : rate;
}

export function discountFor(items, rate) {
  return Math.round(subtotal(items) * rate);
}
