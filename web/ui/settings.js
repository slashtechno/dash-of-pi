// Settings view: general config + auth token management.
import { state, setAuthToken } from './state.js';
import { apiCall } from './api.js';
import { notify, confirmDialog } from './ui.js';

export async function loadGeneralSettings() {
	try {
		const cfg = await apiCall('/api/config');
		document.getElementById('cfgStorageCap').value = cfg.storage_cap_gb;
		document.getElementById('cfgSegmentLen').value = cfg.segment_length_s;
		document.getElementById('cfgPort').value = cfg.port;
	} catch (_) {}
	await loadToken();
}

export async function saveGeneralSettings() {
	const payload = {
		storage_cap_gb: parseInt(document.getElementById('cfgStorageCap').value, 10) || 0,
		segment_length_s: parseInt(document.getElementById('cfgSegmentLen').value, 10) || 0,
		port: parseInt(document.getElementById('cfgPort').value, 10) || 0,
	};
	try {
		const res = await apiCall('/api/config/update', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
		const msg = document.getElementById('generalSavedMsg');
		if (res.restart_required) {
			msg.textContent = 'Saved. Port changed — restart the service to apply.';
			notify('Saved. Restart the service to apply the new port.', 'info');
		} else {
			msg.textContent = 'Saved.';
			notify('Settings saved', 'success');
		}
	} catch (err) {
		notify('Failed to save: ' + err.message, 'error');
	}
}

// Token management
export async function loadToken() {
	try {
		const data = await apiCall('/api/auth/token');
		const box = document.getElementById('tokenBox');
		box.dataset.token = data.token;
		box.value = '•'.repeat(Math.min(32, data.token.length));
		state.tokenVisible = false;
		document.getElementById('tokenRevealBtn').textContent = 'Show';
	} catch (_) {}
}

export function toggleTokenReveal() {
	const box = document.getElementById('tokenBox');
	state.tokenVisible = !state.tokenVisible;
	box.value = state.tokenVisible ? box.dataset.token : '•'.repeat(32);
	document.getElementById('tokenRevealBtn').textContent = state.tokenVisible ? 'Hide' : 'Show';
}

export async function copyToken() {
	const box = document.getElementById('tokenBox');
	try { await navigator.clipboard.writeText(box.dataset.token || ''); notify('Token copied', 'success'); }
	catch (_) { box.select(); document.execCommand('copy'); notify('Token copied', 'success'); }
}

export async function regenerateToken() {
	const ok = await confirmDialog({
		title: 'Regenerate auth token',
		message: 'Generate a new auth token? The current token stops working immediately, and other clients will need the new one.',
		confirmText: 'Regenerate',
	});
	if (!ok) return;
	try {
		const data = await apiCall('/api/auth/regenerate-token', { method: 'POST' });
		setAuthToken(data.token);
		await loadToken();
		notify('Token regenerated and saved to this browser', 'success');
	} catch (err) {
		notify('Failed to regenerate: ' + err.message, 'error');
	}
}