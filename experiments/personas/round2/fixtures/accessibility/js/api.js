const BASE = '/api/v1';
const DEFAULT_TIMEOUT_MS = 10000;

export class ApiError extends Error {
  constructor(message, status, details) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.details = details;
  }
}

async function request(path, { method = 'GET', body, timeout = DEFAULT_TIMEOUT_MS } = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);

  let response;
  try {
    response = await fetch(`${BASE}${path}`, {
      method,
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
      credentials: 'same-origin',
      signal: controller.signal,
    });
  } catch (err) {
    if (err.name === 'AbortError') {
      throw new ApiError('The request took too long. Please try again.', 0);
    }
    throw new ApiError('We could not reach the clinic. Check your connection and try again.', 0);
  } finally {
    clearTimeout(timer);
  }

  if (response.status === 204) {
    return null;
  }

  let payload = null;
  const type = response.headers.get('Content-Type') || '';
  if (type.includes('application/json')) {
    payload = await response.json();
  }

  if (!response.ok) {
    const message = payload?.error?.message || `Request failed (${response.status}).`;
    throw new ApiError(message, response.status, payload?.error?.details);
  }
  return payload;
}

export function listSpecialties() {
  return request('/specialties');
}

export function listClinicians({ specialty, query } = {}) {
  const params = new URLSearchParams();
  if (specialty) params.set('specialty', specialty);
  if (query) params.set('q', query);
  const qs = params.toString();
  return request(`/clinicians${qs ? `?${qs}` : ''}`);
}

export function getClinician(id) {
  return request(`/clinicians/${encodeURIComponent(id)}`);
}

export function listSlots(clinicianId, weekStart) {
  const params = new URLSearchParams({ week: weekStart });
  return request(`/clinicians/${encodeURIComponent(clinicianId)}/slots?${params}`);
}

export function holdSlot(slotId) {
  return request('/holds', { method: 'POST', body: { slotId } });
}

export function releaseHold(holdId) {
  return request(`/holds/${encodeURIComponent(holdId)}`, { method: 'DELETE' });
}

export function createBooking({ holdId, patient }) {
  return request('/bookings', { method: 'POST', body: { holdId, patient } });
}
