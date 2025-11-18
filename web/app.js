let authToken = localStorage.getItem('authToken');
let startTime = Date.now();
let editingCameraId = null;

function showError(msg) {
	const errorBox = document.getElementById('errorBox');
	errorBox.innerHTML = '<div class="error-box">' + msg + '<span class="error-close" onclick="hideError()">×</span></div>';
}

function hideError() {
	document.getElementById('errorBox').innerHTML = '';
}

async function apiCall(url, options = {}) {
	const response = await fetch(url, {
		...options,
		headers: {
			'Authorization': 'Bearer ' + authToken,
			...options.headers
		}
	});

	if (response.status === 401) {
		localStorage.removeItem('authToken');
		authToken = null;
		document.getElementById('authModal').classList.add('active');
		throw new Error('Unauthorized');
	}

	if (!response.ok) {
		throw new Error('API call failed: ' + response.statusText);
	}

	return response.json();
}

function authenticate() {
	const token = document.getElementById('authInput').value;
	authToken = token;
	localStorage.setItem('authToken', token);

	// Test the token
	fetch('/api/status', {
		headers: { 'Authorization': 'Bearer ' + token }
	})
	.then(response => {
		if (response.ok) {
			document.getElementById('authModal').classList.remove('active');
			document.getElementById('authError').style.display = 'none';
			loadStatus();
			loadStream();
			loadCameras();
			checkExportStatus();
			setInterval(loadStatus, 5000);
			setInterval(loadCameras, 30000);
			// Check export status more frequently (every 3 seconds to catch progress updates)
			setInterval(checkExportStatus, 3000);
		} else {
			document.getElementById('authError').textContent = 'Invalid token';
			document.getElementById('authError').style.display = 'block';
		}
	});
}

function formatDuration(seconds) {
	const hours = Math.floor(seconds / 3600);
	const minutes = Math.floor((seconds % 3600) / 60);
	const secs = seconds % 60;
	
	if (hours > 0) {
		return hours + 'h ' + minutes + 'm ' + secs + 's';
	} else if (minutes > 0) {
		return minutes + 'm ' + secs + 's';
	} else {
		return secs + 's';
	}
}

function formatUptime(ms) {
	const seconds = Math.floor(ms / 1000);
	const minutes = Math.floor(seconds / 60);
	const hours = Math.floor(minutes / 60);
	const days = Math.floor(hours / 24);
	
	if (days > 0) {
		return days + 'd ' + (hours % 24) + 'h';
	} else if (hours > 0) {
		return hours + 'h ' + (minutes % 60) + 'm';
	} else if (minutes > 0) {
		return minutes + 'm ' + (seconds % 60) + 's';
	} else {
		return seconds + 's';
	}
}

async function loadStatus() {
	try {
		const data = await apiCall('/api/status');
		
		document.getElementById('statusText').textContent = data.status === 'recording' ? 'Recording' : 'Offline';
		
		// Storage stats
		const storagePercent = Math.round((data.storage.used_bytes / data.storage.cap_bytes) * 100);
		document.getElementById('storageUsed').textContent = data.storage.used_gb.toFixed(2) + ' GB';
		document.getElementById('storageFill').style.width = storagePercent + '%';
		document.getElementById('storageText').textContent = 
			data.storage.used_gb.toFixed(2) + ' GB / ' + data.storage.cap_gb + ' GB';
		
		// Video count
		document.getElementById('videoCount').textContent = data.videos.length;
		
		// Uptime
		document.getElementById('uptime').textContent = formatUptime(Date.now() - startTime);
		
		// Load videos
		loadVideos();
	} catch (err) {
		console.error('Failed to load status:', err);
	}
}

function toggleCustomDate() {
	const form = document.getElementById('customDateForm');
	form.style.display = form.style.display === 'none' ? 'block' : 'none';
}

async function loadVideos() {
	try {
		const data = await apiCall('/api/videos');
		const container = document.getElementById('videoList');

		if (!data.videos || data.videos.length === 0) {
			container.innerHTML = '<div class="empty-state">No videos recorded yet</div>';
			return;
		}

		container.innerHTML = data.videos.map(video => 
			'<div class="video-item">' +
			'<div class="video-info">' +
			'<div class="video-name">' + video.name + '</div>' +
			'<div class="video-meta">' +
			(video.size / (1024 * 1024)).toFixed(2) + ' MB • ' + formatDuration(video.duration) + ' • ' +
			new Date(video.mod_time).toLocaleString('en-US', { timeZone: 'UTC', timeZoneName: 'short' }) +
			'</div>' +
			'</div>' +
			'<div class="video-actions">' +
			'<button onclick="downloadVideo(\'' + video.name + '\')">Download</button>' +
			'</div>' +
			'</div>'
		).join('');
	} catch (err) {
		console.error('Failed to load videos:', err);
	}
}

async function checkExportStatus() {
	try {
		const response = await fetch('/api/videos/export-status?token=' + authToken);
		if (response.ok) {
			const data = await response.json();
			const exportSection = document.getElementById('exportSection');
			const progressSection = document.getElementById('exportProgressSection');
			
			if (data.in_progress) {
				// Show progress, hide completed export
				progressSection.style.display = 'block';
				exportSection.style.display = 'none';
				
				let progressText = data.progress || 'Processing...';
				
				// Add file count if available (during copying phase)
				if (data.processed_files > 0 && data.total_segments > 0 && data.current_size_mb === 0) {
					progressText += ` (${data.processed_files}/${data.total_segments} files)`;
				}
				// During encoding, the progress message already includes size info, so don't duplicate
				
				document.getElementById('exportProgressText').textContent = progressText;
			} else if (data.available) {
				// Hide progress, show completed export
				progressSection.style.display = 'none';
				exportSection.style.display = 'block';
				
				const startDate = new Date(data.start_time).toISOString().replace('T', ' ').slice(0, 16);
				const endDate = new Date(data.end_time).toISOString().replace('T', ' ').slice(0, 16);
				const size = (data.size / (1024 * 1024)).toFixed(2);
				document.getElementById('exportInfo').textContent = 
					`${startDate} to ${endDate} UTC • ${size} MB`;
			} else {
				// Hide both sections
				progressSection.style.display = 'none';
				exportSection.style.display = 'none';
			}
		}
	} catch (err) {
		console.error('Failed to check export status:', err);
	}
}

async function generateVideo(type) {
	let startDate, endDate;
	
	if (type === 'lifetime') {
		startDate = new Date(0).toISOString();
		endDate = new Date().toISOString();
		if (!confirm('⚠️ Lifetime video export may take a VERY long time (several minutes to hours depending on the amount of footage). The export will be available for download when complete. Continue?')) {
			return;
		}
	} else if (type === 'custom') {
		const startInput = document.getElementById('startDate').value;
		const endInput = document.getElementById('endDate').value;
		
		if (!startInput || !endInput) {
			showError('Please select both start and end dates');
			return;
		}
		
		// Treat input as UTC - append Z to force UTC interpretation
		startDate = new Date(startInput + ':00Z').toISOString();
		endDate = new Date(endInput + ':00Z').toISOString();		const daysDiff = (new Date(endDate) - new Date(startDate)) / (1000 * 60 * 60 * 24);
		if (daysDiff > 1 && !confirm('⚠️ Video generation may take several minutes for large date ranges (' + Math.round(daysDiff) + ' days selected). The export will be available for download when complete. Continue?')) {
			return;
		}
	}
	
	const btn = type === 'lifetime' ? document.getElementById('lifetimeBtn') : document.getElementById('generateCustomBtn');
	btn.disabled = true;
	const originalText = btn.textContent;
	btn.textContent = '⏳ Generating...';
	
	try {
		const response = await fetch(
			'/api/videos/generate-export?start=' + encodeURIComponent(startDate) + 
			'&end=' + encodeURIComponent(endDate) + '&token=' + authToken,
			{ method: 'POST' }
		);
		
		if (response.ok) {
			showError('🎬 Video generation started! Progress will appear below.');
			// Start checking for export immediately and more frequently
			setTimeout(checkExportStatus, 1000);
		} else {
			const error = await response.text();
			showError('Failed to generate video: ' + error);
		}
	} catch (err) {
		showError('Failed to generate video: ' + err.message);
	} finally {
		btn.disabled = false;
		btn.textContent = originalText;
	}
}

function downloadExport() {
	const url = '/api/videos/download-export?token=' + authToken;
	const a = document.createElement('a');
	a.href = url;
	a.download = 'dashcam_export.mp4';
	a.click();
}

async function deleteExport() {
	if (!confirm('Are you sure you want to delete the current export?')) {
		return;
	}
	
	try {
		const response = await fetch('/api/videos/delete-export?token=' + authToken, {
			method: 'DELETE'
		});
		
		if (response.ok) {
			checkExportStatus();
		} else {
			showError('Failed to delete export');
		}
	} catch (err) {
		showError('Failed to delete export: ' + err.message);
	}
}

function downloadVideo(filename) {
	const url = '/api/video/download?file=' + filename + '&token=' + authToken;
	const a = document.createElement('a');
	a.href = url;
	a.download = filename;
	a.click();
}

async function loadCameras() {
	try {
		console.log('Loading cameras...');
		const data = await apiCall('/api/cameras');
		console.log('Cameras loaded:', data);
		const container = document.getElementById('camerasList');
		
		if (!data.cameras || data.cameras.length === 0) {
			container.innerHTML = '<div class="empty-state">No cameras configured</div>';
			return;
		}
		
		container.innerHTML = data.cameras.map(camera => {
			const rotationLabel = {
				0: '0°',
				90: '90°',
				180: '180°',
				270: '270°'
			}[camera.rotation] || camera.rotation + '°';
			
			return '<div class="camera-card">' +
				'<div class="camera-header">' +
				'<div class="camera-info">' +
				'<div class="camera-name">' + camera.name + '</div>' +
				'<div class="camera-meta">' +
				'ID: ' + camera.id + ' • ' + camera.res_width + 'x' + camera.res_height + ' • ' +
				'Rotation: ' + rotationLabel +
				'</div>' +
				'<div class="camera-device">' + camera.device + '</div>' +
				'</div>' +
				'<div class="camera-status" style="' + (camera.enabled ? 'background: #4ade80' : 'background: #666') + '">' +
				(camera.enabled ? '✓ Active' : '○ Disabled') +
				'</div>' +
				'</div>' +
				'<div class="camera-actions">' +
				'<button onclick="editCamera(\'' + camera.id + '\')">Edit</button>' +
				'<button onclick="deleteCamera(\'' + camera.id + '\')" style="background: #dc2626;">Delete</button>' +
				'</div>' +
				'</div>';
		}).join('');
	} catch (err) {
		console.error('Failed to load cameras:', err);
		const container = document.getElementById('camerasList');
		container.innerHTML = '<div class="empty-state">Failed to load cameras</div>';
	}
}

function openAddCameraModal() {
	editingCameraId = null;
	document.getElementById('cameraModalTitle').textContent = 'Add New Camera';
	document.getElementById('cameraId').value = '';
	document.getElementById('cameraId').disabled = true;
	document.getElementById('cameraName').value = '';
	document.getElementById('cameraDevice').value = '';
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
		const camera = data.cameras.find(c => c.id === cameraId);
		
		if (!camera) {
			showError('Camera not found');
			return;
		}
		
		editingCameraId = cameraId;
		document.getElementById('cameraModalTitle').textContent = 'Edit Camera';
		document.getElementById('cameraId').value = camera.id;
		document.getElementById('cameraId').disabled = true;
		document.getElementById('cameraName').value = camera.name;
		document.getElementById('cameraDevice').value = camera.device;
		document.getElementById('cameraRotation').value = camera.rotation;
		document.getElementById('cameraResWidth').value = camera.res_width;
		document.getElementById('cameraResHeight').value = camera.res_height;
		document.getElementById('cameraBitrate').value = camera.bitrate;
		document.getElementById('cameraFPS').value = camera.fps;
		document.getElementById('cameraMJPEGQuality').value = camera.mjpeg_quality;
		document.getElementById('cameraEmbedTimestamp').checked = camera.embed_timestamp;
		document.getElementById('cameraEnabled').checked = camera.enabled;
		document.getElementById('cameraModal').classList.add('active');
	} catch (err) {
		console.error('Failed to edit camera:', err);
		showError('Failed to load camera details');
	}
}

function closeCameraModal() {
	document.getElementById('cameraModal').classList.remove('active');
}

async function saveCameraConfig() {
	const name = document.getElementById('cameraName').value;
	const device = document.getElementById('cameraDevice').value;
	
	if (!name || !device) {
		showError('Please fill in required fields (Name, Device)');
		return;
	}
	
	const cameraData = {
		name: name,
		device: device,
		rotation: parseInt(document.getElementById('cameraRotation').value),
		res_width: parseInt(document.getElementById('cameraResWidth').value),
		res_height: parseInt(document.getElementById('cameraResHeight').value),
		bitrate: parseInt(document.getElementById('cameraBitrate').value),
		fps: parseInt(document.getElementById('cameraFPS').value),
		mjpeg_quality: parseInt(document.getElementById('cameraMJPEGQuality').value),
		embed_timestamp: document.getElementById('cameraEmbedTimestamp').checked,
		enabled: document.getElementById('cameraEnabled').checked
	};
	
	try {
		let url = '/api/cameras/add';
		let method = 'POST';
		
		if (editingCameraId) {
			url = '/api/cameras/update?id=' + encodeURIComponent(editingCameraId);
			method = 'PUT';
		}
		
		const response = await apiCall(url, {
			method: method,
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(cameraData)
		});
		
		showError('✓ Camera ' + (editingCameraId ? 'updated' : 'added') + '. Changes applied.');
		closeCameraModal();
		setTimeout(loadCameras, 1000);
	} catch (err) {
		showError('Failed to save camera: ' + err.message);
	}
}

async function deleteCamera(cameraId) {
	if (!confirm('Are you sure you want to delete this camera? Restart required for changes.')) {
		return;
	}
	
	try {
		await apiCall('/api/cameras/delete?id=' + encodeURIComponent(cameraId), {
			method: 'DELETE'
		});
		
		showError('✓ Camera deleted. Changes applied.');
		loadCameras();
	} catch (err) {
		showError('Failed to delete camera: ' + err.message);
	}
}

function loadStream() {
	const container = document.getElementById('playerContainer');
	
	// Use polling for better browser compatibility
	loadStreamPolling();
}

function loadStreamPolling() {
	const container = document.getElementById('playerContainer');
	container.innerHTML = '<img id="live-stream" class="stream-viewer" src="" alt="Live stream">';
	
	const img = document.getElementById('live-stream');
	let isLoading = false;
	
	setInterval(() => {
		if (isLoading) return;
		isLoading = true;
		
		const now = Date.now();
		const url = '/api/stream/frame?token=' + authToken + '&t=' + now;
		
		const newImg = new Image();
		newImg.onload = function() {
			img.src = this.src;
			isLoading = false;
		};
		newImg.onerror = function() {
			isLoading = false;
		};
		newImg.src = url;
	}, 40);
}

// Initial load
if (!authToken) {
	document.getElementById('authModal').classList.add('active');
} else {
	loadStatus();
	loadStream();
	loadCameras();
	checkExportStatus();
	setInterval(loadStatus, 5000);
	setInterval(loadCameras, 30000);
	// Check export status more frequently (every 3 seconds to catch progress updates)
	setInterval(checkExportStatus, 3000);
}
