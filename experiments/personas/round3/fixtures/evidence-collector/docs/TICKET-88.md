# TICKET-88: Editable cart page

The cart page today is a static list. This ticket makes it editable.

## Acceptance criteria

1. **Quantity stepper.** Each line has − and + buttons. The quantity stays between 1 and 10:
   − is disabled at 1 and + is disabled at 10. The line total and the order totals update
   immediately.
2. **Remove and empty state.** "Remove" deletes a line. Removing the last line shows "Your
   cart is empty" and hides the whole order summary, including the Checkout button.
3. **Promo code.** `SAVE10` (case-insensitive) takes 10 % off the subtotal, and the discount
   follows later quantity changes. An unknown code shows "Code not recognised" under the field;
   applying a valid code removes that message.
4. **Money format.** Every amount is shown in US dollars with a thousands separator and two
   decimals, e.g. `$1,234.50`.
5. **Layout.** At widths of 768 px and above, the summary sits to the right of the item list;
   below 768 px it is stacked under the list. At 375 px nothing scrolls horizontally and the
   Checkout button is fully visible.
6. **Persistence.** After a reload the cart shows the same items and quantities.

Out of scope: the checkout page itself (the Checkout link goes to `/checkout`, which TICKET-90
builds), taxes and shipping.
