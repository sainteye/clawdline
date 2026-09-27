import { getClinician, holdSlot, listSlots } from '../api.js';
import {
  addDays,
  escapeHtml,
  formatDay,
  formatShortDay,
  formatSlot,
  formatTime,
  startOfWeek,
  toIsoDate,
} from '../format.js';
import { getState, setState } from '../state.js';
import { confirmHold } from '../components/confirm-dialog.js';
import { showToast } from '../components/toast.js';

function groupByDay(slots) {
  const days = new Map();
  for (const slot of slots) {
    const day = slot.start.slice(0, 10);
    if (!days.has(day)) days.set(day, []);
    days.get(day).push(slot);
  }
  return days;
}

function dayColumn(day, slots) {
  const buttons = slots
    .map(
      (slot) => `
      <li>
        <button type="button" data-slot="${escapeHtml(slot.id)}" data-start="${escapeHtml(slot.start)}">
          ${escapeHtml(formatTime(slot.start))}
        </button>
      </li>`,
    )
    .join('');
  return `
    <div class="slot-day">
      <h3>${escapeHtml(formatDay(day))}</h3>
      <ul class="slot-list">${buttons}</ul>
    </div>
  `;
}

export async function renderSlots(container, { clinicianId }) {
  const state = getState();
  let weekStart = state.weekStart ? new Date(state.weekStart) : startOfWeek(new Date());

  container.innerHTML = `<p>Loading available times…</p>`;

  let clinician;
  try {
    clinician = await getClinician(clinicianId);
  } catch (err) {
    container.innerHTML = `<p>${escapeHtml(err.message)}</p><p><a href="#/">Back to clinicians</a></p>`;
    return;
  }
  setState({ clinician: { id: clinician.id, name: clinician.name } });

  container.innerHTML = `
    <section aria-labelledby="slots-heading">
      <h2 id="slots-heading">${escapeHtml(clinician.name)}</h2>
      <p class="clinician-meta">${escapeHtml(clinician.specialtyName)} · ${escapeHtml(clinician.location)}</p>
      <div class="week-nav">
        <button type="button" class="btn btn-secondary" data-week="-1">Previous week</button>
        <p id="week-label" aria-live="polite"></p>
        <button type="button" class="btn btn-secondary" data-week="1">Next week</button>
      </div>
      <div id="slot-days" class="slot-days"></div>
    </section>
  `;

  const daysEl = container.querySelector('#slot-days');
  const weekLabel = container.querySelector('#week-label');
  const prevButton = container.querySelector('[data-week="-1"]');

  async function loadWeek() {
    const iso = toIsoDate(weekStart);
    setState({ weekStart: iso });
    weekLabel.textContent = `Week of ${formatShortDay(iso)}`;
    prevButton.disabled = weekStart <= startOfWeek(new Date());
    daysEl.innerHTML = '<p>Loading…</p>';
    try {
      const slots = await listSlots(clinician.id, iso);
      if (slots.length === 0) {
        daysEl.innerHTML = '<p>No open times this week. Try the next week.</p>';
        return;
      }
      const days = groupByDay(slots);
      daysEl.innerHTML = Array.from(days, ([day, list]) => dayColumn(day, list)).join('');
    } catch (err) {
      daysEl.innerHTML = `<p>${escapeHtml(err.message)}</p>`;
    }
  }

  container.addEventListener('click', async (event) => {
    const weekButton = event.target.closest('[data-week]');
    if (weekButton) {
      weekStart = addDays(weekStart, 7 * Number(weekButton.dataset.week));
      loadWeek();
      return;
    }

    const slotButton = event.target.closest('[data-slot]');
    if (!slotButton) return;

    for (const b of daysEl.querySelectorAll('.is-selected')) b.classList.remove('is-selected');
    slotButton.classList.add('is-selected');

    const ok = await confirmHold({
      clinicianName: clinician.name,
      when: formatSlot(slotButton.dataset.start),
    });
    if (!ok) {
      slotButton.classList.remove('is-selected');
      return;
    }

    try {
      const hold = await holdSlot(slotButton.dataset.slot);
      setState({ hold: { id: hold.id, slotStart: hold.slotStart, expiresAt: hold.expiresAt } });
      location.hash = '#/checkout';
    } catch (err) {
      showToast(err.status === 409 ? 'Someone just took that time. Please pick another.' : err.message);
      slotButton.classList.remove('is-selected');
      loadWeek();
    }
  });

  await loadWeek();
}
