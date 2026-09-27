import test from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULT_ITEMS, subtotal, lineTotal, increment, decrement, promoRate, discountFor, formatMoney } from '../public/cart.js';

const fresh = () => structuredClone(DEFAULT_ITEMS);

test('subtotal of the default cart', () => {
  assert.equal(subtotal(fresh()), 12900 + 89900 + 2 * 1250);
});

test('increment and decrement change the quantity', () => {
  const item = { price: 500, qty: 2 };
  increment(item);
  assert.equal(item.qty, 3);
  decrement(item);
  decrement(item);
  assert.equal(item.qty, 1);
  assert.equal(lineTotal(item), 500);
});

test('SAVE10 gives 10 percent off, any case', () => {
  assert.equal(promoRate('SAVE10'), 0.10);
  assert.equal(promoRate(' save10 '), 0.10);
  assert.equal(promoRate('FREESHIP'), null);
  assert.equal(discountFor(fresh(), 0.10), 10530);
});

test('formatMoney shows dollars and cents', () => {
  assert.equal(formatMoney(1250), '$12.50');
  assert.equal(formatMoney(5), '$0.05');
});
