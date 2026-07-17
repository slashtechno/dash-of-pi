// Pure helpers: HTML escaping, notifications, downloads, formatting.
export function esc(val) {
	return String(val)
		.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

export function notify(message, type = 'info') {
	const c = document.getElementById('toastContainer');
	if (!c) return;
	const t = document.createElement('div');
	t.className = `toast toast-${type}`;
	t.textContent = message;
	c.appendChild(t);
	setTimeout(() => t.remove(), type === 'error' ? 6000 : 4000);
}

export function triggerDownload(url, filename) {
	const a = document.createElement('a');
	a.href = url; a.download = filename; a.click();
}

export function formatUptime(ms) {
	const s = Math.floor(ms / 1000), m = Math.floor(s / 60), h = Math.floor(m / 60), d = Math.floor(h / 24);
	if (d > 0) return `${d}d ${h % 24}h`;
	if (h > 0) return `${h}h ${m % 60}m`;
	if (m > 0) return `${m}m ${s % 60}s`;
	return `${s}s`;
}

export function formatDuration(seconds) {
	const h = Math.floor(seconds / 3600), m = Math.floor((seconds % 3600) / 60), s = seconds % 60;
	if (h > 0) return `${h}h ${m}m`;
	if (m > 0) return `${m}m ${s}s`;
	return `${s}s`;
}

export function formatBytes(b) {
	if (b >= 1e9) return (b / 1e9).toFixed(2) + ' GB';
	if (b >= 1e6) return (b / 1e6).toFixed(1) + ' MB';
	return Math.round(b / 1e3) + ' KB';
}

export function utcString(iso) {
	return new Date(iso).toLocaleString('en-US', { timeZone: 'UTC', timeZoneName: 'short' });
}

export function getLocalTimeZone() { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'Local'; }

export function formatTimestamp(iso, mode) {
	const d = new Date(iso);
	return d.toLocaleString('en-US', mode === 'utc'
		? { timeZone: 'UTC', timeZoneName: 'short' }
		: { timeZoneName: 'short' });
}

export function formatTimeRange(a, b, mode) { return `${formatTimestamp(a, mode)} to ${formatTimestamp(b, mode)}`; }

// Branded confirmation modal. Returns a Promise that resolves true/false.
// Replaces native confirm() with something on-theme; handles backdrop click + ESC.
export function confirmDialog({ title = 'Confirm', message = '', confirmText = 'Confirm', cancelText = 'Cancel', danger = true } = {}) {
	return new Promise(resolve => {
		const modal = document.getElementById('confirmModal');
		if (!modal) { resolve(window.confirm(message)); return; }
		document.getElementById('confirmTitle').textContent = title;
		document.getElementById('confirmMessage').textContent = message;
		const ok = document.getElementById('confirmOk');
		const cancel = document.getElementById('confirmCancel');
		ok.textContent = confirmText;
		ok.className = danger ? 'btn-danger' : 'btn-primary';
		modal.classList.add('active');

		const done = (val) => {
			ok.removeEventListener('click', onOk);
			cancel.removeEventListener('click', onCancel);
			modal.removeEventListener('click', onBackdrop);
			document.removeEventListener('keydown', onKey);
			modal.classList.remove('active');
			resolve(val);
		};
		const onOk = () => done(true);
		const onCancel = () => done(false);
		const onBackdrop = (e) => { if (e.target === modal) done(false); };
		const onKey = (e) => { if (e.key === 'Escape') done(false); };
		ok.addEventListener('click', onOk);
		cancel.addEventListener('click', onCancel);
		modal.addEventListener('click', onBackdrop);
		document.addEventListener('keydown', onKey);
		setTimeout(() => ok.focus(), 60);
	});
}