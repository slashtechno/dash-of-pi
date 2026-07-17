// Shared mutable UI state. Modules import this object and read/write fields on it.
export const state = {
	authToken: localStorage.getItem('authToken'),
	editingCameraId: null,
	activeRange: 'lifetime',
	streamCameraId: null,
	streamInterval: null,
	segmentTimezone: localStorage.getItem('segmentTimezone') || 'local',
	discovered: { devices: [], csi_available: false },
	currentDev: null,
	tokenVisible: false,
	appStartTime: Date.now(),
};

// Allow ?token=... in the URL to log in via a shared link, then strip it from the bar.
const urlParams = new URLSearchParams(window.location.search);
if (urlParams.has('token')) {
	state.authToken = urlParams.get('token');
	localStorage.setItem('authToken', state.authToken);
	window.history.replaceState({}, document.title, window.location.pathname);
}

export function setAuthToken(t) { state.authToken = t; localStorage.setItem('authToken', t); }
export function clearAuthToken() { localStorage.removeItem('authToken'); state.authToken = null; }