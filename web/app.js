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
let segmentTimezone = localStorage.getItem('segmentTimezone') || 'local';

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

function getLocalTimeZone() {
	const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
	return tz || 'Local';
}

function formatTimestamp(isoString, mode) {
	const date = new Date(isoString);
	const options = mode === 'utc'
		? { timeZone: 'UTC', timeZoneName: 'short' }
		: { timeZoneName: 'short' };
	return date.toLocaleString('en-US', options);
}

function formatTimeRange(startIso, endIso, mode) {
	return `${formatTimestamp(startIso, mode)} to ${formatTimestamp(endIso, mode)}`;
}

function setSegmentTimezone(mode) {
	segmentTimezone = mode;
	localStorage.setItem('segmentTimezone', mode);
	const label = document.getElementById('segmentTimezoneLabel');
	const toggle = document.getElementById('segmentTimezoneToggle');
	if (mode === 'utc') {
		label.textContent = 'UTC';
		toggle.textContent = `Use ${getLocalTimeZone()}`;
	} else {
		label.textContent = getLocalTimeZone();
		toggle.textContent = 'Use UTC';
	}
}

function toggleSegmentTimezone() {
	setSegmentTimezone(segmentTimezone === 'utc' ? 'local' : 'utc');
	const list = document.getElementById('videoList');
	if (list && list.dataset.videos) {
		try {
			const videos = JSON.parse(list.dataset.videos);
			renderVideoList(videos);
		} catch (_) {}
	}
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
	container.dataset.videos = JSON.stringify(videos || []);

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
			const startTime = v.start_time || v.mod_time;
			const endTime = v.end_time || v.mod_time;
			const timeRange = formatTimeRange(startTime, endTime, segmentTimezone);
			// camera_id and name are user-controlled -> escape
			parts.push(`
				<div class="video-item">
					<div class="video-info">
						<div class="video-name">${esc(v.name)}</div>
						<div class="video-time">${esc(timeRange)}</div>
						<div class="video-meta">${formatBytes(v.size)} | ${formatDuration(v.duration)} | ${esc(segmentTimezone === 'utc' ? 'UTC' : getLocalTimeZone())}</div>
					</div>
					<div class="video-actions">
						<button data-camera="${esc(v.camera_id)}" data-file="${esc(v.name)}" class="dl-btn">Download MJPEG</button>
						<button data-camera="${esc(v.camera_id)}" data-file="${esc(v.name)}" class="remux-btn btn-ghost">Download MP4</button>
					</div>
				</div>`);
		}
	}

	container.innerHTML = parts.join('');

	// Attach download handlers via data attributes to avoid inline event handlers
	container.querySelectorAll('.dl-btn').forEach(btn => {
		btn.addEventListener('click', () => downloadSegment(btn.dataset.camera, btn.dataset.file));
	});
	container.querySelectorAll('.remux-btn').forEach(btn => {
		btn.addEventListener('click', () => remuxSegment(btn.dataset.camera, btn.dataset.file));
	});
}

function downloadSegment(cameraId, filename) {
	triggerDownload(
		`/api/video/download?camera=${encodeURIComponent(cameraId)}&file=${encodeURIComponent(filename)}&token=${authToken}`,
		filename
	);
}

async function remuxSegment(cameraId, filename) {
	const btn = document.querySelector(`.remux-btn[data-camera="${CSS.escape(cameraId)}"][data-file="${CSS.escape(filename)}"]`);
	if (btn) {
		btn.disabled = true;
		btn.textContent = 'Remuxing...';
	}

	try {
		const response = await fetch(
			`/api/video/remux?camera=${encodeURIComponent(cameraId)}&file=${encodeURIComponent(filename)}&token=${authToken}`,
			{ method: 'POST' }
		);
		if (!response.ok) {
			throw new Error(`Remux failed: ${response.status} ${response.statusText}`);
		}
		const data = await response.json();
		const mp4Name = data.filename || filename.replace(/\.mjpeg$/i, '.mp4');
		
		checkRemuxStatus();
		
		// Wait for remux to complete before downloading
		await waitForRemuxCompletion(5 * 60 * 1000); // 5 minute timeout
		
		triggerDownload(`/api/video/remux/download?token=${authToken}&file=${encodeURIComponent(mp4Name)}`, mp4Name);
		notify('Remux complete, download started', 'success');
	} catch (err) {
		notify(err.message || 'Failed to remux segment', 'error');
	} finally {
		if (btn) {
			btn.disabled = false;
			btn.textContent = 'Download MP4';
		}
	}
}

async function waitForRemuxCompletion(timeoutMs = 300000) {
	const startTime = Date.now();
	const pollInterval = 500; // Check every 500ms
	
	while (Date.now() - startTime < timeoutMs) {
		try {
			const r = await fetch(`/api/video/remux/status?token=${authToken}`);
			if (r.ok) {
				const data = await r.json();
				if (data && data.available && !data.in_progress) {
					return; // Remux complete
				}
			}
		} catch (_) {}
		
		await new Promise(resolve => setTimeout(resolve, pollInterval));
	}
	
	throw new Error('Remux timed out');
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
		const select = document.getElementById('streamCamera');
		if (enabled.length > 0) {
			select.innerHTML = enabled.map(c => {
				const label = c.name || c.id || 'Camera';
				return `<option value="${esc(c.id)}">${esc(label)}</option>`;
			}).join('');
			if (!streamCameraId || !enabled.some(c => c.id === streamCameraId)) {
				streamCameraId = enabled[0].id;
			}
			select.value = streamCameraId;
		} else {
			select.innerHTML = '';
		}
		select.classList.toggle('hidden', enabled.length <= 1);
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

async function checkRemuxStatus() {
	try {
		const r = await fetch(`/api/video/remux/status?token=${authToken}`);
		if (!r.ok) return;
		const data = await r.json();

		const statusEl = document.getElementById('segmentRemuxStatus');
		const labelEl = document.getElementById('segmentRemuxLabel');
		const fillEl = document.getElementById('segmentRemuxFill');
		const textEl = document.getElementById('segmentRemuxText');

		if (data && data.in_progress) {
			statusEl.classList.remove('hidden');
			labelEl.textContent = 'Remuxing segment...';
			textEl.textContent = data.progress || 'Working...';
			fillEl.style.width = '60%';
			return;
		}

		if (data && data.available) {
			statusEl.classList.remove('hidden');
			labelEl.textContent = 'Remux complete';
			textEl.textContent = data.filename || 'Ready to download';
			fillEl.style.width = '100%';
			setTimeout(() => statusEl.classList.add('hidden'), 6000);
			return;
		}

		statusEl.classList.add('hidden');
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
	setRange(activeRange);
	setSegmentTimezone(segmentTimezone);
	loadStatus();
	loadCameras().then(() => startStream());
	checkExportStatus();
	checkRemuxStatus();
	setInterval(loadStatus, 5000);
	setInterval(loadCameras, 30000);
	setInterval(checkExportStatus, 3000);
	setInterval(checkRemuxStatus, 3000);
}

if (!authToken) {
	document.getElementById('authModal').classList.add('active');
} else {
	startApp();
}
