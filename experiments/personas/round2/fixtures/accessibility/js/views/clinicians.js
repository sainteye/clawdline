import { listClinicians, listSpecialties } from '../api.js';
import { escapeHtml } from '../format.js';
import { getState, setState } from '../state.js';
import { createSpecialtySelect } from '../components/specialty-select.js';

const SEARCH_DEBOUNCE_MS = 300;

function availabilityLevel(clinician) {
  if (clinician.openSlotsThisWeek >= 6) return 'high';
  if (clinician.openSlotsThisWeek > 0) return 'low';
  return 'none';
}

function clinicianCard(c) {
  const level = availabilityLevel(c);
  return `
    <li class="clinician">
      <h3>${escapeHtml(c.name)}</h3>
      <a class="btn btn-secondary" href="#/clinician/${encodeURIComponent(c.id)}">
        View times<span class="visually-hidden"> for ${escapeHtml(c.name)}</span>
      </a>
      <p class="clinician-meta">
        <span class="availability availability--${level}"></span>${escapeHtml(c.specialtyName)} · ${escapeHtml(c.location)}
      </p>
    </li>
  `;
}

export async function renderClinicians(container) {
  const { specialty, query } = getState();

  container.innerHTML = `
    <section aria-labelledby="find-heading">
      <h2 id="find-heading" class="visually-hidden">Find a clinician</h2>
      <div class="filters">
        <div>
          <label class="field-label" for="clinician-search">Search by name</label>
          <input id="clinician-search" type="search" tabindex="1" autocomplete="off" value="${escapeHtml(query)}">
        </div>
        <div id="specialty-slot"></div>
      </div>
      <p class="legend">
        <span class="availability availability--high"></span>Many times this week
        <span class="availability availability--low"></span>A few times
        <span class="availability availability--none"></span>Fully booked
      </p>
      <p id="results-count" class="results-count" aria-live="polite"></p>
      <ul class="clinician-list" id="clinician-list"></ul>
    </section>
  `;

  const list = container.querySelector('#clinician-list');
  const count = container.querySelector('#results-count');
  const search = container.querySelector('#clinician-search');

  let requestId = 0;
  async function refresh() {
    const id = ++requestId;
    const { specialty: s, query: q } = getState();
    try {
      const clinicians = await listClinicians({ specialty: s, query: q });
      if (id !== requestId) return;
      list.innerHTML = clinicians.map(clinicianCard).join('');
      count.textContent =
        clinicians.length === 1 ? '1 clinician found' : `${clinicians.length} clinicians found`;
    } catch (err) {
      if (id !== requestId) return;
      list.innerHTML = '';
      count.textContent = err.message;
    }
  }

  let debounce = 0;
  search.addEventListener('input', () => {
    window.clearTimeout(debounce);
    debounce = window.setTimeout(() => {
      setState({ query: search.value.trim() });
      refresh();
    }, SEARCH_DEBOUNCE_MS);
  });

  let select = null;
  try {
    const specialties = await listSpecialties();
    select = createSpecialtySelect({
      options: specialties,
      value: specialty,
      onChange(id) {
        setState({ specialty: id });
        refresh();
      },
    });
    container.querySelector('#specialty-slot').append(select.element);
  } catch {
    // The filter is optional; the list still loads without it.
  }

  await refresh();

  return () => {
    window.clearTimeout(debounce);
    select?.destroy();
  };
}
