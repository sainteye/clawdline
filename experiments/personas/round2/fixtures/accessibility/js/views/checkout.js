import { createBooking, releaseHold } from '../api.js';
import { escapeHtml, formatCountdown, formatSlot } from '../format.js';
import { getState, setState } from '../state.js';
import { showToast } from '../components/toast.js';

const PHONE_RE = /^[+()\d\s-]{7,20}$/;
const EMAIL_RE = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;

const FIELDS = [
  { name: 'fullName', label: 'Full name', type: 'text', autocomplete: 'name', required: true },
  { name: 'dateOfBirth', label: 'Date of birth', type: 'date', autocomplete: 'bday', required: true },
  { name: 'phone', label: 'Mobile phone', type: 'tel', autocomplete: 'tel', required: true },
  { name: 'email', label: 'Email', type: 'email', autocomplete: 'email', required: true },
];

function validate(values) {
  const errors = {};
  if (!values.fullName.trim()) errors.fullName = 'Enter your full name.';
  if (!values.dateOfBirth) {
    errors.dateOfBirth = 'Enter your date of birth.';
  } else if (new Date(values.dateOfBirth) > new Date()) {
    errors.dateOfBirth = 'Date of birth cannot be in the future.';
  }
  if (!PHONE_RE.test(values.phone.trim())) errors.phone = 'Enter a phone number we can text, for example 555-010-0200.';
  if (!EMAIL_RE.test(values.email.trim())) errors.email = 'Enter an email address like name@example.com.';
  if (values.reason.length > 500) errors.reason = 'Keep the reason under 500 characters.';
  if (!values.consent) errors.consent = 'You need to accept the clinic policy to book.';
  return errors;
}

function fieldMarkup(f) {
  return `
    <div class="field" data-field="${f.name}">
      <label for="f-${f.name}">${escapeHtml(f.label)}</label>
      <input id="f-${f.name}" name="${f.name}" type="${f.type}" autocomplete="${f.autocomplete}"${f.required ? ' required' : ''}>
    </div>`;
}

function clearErrors(form) {
  for (const el of form.querySelectorAll('.field-error')) el.remove();
  for (const el of form.querySelectorAll('.invalid')) el.classList.remove('invalid');
}

function showErrors(form, errors) {
  for (const [name, message] of Object.entries(errors)) {
    const wrapper = form.querySelector(`[data-field="${name}"]`);
    const input = wrapper.querySelector('input, textarea');
    input.classList.add('invalid');
    const p = document.createElement('p');
    p.className = 'field-error';
    p.textContent = message;
    wrapper.append(p);
  }
  const first = form.querySelector('.invalid');
  first?.focus();
}

export function renderCheckout(container, { navigate }) {
  const { hold, clinician } = getState();
  if (!hold || !clinician) {
    navigate('#/');
    return;
  }

  container.innerHTML = `
    <section aria-labelledby="checkout-heading">
      <h2 id="checkout-heading">Your details</h2>
      <p>${escapeHtml(clinician.name)}, ${escapeHtml(formatSlot(hold.slotStart))}</p>
      <p class="hold-timer">Time held for you: <span id="hold-remaining"></span></p>
      <form id="checkout-form" class="form-grid" novalidate>
        ${FIELDS.map(fieldMarkup).join('')}
        <div class="field" data-field="reason">
          <label for="f-reason">Reason for visit <span class="optional">(optional)</span></label>
          <textarea id="f-reason" name="reason" rows="4"></textarea>
        </div>
        <div class="field" data-field="consent">
          <input id="f-consent" name="consent" type="checkbox">
          <label for="f-consent">I have read the <a href="/policy" target="_blank" rel="noopener">clinic policy</a> (opens in a new tab).</label>
        </div>
        <div id="checkout-status" class="form-status"></div>
        <div class="form-actions">
          <button type="submit" class="btn btn-primary">Book appointment</button>
          <button type="button" class="btn btn-secondary" id="cancel-hold">Cancel</button>
        </div>
      </form>
    </section>
  `;

  const form = container.querySelector('#checkout-form');
  const status = container.querySelector('#checkout-status');
  const remaining = container.querySelector('#hold-remaining');
  const submitButton = form.querySelector('[type="submit"]');

  const expiresAt = Date.parse(hold.expiresAt);
  function tick() {
    const left = expiresAt - Date.now();
    remaining.textContent = formatCountdown(left);
    if (left <= 0) {
      window.clearInterval(timer);
      setState({ hold: null });
      showToast('Your held time expired. Please choose a time again.');
      navigate(`#/clinician/${encodeURIComponent(clinician.id)}`);
    }
  }
  const timer = window.setInterval(tick, 1000);
  tick();

  container.querySelector('#cancel-hold').addEventListener('click', async () => {
    window.clearInterval(timer);
    try {
      await releaseHold(hold.id);
    } catch {
      // The hold expires on its own; nothing else to do.
    }
    setState({ hold: null });
    navigate(`#/clinician/${encodeURIComponent(clinician.id)}`);
  });

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    clearErrors(form);
    status.textContent = '';
    status.classList.remove('error');

    const data = new FormData(form);
    const values = {
      fullName: data.get('fullName') || '',
      dateOfBirth: data.get('dateOfBirth') || '',
      phone: data.get('phone') || '',
      email: data.get('email') || '',
      reason: data.get('reason') || '',
      consent: data.get('consent') === 'on',
    };

    const errors = validate(values);
    if (Object.keys(errors).length > 0) {
      showErrors(form, errors);
      return;
    }

    submitButton.disabled = true;
    submitButton.textContent = 'Booking…';
    try {
      const booking = await createBooking({ holdId: hold.id, patient: values });
      window.clearInterval(timer);
      setState({ hold: null, booking });
      showToast(`Booked. Reference ${booking.reference}.`);
      navigate('#/done');
    } catch (err) {
      status.textContent = err.message;
      status.classList.add('error');
    } finally {
      submitButton.disabled = false;
      submitButton.textContent = 'Book appointment';
    }
  });

  return () => window.clearInterval(timer);
}
