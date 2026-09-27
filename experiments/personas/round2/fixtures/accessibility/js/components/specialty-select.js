import { escapeHtml } from '../format.js';

export function createSpecialtySelect({ options, value, onChange }) {
  const root = document.createElement('div');
  root.className = 'select-field';

  const all = [{ id: '', name: 'All specialties' }, ...options];
  let current = all.find((o) => o.id === value) || all[0];
  let open = false;

  root.innerHTML = `
    <span class="field-label">Specialty</span>
    <div class="select">
      <div class="select-trigger">${escapeHtml(current.name)}</div>
      <ul class="select-options" hidden>
        ${all
          .map(
            (o) => `
          <li class="select-option${o.id === current.id ? ' is-selected' : ''}" data-id="${escapeHtml(o.id)}">
            ${escapeHtml(o.name)}
          </li>`,
          )
          .join('')}
      </ul>
    </div>
  `;

  const trigger = root.querySelector('.select-trigger');
  const list = root.querySelector('.select-options');

  function setOpen(next) {
    open = next;
    list.hidden = !open;
  }

  function choose(id) {
    const next = all.find((o) => o.id === id);
    if (!next) return;
    current = next;
    trigger.textContent = next.name;
    for (const item of list.children) {
      item.classList.toggle('is-selected', item.dataset.id === id);
    }
    setOpen(false);
    onChange(next.id);
  }

  trigger.addEventListener('click', () => setOpen(!open));

  list.addEventListener('click', (event) => {
    const item = event.target.closest('.select-option');
    if (item) choose(item.dataset.id);
  });

  function onDocumentClick(event) {
    if (open && !root.contains(event.target)) setOpen(false);
  }
  document.addEventListener('click', onDocumentClick);

  return {
    element: root,
    destroy() {
      document.removeEventListener('click', onDocumentClick);
    },
  };
}
