// Dashboard: status, recorded segments, exports, segment timezone toggle.
import { state } from './state.js';
import { apiCall } from './api.js';
import { esc, notify, triggerDownload, confirmDialog, formatUptime, formatDuration, formatBytes, utcString, getLocalTimeZone, formatTimeRange } from './ui.js';

export async function loadStatus() {
	try {
		const data = await apiCall('/api/status');
		const badge = document.querySelector('.status-badge');
		badge.classList.toggle('recording', data.status === 'recording');
		document.getElementById('statusText').textContent = data.status === 'recording' ? 'Recording' : 'Idle';

		const pct = Math.round((data.storage.used_bytes / data.storage.cap_bytes) * 100);
		document.getElementById('storageUsed').textContent = data.storage.used_gb.toFixed(2) + ' GB';
		const fill = document.getElementById('storageFill');
		fill.style.width = pct + '%';
		fill.className = 'storage-fill' + (pct > 90 ? ' storage-fill-warn' : '');
		document.getElementById('storageText').textContent = `${data.storage.used_gb.toFixed(2)} GB / ${data.storage.cap_gb} GB`;
		document.getElementById('videoCount').textContent = data.videos.length;
		document.getElementById('uptime').textContent = formatUptime(Date.now() - state.appStartTime);
		renderVideoList(data.videos);
	} catch (_) {}
}

export function renderVideoList(videos) {
	const c = document.getElementById('videoList');
	c.dataset.videos = JSON.stringify(videos || []);
	if (!videos || videos.length === 0) { c.innerHTML = '<div class="empty-state">No segments recorded yet</div>'; return; }
	const groups = {};
	for (const v of videos) { const k = v.camera_id || 'default'; (groups[k] = groups[k] || []).push(v); }
	const multi = Object.keys(groups).length > 1;
	const parts = [];
	for (const [cameraId, items] of Object.entries(groups)) {
		if (multi) parts.push(`<div class="video-group-header" style="font-size:12px;color:var(--muted);padding:6px 2px 2px">${esc(cameraId)}</div>`);
		for (const v of items) {
			const st = v.start_time || v.mod_time, en = v.end_time || v.mod_time;
			parts.push(`
				<div class="video-item">
					<div class="video-info">
						<div class="video-name">${esc(v.name)}</div>
						<div class="video-time">${esc(formatTimeRange(st, en, state.segmentTimezone))}</div>
						<div class="video-meta">${formatBytes(v.size)} | ${formatDuration(v.duration)} | ${esc(state.segmentTimezone === 'utc' ? 'UTC' : getLocalTimeZone())}</div>
					</div>
					<div class="video-actions">
						<button class="btn-ghost btn-sm" data-camera="${esc(v.camera_id)}" data-file="${esc(v.name)}" data-act="dl">MJPEG</button>
						<button class="btn-ghost btn-sm" data-camera="${esc(v.camera_id)}" data-file="${esc(v.name)}" data-act="remux">MP4</button>
					</div>
				</div>`);
		}
	}
	c.innerHTML = parts.join('');
	c.querySelectorAll('button[data-act]').forEach(b => b.addEventListener('click', () =>
		b.dataset.act === 'dl' ? downloadSegment(b.dataset.camera, b.dataset.file) : remuxSegment(b.dataset.camera, b.dataset.file)));
}

export function downloadSegment(cameraId, filename) {
	triggerDownload(`/api/video/download?camera=${encodeURIComponent(cameraId)}&file=${encodeURIComponent(filename)}&token=${state.authToken}`, filename);
}

export async function remuxSegment(cameraId, filename) {
	const btn = document.querySelector(`button[data-camera="${CSS.escape(cameraId)}"][data-file="${CSS.escape(filename)}"][data-act="remux"]`);
	if (btn) { btn.disabled = true; btn.textContent = 'Remuxing…'; }
	try {
		const r = await fetch(`/api/video/remux?camera=${encodeURIComponent(cameraId)}&file=${encodeURIComponent(filename)}&token=${state.authToken}`, { method: 'POST' });
		if (!r.ok) throw new Error(`Remux failed: ${r.status}`);
		const data = await r.json();
		const mp4Name = data.filename || filename.replace(/\.mjpeg$/i, '.mp4');
		checkRemuxStatus();
		await waitForRemuxCompletion(5 * 60 * 1000);
		triggerDownload(`/api/video/remux/download?token=${state.authToken}&file=${encodeURIComponent(mp4Name)}`, mp4Name);
		notify('Remux complete, download started', 'success');
	} catch (err) {
		notify(err.message || 'Failed to remux segment', 'error');
	} finally {
		if (btn) { btn.disabled = false; btn.textContent = 'MP4'; }
	}
}

async function waitForRemuxCompletion(timeoutMs = 300000) {
	const start = Date.now();
	while (Date.now() - start < timeoutMs) {
		try {
			const r = await fetch(`/api/video/remux/status?token=${state.authToken}`);
			if (r.ok) { const d = await r.json(); if (d && d.available && !d.in_progress) return; }
		} catch (_) {}
		await new Promise(r => setTimeout(r, 500));
	}
	throw new Error('Remux timed out');
}

// Segment timezone toggle
export function setSegmentTimezone(mode) {
	state.segmentTimezone = mode;
	localStorage.setItem('segmentTimezone', mode);
	const label = document.getElementById('segmentTimezoneLabel');
	const toggle = document.getElementById('segmentTimezoneToggle');
	if (mode === 'utc') { label.textContent = 'UTC'; toggle.textContent = `Use ${getLocalTimeZone()}`; }
	else { label.textContent = getLocalTimeZone(); toggle.textContent = 'Use UTC'; }
}
export function toggleSegmentTimezone() {
	setSegmentTimezone(state.segmentTimezone === 'utc' ? 'local' : 'utc');
	const list = document.getElementById('videoList');
	if (list && list.dataset.videos) { try { renderVideoList(JSON.parse(list.dataset.videos)); } catch (_) {} }
}

// Export
export function setRange(type) {
	state.activeRange = type;
	document.getElementById('lifetimeRangeBtn').classList.toggle('active', type === 'lifetime');
	document.getElementById('customRangeBtn').classList.toggle('active', type === 'custom');
	document.getElementById('customDateForm').classList.toggle('hidden', type === 'lifetime');
}

function getDateRange() {
	if (state.activeRange === 'lifetime') return { start: new Date(0).toISOString(), end: new Date().toISOString() };
	const s = document.getElementById('startDate').value, e = document.getElementById('endDate').value;
	if (!s || !e) { notify('Please select both start and end dates', 'error'); return null; }
	return { start: new Date(s + ':00Z').toISOString(), end: new Date(e + ':00Z').toISOString() };
}

export async function generateVideo() {
	const range = getDateRange(); if (!range) return;
	const btn = document.getElementById('serverExportBtn');
	btn.disabled = true; btn.textContent = 'Starting…';
	try {
		await apiCall(`/api/videos/generate-export?start=${encodeURIComponent(range.start)}&end=${encodeURIComponent(range.end)}`, { method: 'POST' });
		notify('Export started on Pi', 'info');
		setTimeout(checkExportStatus, 500);
	} catch (err) {
		notify('Failed to start export: ' + err.message, 'error');
	} finally { btn.disabled = false; btn.textContent = 'Generate Export'; }
}

export async function checkExportStatus() {
	try {
		const r = await fetch(`/api/videos/export-status?token=${state.authToken}`); if (!r.ok) return;
		const d = await r.json();
		const prog = document.getElementById('exportProgress'), dl = document.getElementById('exportDownload');
		if (d.in_progress) {
			prog.classList.remove('hidden'); dl.classList.add('hidden');
			document.getElementById('exportProgressLabel').textContent = 'Exporting on Pi…';
			document.getElementById('exportProgressText').textContent = d.progress || 'Working…';
			document.getElementById('exportProgressFill').style.width = (d.current_size_mb > 0 ? Math.min(80, d.current_size_mb) : 20) + '%';
		} else if (d.available) {
			prog.classList.add('hidden'); dl.classList.remove('hidden');
			document.getElementById('exportProgressFill').style.width = '100%';
			document.getElementById('exportDownloadInfo').textContent = `${utcString(d.start_time)} → ${utcString(d.end_time)} | ${(d.size / 1e6).toFixed(1)} MB`;
		} else { prog.classList.add('hidden'); dl.classList.add('hidden'); }
	} catch (_) {}
}

export async function checkRemuxStatus() {
	try {
		const r = await fetch(`/api/video/remux/status?token=${state.authToken}`); if (!r.ok) return;
		const d = await r.json();
		const st = document.getElementById('segmentRemuxStatus');
		if (d && d.in_progress) {
			st.classList.remove('hidden');
			document.getElementById('segmentRemuxLabel').textContent = 'Remuxing segment…';
			document.getElementById('segmentRemuxText').textContent = d.progress || 'Working…';
			document.getElementById('segmentRemuxFill').style.width = '60%'; return;
		}
		if (d && d.available) {
			st.classList.remove('hidden');
			document.getElementById('segmentRemuxLabel').textContent = 'Remux complete';
			document.getElementById('segmentRemuxText').textContent = d.filename || 'Ready to download';
			document.getElementById('segmentRemuxFill').style.width = '100%';
			setTimeout(() => st.classList.add('hidden'), 6000); return;
		}
		st.classList.add('hidden');
	} catch (_) {}
}

export function downloadExport() { triggerDownload(`/api/videos/download-export?token=${state.authToken}`, 'dashcam_export.mp4'); }

export async function deleteExport() {
	const ok = await confirmDialog({ title: 'Delete export', message: 'Delete the current export? You can generate a new one anytime.', confirmText: 'Delete' });
	if (!ok) return;
	try { await fetch(`/api/videos/delete-export?token=${state.authToken}`, { method: 'DELETE' }); checkExportStatus(); notify('Export deleted', 'success'); }
	catch (err) { notify('Failed to delete export: ' + err.message, 'error'); }
}