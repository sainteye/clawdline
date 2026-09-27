const backdrop = () => document.getElementById('confirm-dialog');

let pending = null;

function settle(result) {
  const el = backdrop();
  el.hidden = true;
  document.body.classList.remove('has-dialog');
  if (pending) {
    const { resolve } = pending;
    pending = null;
    resolve(result);
  }
}

function onClick(event) {
  const action = event.target.closest('[data-action]')?.dataset.action;
  if (action === 'confirm') {
    settle(true);
  } else if (action === 'cancel' || event.target === event.currentTarget) {
    settle(false);
  }
}

let wired = false;

function wire() {
  if (wired) return;
  backdrop().addEventListener('click', onClick);
  wired = true;
}

export function confirmHold({ clinicianName, when }) {
  wire();
  if (pending) {
    settle(false);
  }

  const el = backdrop();
  el.querySelector('#confirm-body').textContent =
    `We will hold ${when} with ${clinicianName} for 5 minutes while you enter your details.`;
  el.hidden = false;
  document.body.classList.add('has-dialog');

  return new Promise((resolve) => {
    pending = { resolve };
  });
}
