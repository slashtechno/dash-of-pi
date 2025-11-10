let authToken = localStorage.getItem('authToken');
let startTime = Date.now();

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
			checkExportStatus();
			setInterval(loadStatus, 5000);
			setInterval(checkExportStatus, 10000); // Check for exports every 10 seconds
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
			
			if (data.available) {
				exportSection.style.display = 'block';
				const startDate = new Date(data.start_time).toLocaleString('en-US', { 
					timeZone: 'UTC', 
					dateStyle: 'short',
					timeStyle: 'short',
					timeZoneName: 'short'
				});
				const endDate = new Date(data.end_time).toLocaleString('en-US', { 
					timeZone: 'UTC',
					dateStyle: 'short', 
					timeStyle: 'short',
					timeZoneName: 'short'
				});
				const size = (data.size / (1024 * 1024)).toFixed(2);
				document.getElementById('exportInfo').textContent = 
					`${startDate} to ${endDate} • ${size} MB`;
			} else {
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
		
		// Treat input as UTC
		startDate = new Date(startInput + ':00Z').toISOString();
		endDate = new Date(endInput + ':00Z').toISOString();
		
		const daysDiff = (new Date(endDate) - new Date(startDate)) / (1000 * 60 * 60 * 24);
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
			showError('🎬 Video generation started! This may take several minutes. The export will appear above when ready. Check server logs for progress.');
			// Start checking for export immediately
			setTimeout(checkExportStatus, 2000);
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

function loadStream() {
	const container = document.getElementById('playerContainer');
	
	// Try MJPEG multipart streaming first (more efficient)
	const useMJPEGStream = true;
	
	if (useMJPEGStream) {
		container.innerHTML = '<img id="live-stream" class="stream-viewer" src="" alt="Live stream">';
		const img = document.getElementById('live-stream');
		let reconnectAttempts = 0;
		const maxReconnectAttempts = 3;
		
		function connectStream() {
			const timestamp = new Date().getTime();
			img.src = '/api/stream/mjpeg?token=' + authToken + '&t=' + timestamp;
			console.log('MJPEG stream connecting...');
		}
		
		// Auto-reconnect on error
		img.onerror = function() {
			reconnectAttempts++;
			console.log('MJPEG stream error (attempt ' + reconnectAttempts + '/' + maxReconnectAttempts + ')');
			
			if (reconnectAttempts < maxReconnectAttempts) {
				setTimeout(connectStream, 2000);
			} else {
				console.log('MJPEG streaming failed after ' + maxReconnectAttempts + ' attempts, falling back to polling');
				loadStreamPolling();
			}
		};
		
		// Monitor for stream stalls
		let lastImageUpdate = Date.now();
		img.onload = function() {
			lastImageUpdate = Date.now();
			reconnectAttempts = 0;
		};
		
		setInterval(function() {
			const timeSinceUpdate = Date.now() - lastImageUpdate;
			if (timeSinceUpdate > 10000 && reconnectAttempts < maxReconnectAttempts) {
				console.log('Stream appears stalled, reconnecting...');
				connectStream();
			}
		}, 10000);
		
		connectStream();
	} else {
		loadStreamPolling();
	}
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
	checkExportStatus();
	setInterval(loadStatus, 5000);
	setInterval(checkExportStatus, 10000);
}
