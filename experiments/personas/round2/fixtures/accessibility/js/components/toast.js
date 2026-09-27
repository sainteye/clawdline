const DEFAULT_DURATION_MS = 2000;

let region = null;

function getRegion() {
  if (!region) {
    region = document.getElementById('toast-region');
  }
  return region;
}

export function showToast(message, { duration = DEFAULT_DURATION_MS } = {}) {
  const host = getRegion();
  if (!host) return;

  const toast = document.createElement('div');
  toast.className = 'toast';
  toast.textContent = message;
  host.append(toast);

  window.setTimeout(() => {
    toast.remove();
  }, duration);
}
