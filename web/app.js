// Top-level orchestrator: wires DOM events to module functions and starts the app.
import { state } from './ui/state.js';
import { authenticate } from './ui/api.js';
import * as cameras from './ui/cameras.js';
import * as stream from './ui/stream.js';
import * as dashboard from './ui/dashboard.js';
import * as settings from './ui/settings.js';

export function switchView(name) {
	document.querySelectorAll('.view').forEach(v => v.classList.remove('active'));
	document.getElementById('view-' + name).classList.add('active');
	document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.view === name));
	if (name === 'settings') settings.loadGeneralSettings();
}

function wireUI() {
	// Navigation tabs
	document.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => switchView(t.dataset.view)));

	// First-run banner
	document.getElementById('setupAddBtn').addEventListener('click', () => { switchView('settings'); cameras.openAddCameraModal(); });

	// Live stream camera selector
	document.getElementById('streamCamera').addEventListener('change', stream.switchStreamCamera);

	// Export range + actions
	document.getElementById('lifetimeRangeBtn').addEventListener('click', () => dashboard.setRange('lifetime'));
	document.getElementById('customRangeBtn').addEventListener('click', () => dashboard.setRange('custom'));
	document.getElementById('segmentTimezoneToggle').addEventListener('click', dashboard.toggleSegmentTimezone);
	document.getElementById('serverExportBtn').addEventListener('click', dashboard.generateVideo);
	document.getElementById('btnDownloadExport').addEventListener('click', dashboard.downloadExport);
	document.getElementById('btnDeleteExport').addEventListener('click', dashboard.deleteExport);

	// Settings
	document.getElementById('saveGeneralBtn').addEventListener('click', settings.saveGeneralSettings);
	document.getElementById('tokenRevealBtn').addEventListener('click', settings.toggleTokenReveal);
	document.getElementById('btnCopyToken').addEventListener('click', settings.copyToken);
	document.getElementById('btnRegenerateToken').addEventListener('click', settings.regenerateToken);
	document.getElementById('btnAddCamera').addEventListener('click', cameras.openAddCameraModal);

	// Camera modal
	document.getElementById('btnRescan').addEventListener('click', cameras.loadDiscovery);
	document.getElementById('cameraDeviceSelect').addEventListener('change', cameras.onDeviceSelectChange);
	document.getElementById('cameraResolution').addEventListener('change', cameras.onResolutionChange);
	document.getElementById('btnCancelCamera').addEventListener('click', cameras.closeCameraModal);
	document.getElementById('btnCancelCameraX').addEventListener('click', cameras.closeCameraModal);
	document.getElementById('btnSaveCamera').addEventListener('click', cameras.saveCameraConfig);

	// Auth — on success, start the dashboard (authenticate() only closes the modal).
	async function doAuth() { if (await authenticate()) startApp(); }
	document.getElementById('authInput').addEventListener('keydown', e => { if (e.key === 'Enter') doAuth(); });
	document.getElementById('btnAuthSignIn').addEventListener('click', doAuth);

	// Modal close UX: click backdrop or press ESC to close the camera modal.
	const cameraModal = document.getElementById('cameraModal');
	cameraModal.addEventListener('click', e => { if (e.target === cameraModal) cameras.closeCameraModal(); });
	document.addEventListener('keydown', e => {
		if (e.key === 'Escape' && cameraModal.classList.contains('active')) cameras.closeCameraModal();
	});
}

function startApp() {
	dashboard.setRange(state.activeRange);
	dashboard.setSegmentTimezone(state.segmentTimezone);
	dashboard.loadStatus();
	cameras.loadCameras().then(() => stream.startStream());
	dashboard.checkExportStatus();
	dashboard.checkRemuxStatus();
	setInterval(dashboard.loadStatus, 5000);
	setInterval(cameras.loadCameras, 30000);
	setInterval(dashboard.checkExportStatus, 3000);
	setInterval(dashboard.checkRemuxStatus, 3000);
}

wireUI();
if (!state.authToken) document.getElementById('authModal').classList.add('active');
else startApp();