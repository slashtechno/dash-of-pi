// Auth

let authToken = localStorage.getItem('authToken');

const urlParams = new URLSearchParams(window.location.search);
if (urlParams.has('token')) {
	authToken = urlParams.get('token');
	localStorage.setItem('authToken', authToken);
	window.history.replaceState({}, document.title, window.location.pathname);
}

let editingCameraId = null;
let activeRange = 'lifetime';
let streamCameraId = null;
let streamInterval = null;

// HTML escaping

// Escape user-controlled strings before placing them in innerHTML.
function esc(val) {
	return String(val)
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;')
		.replace(/'/g, '&#39;');
}

// Notifications

function notify(message, type = 'info') {
	const container = document.getElementById('toastContainer');
	const toast = document.createElement('div');
	toast.className = `toast toast-${type}`;
	toast.textContent = message; // textContent is safe
	container.appendChild(toast);
	setTimeout(() => toast.remove(), type === 'error' ? 6000 : 4000);
}

// API

async function apiCall(url, options = {}) {
	const response = await fetch(url, {
		...options,
		headers: {
			'Authorization': 'Bearer ' + authToken,
			...options.headers,
		},
	});

	if (response.status === 401) {
		localStorage.removeItem('authToken');
		authToken = null;
		document.getElementById('authModal').classList.add('active');
		throw new Error('Unauthorized');
	}

	if (!response.ok) {
		throw new Error(`API error: ${response.status} ${response.statusText}`);
	}

	return response.json();
}

// Auth modal

function authenticate() {
	const token = document.getElementById('authInput').value.trim();
	if (!token) return;

	fetch('/api/status', { headers: { 'Authorization': 'Bearer ' + token } })
		.then(r => {
			if (r.ok) {
				authToken = token;
				localStorage.setItem('authToken', token);
				document.getElementById('authModal').classList.remove('active');
				document.getElementById('authError').classList.add('hidden');
				startApp();
			} else {
				document.getElementById('authError').textContent = 'Invalid token';
				document.getElementById('authError').classList.remove('hidden');
			}
		});
}

// Formatting

function formatUptime(ms) {
	const s = Math.floor(ms / 1000);
	const m = Math.floor(s / 60);
	const h = Math.floor(m / 60);
	const d = Math.floor(h / 24);
	if (d > 0) return `${d}d ${h % 24}h`;
	if (h > 0) return `${h}h ${m % 60}m`;
	if (m > 0) return `${m}m ${s % 60}s`;
	return `${s}s`;
}

function formatDuration(seconds) {
	const h = Math.floor(seconds / 3600);
	const m = Math.floor((seconds % 3600) / 60);
	const s = seconds % 60;
	if (h > 0) return `${h}h ${m}m`;
	if (m > 0) return `${m}m ${s}s`;
	return `${s}s`;
}

function formatBytes(bytes) {
	if (bytes >= 1e9) return (bytes / 1e9).toFixed(2) + ' GB';
	if (bytes >= 1e6) return (bytes / 1e6).toFixed(1) + ' MB';
	return Math.round(bytes / 1e3) + ' KB';
}

function utcString(isoString) {
	return new Date(isoString).toLocaleString('en-US', { timeZone: 'UTC', timeZoneName: 'short' });
}

// Status

const appStartTime = Date.now();

async function loadStatus() {
	try {
		const data = await apiCall('/api/status');

		document.getElementById('statusText').textContent =
			data.status === 'recording' ? 'Recording' : 'Idle';

		const pct = Math.round((data.storage.used_bytes / data.storage.cap_bytes) * 100);
		document.getElementById('storageUsed').textContent = data.storage.used_gb.toFixed(2) + ' GB';
		document.getElementById('storageFill').style.width = pct + '%';
		document.getElementById('storageFill').className =
			'storage-fill' + (pct > 90 ? ' storage-fill-warn' : '');
		document.getElementById('storageText').textContent =
			`${data.storage.used_gb.toFixed(2)} GB / ${data.storage.cap_gb} GB`;

		document.getElementById('videoCount').textContent = data.videos.length;
		document.getElementById('uptime').textContent = formatUptime(Date.now() - appStartTime);

		renderVideoList(data.videos);
	} catch (_) {}
}

// Video list

function renderVideoList(videos) {
	const container = document.getElementById('videoList');

	if (!videos || videos.length === 0) {
		container.innerHTML = '<div class="empty-state">No segments recorded yet</div>';
		return;
	}

	// Group by camera when multiple cameras are configured
	const groups = {};
	for (const v of videos) {
		const key = v.camera_id || 'default';
		if (!groups[key]) groups[key] = [];
		groups[key].push(v);
	}

	const multiCamera = Object.keys(groups).length > 1;
	const parts = [];

	for (const [cameraId, items] of Object.entries(groups)) {
		if (multiCamera) {
			parts.push(`<div class="video-group-header">${esc(cameraId)}</div>`);
		}
		for (const v of items) {
			// camera_id and name are user-controlled -> escape
			parts.push(`
				<div class="video-item">
					<div class="video-info">
						<div class="video-name">${esc(v.name)}</div>
						<div class="video-meta">${formatBytes(v.size)} | ${formatDuration(v.duration)} | ${utcString(v.mod_time)}</div>
					</div>
					<button data-camera="${esc(v.camera_id)}" data-file="${esc(v.name)}" class="dl-btn">Download</button>
				</div>`);
		}
	}

	container.innerHTML = parts.join('');

	// Attach download handlers via data attributes to avoid inline event handlers
	container.querySelectorAll('.dl-btn').forEach(btn => {
		btn.addEventListener('click', () => downloadSegment(btn.dataset.camera, btn.dataset.file));
	});
}

function downloadSegment(cameraId, filename) {
	triggerDownload(
		`/api/video/download?camera=${encodeURIComponent(cameraId)}&file=${encodeURIComponent(filename)}&token=${authToken}`,
		filename
	);
}

function triggerDownload(url, filename) {
	const a = document.createElement('a');
	a.href = url;
	a.download = filename;
	a.click();
}

// Live stream

async function loadCameras() {
	try {
		const data = await apiCall('/api/cameras');
		renderCameraList(data.cameras);

		const enabled = (data.cameras || []).filter(c => c.enabled);
		if (enabled.length > 0) {
			const select = document.getElementById('streamCamera');
			select.innerHTML = enabled.map(c => `<option value="${esc(c.id)}">${esc(c.name)}</option>`).join('');
			select.classList.toggle('hidden', enabled.length <= 1);
			if (!streamCameraId) streamCameraId = enabled[0].id;
		}
	} catch (_) {
		document.getElementById('camerasList').innerHTML = '<div class="empty-state">Failed to load cameras</div>';
	}
}

function switchStreamCamera() {
	streamCameraId = document.getElementById('streamCamera').value;
}

function startStream() {
	const container = document.getElementById('playerContainer');
	container.innerHTML = '<img id="liveStream" class="stream-viewer" alt="Live stream">';
	const img = document.getElementById('liveStream');
	let loading = false;

	if (streamInterval) clearInterval(streamInterval);

	streamInterval = setInterval(() => {
		if (loading) return;
		loading = true;
		const cam = streamCameraId ? `&camera=${encodeURIComponent(streamCameraId)}` : '';
		const next = new Image();
		next.onload = () => { img.src = next.src; loading = false; };
		next.onerror = () => { loading = false; };
		next.src = `/api/stream/frame?token=${authToken}${cam}&t=${Date.now()}`;
	}, 40);
}

// Camera management

function renderCameraList(cameras) {
	const container = document.getElementById('camerasList');

	if (!cameras || cameras.length === 0) {
		container.innerHTML = '<div class="empty-state">No cameras configured</div>';
		return;
	}

	const parts = cameras.map(cam => `
		<div class="camera-card">
			<div class="camera-header">
				<div class="camera-info">
					<div class="camera-name">${esc(cam.name)}</div>
					<div class="camera-meta">${cam.res_width}x${cam.res_height} | ${cam.fps} fps | ${cam.rotation} deg | ID: ${esc(cam.id)}</div>
					<div class="camera-device">${esc(cam.device)}</div>
				</div>
				<span class="camera-status ${cam.enabled ? 'status-active' : 'status-inactive'}">
					${cam.enabled ? 'Active' : 'Disabled'}
				</span>
			</div>
			<div class="camera-actions">
				<button data-action="edit" data-id="${esc(cam.id)}">Edit</button>
				<button data-action="delete" data-id="${esc(cam.id)}" class="btn-danger-sm">Delete</button>
			</div>
		</div>
	`);

	container.innerHTML = parts.join('');

	container.querySelectorAll('[data-action]').forEach(btn => {
		btn.addEventListener('click', () => {
			if (btn.dataset.action === 'edit') editCamera(btn.dataset.id);
			else if (btn.dataset.action === 'delete') deleteCamera(btn.dataset.id);
		});
	});
}

function openAddCameraModal() {
	editingCameraId = null;
	document.getElementById('cameraModalTitle').textContent = 'Add Camera';
	['cameraId', 'cameraName', 'cameraDevice'].forEach(id => {
		document.getElementById(id).value = '';
	});
	document.getElementById('cameraRotation').value = '0';
	document.getElementById('cameraResWidth').value = '1920';
	document.getElementById('cameraResHeight').value = '1080';
	document.getElementById('cameraBitrate').value = '2048';
	document.getElementById('cameraFPS').value = '30';
	document.getElementById('cameraMJPEGQuality').value = '5';
	document.getElementById('cameraEmbedTimestamp').checked = true;
	document.getElementById('cameraEnabled').checked = true;
	document.getElementById('cameraModal').classList.add('active');
}

async function editCamera(cameraId) {
	try {
		const data = await apiCall('/api/cameras');
		const cam = data.cameras.find(c => c.id === cameraId);
		if (!cam) { notify('Camera not found', 'error'); return; }

		editingCameraId = cameraId;
		document.getElementById('cameraModalTitle').textContent = 'Edit Camera';
		document.getElementById('cameraId').value = cam.id;
		document.getElementById('cameraName').value = cam.name;
		document.getElementById('cameraDevice').value = cam.device;
		document.getElementById('cameraRotation').value = cam.rotation;
		document.getElementById('cameraResWidth').value = cam.res_width;
		document.getElementById('cameraResHeight').value = cam.res_height;
		document.getElementById('cameraBitrate').value = cam.bitrate;
		document.getElementById('cameraFPS').value = cam.fps;
		document.getElementById('cameraMJPEGQuality').value = cam.mjpeg_quality;
		document.getElementById('cameraEmbedTimestamp').checked = cam.embed_timestamp;
		document.getElementById('cameraEnabled').checked = cam.enabled;
		document.getElementById('cameraModal').classList.add('active');
	} catch (err) {
		notify('Failed to load camera: ' + err.message, 'error');
	}
}

function closeCameraModal() {
	document.getElementById('cameraModal').classList.remove('active');
}

async function saveCameraConfig() {
	const name = document.getElementById('cameraName').value.trim();
	const device = document.getElementById('cameraDevice').value.trim();

	if (!name || !device) {
		notify('Name and device are required', 'error');
		return;
	}

	const payload = {
		name,
		device,
		rotation: parseInt(document.getElementById('cameraRotation').value),
		res_width: parseInt(document.getElementById('cameraResWidth').value),
		res_height: parseInt(document.getElementById('cameraResHeight').value),
		bitrate: parseInt(document.getElementById('cameraBitrate').value),
		fps: parseInt(document.getElementById('cameraFPS').value),
		mjpeg_quality: parseInt(document.getElementById('cameraMJPEGQuality').value),
		embed_timestamp: document.getElementById('cameraEmbedTimestamp').checked,
		enabled: document.getElementById('cameraEnabled').checked,
	};

	try {
		const url = editingCameraId
			? `/api/cameras/update?id=${encodeURIComponent(editingCameraId)}`
			: '/api/cameras/add';
		await apiCall(url, {
			method: editingCameraId ? 'PUT' : 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(payload),
		});
		notify(`Camera ${editingCameraId ? 'updated' : 'added'}`, 'success');
		closeCameraModal();
		setTimeout(loadCameras, 500);
	} catch (err) {
		notify('Failed to save camera: ' + err.message, 'error');
	}
}

async function deleteCamera(cameraId) {
	if (!confirm(`Delete camera "${cameraId}"?`)) return;
	try {
		await apiCall(`/api/cameras/delete?id=${encodeURIComponent(cameraId)}`, { method: 'DELETE' });
		notify('Camera deleted', 'success');
		loadCameras();
	} catch (err) {
		notify('Failed to delete: ' + err.message, 'error');
	}
}

// Export: date range

function setRange(type) {
	activeRange = type;
	document.getElementById('lifetimeRangeBtn').classList.toggle('active', type === 'lifetime');
	document.getElementById('customRangeBtn').classList.toggle('active', type === 'custom');
	document.getElementById('customDateForm').classList.toggle('hidden', type === 'lifetime');
}

function getDateRange() {
	if (activeRange === 'lifetime') {
		return { start: new Date(0).toISOString(), end: new Date().toISOString() };
	}

	const startInput = document.getElementById('startDate').value;
	const endInput = document.getElementById('endDate').value;
	if (!startInput || !endInput) {
		notify('Please select both start and end dates', 'error');
		return null;
	}

	return {
		start: new Date(startInput + ':00Z').toISOString(),
		end: new Date(endInput + ':00Z').toISOString(),
	};
}

// Export: server-side

async function generateVideo() {
	const range = getDateRange();
	if (!range) return;

	const btn = document.getElementById('serverExportBtn');
	btn.disabled = true;
	btn.textContent = 'Starting...';

	try {
		await apiCall(
			`/api/videos/generate-export?start=${encodeURIComponent(range.start)}&end=${encodeURIComponent(range.end)}`,
			{ method: 'POST' }
		);
		notify('Export started on Pi', 'info');
		setTimeout(checkExportStatus, 500);
	} catch (err) {
		notify('Failed to start export: ' + err.message, 'error');
	} finally {
		btn.disabled = false;
		btn.textContent = 'Export';
	}
}

async function checkExportStatus() {
	try {
		const r = await fetch(`/api/videos/export-status?token=${authToken}`);
		if (!r.ok) return;
		const data = await r.json();

		const progressEl = document.getElementById('exportProgress');
		const downloadEl = document.getElementById('exportDownload');

		if (data.in_progress) {
			progressEl.classList.remove('hidden');
			downloadEl.classList.add('hidden');
			document.getElementById('exportProgressLabel').textContent = 'Exporting on Pi...';
			document.getElementById('exportProgressText').textContent = data.progress || 'Working...';
			// Indeterminate progress during remux
			const pct = data.current_size_mb > 0 ? Math.min(80, data.current_size_mb) : 20;
			document.getElementById('exportProgressFill').style.width = pct + '%';
		} else if (data.available) {
			progressEl.classList.add('hidden');
			downloadEl.classList.remove('hidden');
			document.getElementById('exportProgressFill').style.width = '100%';

			const sizeMB = (data.size / 1e6).toFixed(1);
			document.getElementById('exportDownloadInfo').textContent =
				`${utcString(data.start_time)} -> ${utcString(data.end_time)} | ${sizeMB} MB`;
		} else {
			progressEl.classList.add('hidden');
			downloadEl.classList.add('hidden');
		}
	} catch (_) {}
}

function downloadExport() {
	triggerDownload(`/api/videos/download-export?token=${authToken}`, 'dashcam_export.mp4');
}

async function deleteExport() {
	if (!confirm('Delete the current export?')) return;
	try {
		await fetch(`/api/videos/delete-export?token=${authToken}`, { method: 'DELETE' });
		checkExportStatus();
		notify('Export deleted', 'success');
	} catch (err) {
		notify('Failed to delete export: ' + err.message, 'error');
	}
}

// Init

function startApp() {
	loadStatus();
	loadCameras().then(() => startStream());
	checkExportStatus();
	setInterval(loadStatus, 5000);
	setInterval(loadCameras, 30000);
	setInterval(checkExportStatus, 3000);
}

if (!authToken) {
	document.getElementById('authModal').classList.add('active');
} else {
	startApp();
}
