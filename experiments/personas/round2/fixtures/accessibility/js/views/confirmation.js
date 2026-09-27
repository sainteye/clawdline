import { escapeHtml, formatSlot } from '../format.js';
import { getState, resetBooking } from '../state.js';

export function renderConfirmation(container, { navigate }) {
  const { booking } = getState();
  if (!booking) {
    navigate('#/');
    return;
  }

  container.innerHTML = `
    <section aria-labelledby="done-heading">
      <h2 id="done-heading" tabindex="-1">You're booked</h2>
      <dl class="summary">
        <dt>Reference</dt>
        <dd>${escapeHtml(booking.reference)}</dd>
        <dt>Clinician</dt>
        <dd>${escapeHtml(booking.clinicianName)}</dd>
        <dt>When</dt>
        <dd>${escapeHtml(formatSlot(booking.slotStart))}</dd>
        <dt>Where</dt>
        <dd>${escapeHtml(booking.location)}</dd>
      </dl>
      <p>We sent a confirmation to ${escapeHtml(booking.patientEmail)}. Please arrive 10 minutes early.</p>
      <button type="button" class="btn btn-secondary" id="book-another">Book another appointment</button>
    </section>
  `;

  container.querySelector('#done-heading').focus();

  container.querySelector('#book-another').addEventListener('click', () => {
    resetBooking();
    navigate('#/');
  });
}
