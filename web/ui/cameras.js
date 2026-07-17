// Camera discovery, type-aware add/edit form, and camera CRUD.
import { state } from './state.js';
import { apiCall } from './api.js';
import { esc, notify, confirmDialog } from './ui.js';

export async function loadDiscovery() {
	const sel = document.getElementById('cameraDeviceSelect');
	sel.innerHTML = '<option value="">Scanning…</option>';
	try {
		state.discovered = await apiCall('/api/cameras/discover');
		buildDeviceSelect();
	} catch (err) {
		sel.innerHTML = '<option value="custom">Discovery failed — type path manually</option>';
		notify('Camera discovery failed: ' + err.message, 'error');
	}
}

function buildDeviceSelect() {
	const sel = document.getElementById('cameraDeviceSelect');
	const opts = ['<option value="">Select a camera…</option>'];
	state.discovered.devices.forEach((d, i) => {
		const tag = d.type === 'usb' ? 'USB' : d.type === 'csi' ? 'CSI' : 'V4L2';
		opts.push(`<option value="dev:${i}">${esc(d.name || d.device)} (${tag} · ${esc(d.device)})</option>`);
	});
	if (state.discovered.csi_available) opts.push(`<option value="csi">Raspberry Pi CSI Camera (libcamera)</option>`);
	opts.push(`<option value="custom">Custom — type device path…</option>`);
	sel.innerHTML = opts.join('');
}

export function onDeviceSelectChange() {
	const sel = document.getElementById('cameraDeviceSelect');
	const v = sel.value;
	const resSel = document.getElementById('cameraResolution');
	const fpsSel = document.getElementById('cameraFPS');
	const resGroup = document.getElementById('resolutionGroup');
	const fpsGroup = document.getElementById('fpsGroup');
	const rotSel = document.getElementById('cameraRotation');
	const tsGroup = document.getElementById('timestampGroup');

	// Default: USB-style, everything visible.
	resGroup.classList.remove('hidden');
	fpsGroup.classList.remove('hidden');
	tsGroup.classList.remove('hidden');
	Array.from(rotSel.options).forEach(o => o.disabled = false);

	if (v === 'csi') {
		document.getElementById('cameraDevice').value = 'csi';
		state.currentDev = null;
		resSel.innerHTML = '<option value="">libcamera default</option>';
		fpsSel.innerHTML = '<option value="">libcamera default</option>';
		Array.from(rotSel.options).forEach(o => { if (o.value === '90' || o.value === '270') o.disabled = true; });
		rotSel.value = '0';
		tsGroup.classList.add('hidden');
		document.getElementById('cameraEmbedTimestamp').checked = false;
		document.getElementById('deviceHint').textContent = 'CSI camera — resolution/FPS/timestamp managed by libcamera.';
		return;
	}

	if (v === 'custom' || v === '') {
		if (v === 'custom') { document.getElementById('cameraDevice').value = ''; document.getElementById('cameraDevice').focus(); }
		state.currentDev = null;
		buildResolutionSelect(null);
		document.getElementById('deviceHint').textContent = 'Type the device path and pick a resolution/FPS, or choose Custom.';
		return;
	}

	if (v.startsWith('dev:')) {
		const d = state.discovered.devices[parseInt(v.slice(4), 10)];
		state.currentDev = d;
		document.getElementById('cameraDevice').value = d.device;
		buildResolutionSelect(d);
		document.getElementById('deviceHint').textContent =
			d.type === 'usb' ? 'USB webcam — full options available.' : 'V4L2 device — full options available.';
	}
}

// Build resolution options from a discovered device's formats (prefers MJPG).
function buildResolutionSelect(dev) {
	const resSel = document.getElementById('cameraResolution');
	if (!dev || !dev.formats || dev.formats.length === 0) {
		resSel.innerHTML = '<option value="custom">Custom size…</option>';
		onResolutionChange();
		return;
	}
	const map = {};
	for (const f of dev.formats) {
		const k = f.width + 'x' + f.height;
		(map[k] = map[k] || { w: f.width, h: f.height, fps: new Set(), fmt: new Set() });
		map[k].fps.add(f.fps); map[k].fmt.add(f.format);
	}
	const sizes = Object.values(map).sort((a, b) => (b.w * b.h) - (a.w * a.h));
	resSel.innerHTML = sizes.map(s => {
		const mjpg = s.fmt.has('MJPG') || s.fmt.has('MJPEG');
		const tag = mjpg ? ' MJPG' : (s.fmt.has('H264') ? ' H264' : '');
		return `<option value="${s.w}x${s.h}">${s.w}×${s.h}${tag}</option>`;
	}).join('') + '<option value="custom">Custom size…</option>';
	resSel.value = sizes[0] ? `${sizes[0].w}x${sizes[0].h}` : 'custom';
	onResolutionChange();
}

export function onResolutionChange() {
	const resSel = document.getElementById('cameraResolution');
	const fpsSel = document.getElementById('cameraFPS');
	const customRow = document.getElementById('customResRow');
	const fpsManual = document.getElementById('cameraFPSManual');
	const v = resSel.value;

	if (v === 'custom') {
		customRow.classList.remove('hidden');
		fpsSel.innerHTML = '<option value="custom">Custom FPS…</option>';
		fpsManual.classList.remove('hidden');
		return;
	}
	customRow.classList.add('hidden');

	if (!state.currentDev) { fpsSel.innerHTML = '<option value="custom">Custom FPS…</option>'; return; }
	const [w, h] = v.split('x').map(Number);
	const fps = new Set();
	for (const f of state.currentDev.formats) if (f.width === w && f.height === h) fps.add(f.fps);
	const arr = [...fps].filter(x => x > 0).sort((a, b) => b - a);
	if (arr.length) {
		fpsSel.innerHTML = arr.map(f => `<option value="${f}">${f}</option>`).join('') + '<option value="custom">Custom…</option>';
		fpsSel.value = String(arr[0]);
		fpsManual.classList.add('hidden');
	} else {
		fpsSel.innerHTML = '<option value="custom">Custom FPS…</option>';
		fpsManual.classList.remove('hidden');
	}
}

function readResolutionFromForm() {
	const resSel = document.getElementById('cameraResolution');
	if (resSel.value && resSel.value !== 'custom') {
		const [w, h] = resSel.value.split('x').map(Number);
		return { width: w, height: h };
	}
	return {
		width: parseInt(document.getElementById('cameraResWidth').value, 10) || 0,
		height: parseInt(document.getElementById('cameraResHeight').value, 10) || 0,
	};
}

function readFPSFromForm() {
	const fpsSel = document.getElementById('cameraFPS');
	if (fpsSel && fpsSel.value && fpsSel.value !== 'custom') return parseInt(fpsSel.value, 10);
	return parseInt(document.getElementById('cameraFPSManual').value, 10) || 30;
}

export function renderCameraList(cameras) {
	const c = document.getElementById('camerasList');
	if (!cameras || cameras.length === 0) { c.innerHTML = '<div class="empty-state">No cameras configured. Click “Add Camera”.</div>'; return; }
	c.innerHTML = cameras.map(cam => `
		<div class="camera-card">
			<div class="camera-header">
				<div class="camera-info">
					<div class="camera-name">${esc(cam.name)}</div>
					<div class="camera-meta">${cam.res_width}×${cam.res_height} · ${cam.fps} fps · ${cam.rotation}° · ID: ${esc(cam.id)}</div>
					<div class="camera-device">${esc(cam.device)}</div>
				</div>
				<span class="camera-status ${cam.enabled ? 'status-active' : 'status-inactive'}">${cam.enabled ? 'Active' : 'Disabled'}</span>
			</div>
			<div class="camera-actions">
				<button class="btn-ghost btn-sm" data-action="edit" data-id="${esc(cam.id)}">Edit</button>
				<button class="btn-danger btn-sm" data-action="delete" data-id="${esc(cam.id)}">Delete</button>
			</div>
		</div>`).join('');
	c.querySelectorAll('[data-action]').forEach(b => b.addEventListener('click', () =>
		b.dataset.action === 'edit' ? editCamera(b.dataset.id) : deleteCamera(b.dataset.id)));
}

export function openAddCameraModal() {
	state.editingCameraId = null;
	document.getElementById('cameraModalTitle').textContent = 'Add Camera';
	document.getElementById('cameraId').value = '';
	document.getElementById('cameraName').value = '';
	document.getElementById('cameraDevice').value = '';
	document.getElementById('cameraRotation').value = '0';
	document.getElementById('cameraResWidth').value = '';
	document.getElementById('cameraResHeight').value = '';
	document.getElementById('cameraFPSManual').value = '';
	document.getElementById('cameraMJPEGQuality').value = '8';
	document.getElementById('cameraEmbedTimestamp').checked = true;
	document.getElementById('cameraEnabled').checked = true;
	document.getElementById('cameraModal').classList.add('active');
	loadDiscovery().then(() => {
		if (state.discovered.devices[0]) {
			document.getElementById('cameraDeviceSelect').value = 'dev:0';
			onDeviceSelectChange();
		}
	});
}

export async function editCamera(cameraId) {
	try {
		const data = await apiCall('/api/cameras');
		const cam = data.cameras.find(c => c.id === cameraId);
		if (!cam) { notify('Camera not found', 'error'); return; }
		state.editingCameraId = cameraId;
		document.getElementById('cameraModalTitle').textContent = 'Edit Camera';
		document.getElementById('cameraId').value = cam.id;
		document.getElementById('cameraName').value = cam.name;
		document.getElementById('cameraDevice').value = cam.device;
		document.getElementById('cameraRotation').value = cam.rotation;
		document.getElementById('cameraResWidth').value = cam.res_width;
		document.getElementById('cameraResHeight').value = cam.res_height;
		document.getElementById('cameraFPSManual').value = cam.fps;
		document.getElementById('cameraMJPEGQuality').value = cam.mjpeg_quality;
		document.getElementById('cameraEmbedTimestamp').checked = cam.embed_timestamp;
		document.getElementById('cameraEnabled').checked = cam.enabled;
		document.getElementById('cameraModal').classList.add('active');

		// Try to match the device to a discovered one so dropdowns populate.
		await loadDiscovery();
		const matchIdx = state.discovered.devices.findIndex(d => d.device === cam.device);
		const sel = document.getElementById('cameraDeviceSelect');
		if (matchIdx >= 0) { sel.value = 'dev:' + matchIdx; onDeviceSelectChange(); }
		else { sel.value = 'custom'; onDeviceSelectChange(); }

		// Force custom resolution/FPS with the existing values shown.
		const resSel = document.getElementById('cameraResolution');
		if (resSel) resSel.value = 'custom';
		document.getElementById('customResRow').classList.remove('hidden');
		const fpsSel = document.getElementById('cameraFPS');
		if (fpsSel) { fpsSel.value = 'custom'; document.getElementById('cameraFPSManual').classList.remove('hidden'); }
		document.getElementById('cameraResWidth').value = cam.res_width;
		document.getElementById('cameraResHeight').value = cam.res_height;
		document.getElementById('cameraFPSManual').value = cam.fps;
	} catch (err) {
		notify('Failed to load camera: ' + err.message, 'error');
	}
}

export function closeCameraModal() { document.getElementById('cameraModal').classList.remove('active'); }

export async function saveCameraConfig() {
	const name = document.getElementById('cameraName').value.trim();
	const device = document.getElementById('cameraDevice').value.trim();
	if (!name || !device) { notify('Name and device path are required', 'error'); return; }
	const { width, height } = readResolutionFromForm();
	if (!width || !height) { notify('Valid resolution required', 'error'); return; }
	const fps = readFPSFromForm();
	const payload = {
		name, device,
		rotation: parseInt(document.getElementById('cameraRotation').value, 10) || 0,
		res_width: width, res_height: height, fps,
		bitrate: 1024, // unused by MJPEG capture; kept for config compatibility
		mjpeg_quality: parseInt(document.getElementById('cameraMJPEGQuality').value, 10) || 8,
		embed_timestamp: document.getElementById('cameraEmbedTimestamp').checked,
		enabled: document.getElementById('cameraEnabled').checked,
	};
	if (state.editingCameraId) payload.id = state.editingCameraId;
	try {
		const url = state.editingCameraId
			? `/api/cameras/update?id=${encodeURIComponent(state.editingCameraId)}`
			: '/api/cameras/add';
		await apiCall(url, { method: state.editingCameraId ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
		notify(`Camera ${state.editingCameraId ? 'updated' : 'added'}`, 'success');
		closeCameraModal();
		setTimeout(loadCameras, 500);
	} catch (err) {
		notify('Failed to save camera: ' + err.message, 'error');
	}
}

export async function deleteCamera(cameraId) {
	const ok = await confirmDialog({
		title: 'Delete camera',
		message: `Delete camera "${cameraId}"? Its recording will stop and the entry is removed from config.`,
		confirmText: 'Delete',
	});
	if (!ok) return;
	try {
		await apiCall(`/api/cameras/delete?id=${encodeURIComponent(cameraId)}`, { method: 'DELETE' });
		notify('Camera deleted', 'success');
		loadCameras();
	} catch (err) {
		notify('Failed to delete: ' + err.message, 'error');
	}
}

// loadCameras refreshes the cameras list, the stream-camera selector, and the
// first-run setup banner. Returns the cameras array for callers that chain on it.
export async function loadCameras() {
	try {
		const data = await apiCall('/api/cameras');
		renderCameraList(data.cameras);
		const enabled = (data.cameras || []).filter(c => c.enabled);
		const sel = document.getElementById('streamCamera');
		if (enabled.length) {
			sel.innerHTML = enabled.map(c => `<option value="${esc(c.id)}">${esc(c.name || c.id || 'Camera')}</option>`).join('');
			if (!state.streamCameraId || !enabled.some(c => c.id === state.streamCameraId)) state.streamCameraId = enabled[0].id;
			sel.value = state.streamCameraId;
		} else sel.innerHTML = '';
		sel.classList.toggle('hidden', enabled.length <= 1);
		document.getElementById('setupBanner').classList.toggle('hidden', (data.cameras || []).length > 0);
		return data.cameras;
	} catch (_) {
		document.getElementById('camerasList').innerHTML = '<div class="empty-state">Failed to load cameras</div>';
		return [];
	}
}