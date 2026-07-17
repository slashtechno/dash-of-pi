// API + auth. The bearer token lives in state.js so all modules share it.
import { state, setAuthToken, clearAuthToken } from './state.js';

export async function apiCall(url, options = {}) {
	const res = await fetch(url, {
		...options,
		headers: { 'Authorization': 'Bearer ' + state.authToken, ...options.headers },
	});
	if (res.status === 401) {
		clearAuthToken();
		document.getElementById('authModal').classList.add('active');
		throw new Error('Unauthorized');
	}
	if (!res.ok) throw new Error(`API error: ${res.status} ${res.statusText}`);
	return res.json();
}

// authenticate returns true on success and hides the auth modal.
export async function authenticate() {
	const token = document.getElementById('authInput').value.trim();
	if (!token) return false;
	const r = await fetch('/api/status', { headers: { 'Authorization': 'Bearer ' + token } });
	if (r.ok) {
		setAuthToken(token);
		document.getElementById('authModal').classList.remove('active');
		document.getElementById('authError').classList.add('hidden');
		return true;
	}
	const e = document.getElementById('authError');
	e.textContent = 'Invalid token';
	e.classList.remove('hidden');
	return false;
}