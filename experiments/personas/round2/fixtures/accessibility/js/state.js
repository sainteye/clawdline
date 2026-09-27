const STORAGE_KEY = 'linden.booking.v1';

const initial = {
  specialty: '',
  query: '',
  clinician: null,
  weekStart: null,
  hold: null,
  booking: null,
};

let state = load();
const listeners = new Set();

function load() {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...initial };
    const saved = JSON.parse(raw);
    if (saved.hold && Date.parse(saved.hold.expiresAt) <= Date.now()) {
      saved.hold = null;
    }
    return { ...initial, ...saved };
  } catch {
    return { ...initial };
  }
}

function persist() {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch {
    // Storage can be unavailable in private mode; the app still works in memory.
  }
}

export function getState() {
  return state;
}

export function setState(patch) {
  state = { ...state, ...patch };
  persist();
  for (const listener of listeners) {
    listener(state);
  }
}

export function subscribe(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function resetBooking() {
  setState({ clinician: null, weekStart: null, hold: null, booking: null });
}
