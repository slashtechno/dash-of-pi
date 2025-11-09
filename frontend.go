package main

func getEmbeddedHTML() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Pi DashCam</title>

	<style>
		* {
			margin: 0;
			padding: 0;
			box-sizing: border-box;
		}

		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
			background: #0f1419;
			color: #e0e0e0;
			min-height: 100vh;
			padding: 20px;
		}

		.container {
			max-width: 1200px;
			margin: 0 auto;
		}

		header {
			display: flex;
			justify-content: space-between;
			align-items: center;
			margin-bottom: 30px;
			padding-bottom: 20px;
			border-bottom: 1px solid #333;
		}

		h1 {
			font-size: 28px;
			font-weight: 600;
		}

		.status-badge {
			display: inline-flex;
			align-items: center;
			gap: 8px;
			padding: 8px 16px;
			background: #1a4d2e;
			border-radius: 20px;
			font-size: 14px;
		}

		.status-dot {
			width: 8px;
			height: 8px;
			background: #4ade80;
			border-radius: 50%;
			animation: pulse 2s infinite;
		}

		@keyframes pulse {
			0%, 100% { opacity: 1; }
			50% { opacity: 0.5; }
		}

		.stats-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
			gap: 20px;
			margin-bottom: 30px;
		}

		.stat-card {
			background: #1a1f26;
			border: 1px solid #333;
			border-radius: 8px;
			padding: 20px;
		}

		.stat-label {
			font-size: 12px;
			color: #888;
			text-transform: uppercase;
			margin-bottom: 10px;
			letter-spacing: 1px;
		}

		.stat-value {
			font-size: 28px;
			font-weight: 600;
			color: #4ade80;
			margin-bottom: 10px;
		}

		.storage-bar {
			height: 6px;
			background: #333;
			border-radius: 3px;
			overflow: hidden;
		}

		.storage-fill {
			height: 100%;
			background: #4ade80;
			transition: width 0.3s ease;
		}

		.storage-fill.warning {
			background: #facc15;
		}

		.storage-fill.critical {
			background: #ef4444;
		}

		.storage-text {
			font-size: 12px;
			color: #888;
			margin-top: 8px;
		}

		.section {
			margin-bottom: 30px;
		}

		.section-title {
			font-size: 18px;
			font-weight: 600;
			margin-bottom: 15px;
			padding-bottom: 10px;
			border-bottom: 1px solid #333;
		}

		.video-list {
			display: grid;
			gap: 10px;
		}

		.video-item {
			background: #1a1f26;
			border: 1px solid #333;
			border-radius: 8px;
			padding: 15px;
			display: flex;
			justify-content: space-between;
			align-items: center;
			transition: background 0.2s;
		}

		.video-item:hover {
			background: #242a33;
		}

		.video-info {
			flex: 1;
		}

		.video-name {
			font-weight: 500;
			margin-bottom: 5px;
		}

		.video-meta {
			font-size: 12px;
			color: #888;
		}

		.video-actions {
			display: flex;
			gap: 10px;
		}

		button {
			padding: 8px 16px;
			background: #2563eb;
			color: white;
			border: none;
			border-radius: 6px;
			cursor: pointer;
			font-size: 14px;
			transition: background 0.2s;
		}

		button:hover {
			background: #1d4ed8;
		}

		button:disabled {
			background: #555;
			cursor: not-allowed;
		}

		.btn-danger {
			background: #dc2626;
		}

		.btn-danger:hover {
			background: #b91c1c;
		}

		.empty-state {
			text-align: center;
			padding: 40px 20px;
			color: #666;
		}

		.loading {
			text-align: center;
			padding: 20px;
			color: #666;
		}

		.error {
			background: #7f1d1d;
			border: 1px solid #991b1b;
			color: #fca5a5;
			padding: 15px;
			border-radius: 6px;
			margin-bottom: 20px;
		}

		.auth-modal {
			display: none;
			position: fixed;
			top: 0;
			left: 0;
			right: 0;
			bottom: 0;
			background: rgba(0, 0, 0, 0.7);
			z-index: 1000;
			justify-content: center;
			align-items: center;
		}

		.auth-modal.active {
			display: flex;
		}

		.auth-form {
			background: #1a1f26;
			border: 1px solid #333;
			border-radius: 8px;
			padding: 30px;
			width: 90%;
			max-width: 400px;
		}

		.auth-form h2 {
			margin-bottom: 20px;
		}

		.form-group {
			margin-bottom: 20px;
		}

		.form-group label {
			display: block;
			margin-bottom: 8px;
			font-size: 14px;
			color: #888;
		}

		.form-group input {
			width: 100%;
			padding: 10px;
			background: #0f1419;
			border: 1px solid #333;
			border-radius: 6px;
			color: #e0e0e0;
			font-size: 14px;
		}

		.form-group input:focus {
			outline: none;
			border-color: #2563eb;
		}

		.player-container {
			background: #1a1f26;
			border: 1px solid #333;
			border-radius: 8px;
			padding: 20px;
			margin-bottom: 20px;
			min-height: 500px;
			display: flex;
			justify-content: center;
			align-items: center;
			overflow: hidden;
		}

		.stream-viewer {
			width: 100%;
			height: auto;
			max-width: 100%;
			max-height: 90%;
			display: block;
			background: #000;
			border-radius: 4px;
			object-fit: contain;
		}
	</style>
</head>
<body>
	<div class="container">
		<header>
			<h1>📹 Pi DashCam</h1>
			<div class="status-badge">
				<span class="status-dot"></span>
				<span id="statusText">Initializing...</span>
			</div>
		</header>

		<div id="errorBox"></div>

		<div class="stats-grid">
			<div class="stat-card">
				<div class="stat-label">Storage Used</div>
				<div class="stat-value" id="storageUsed">-- GB</div>
				<div class="storage-bar">
					<div class="storage-fill" id="storageFill" style="width: 0%"></div>
				</div>
				<div class="storage-text" id="storageText">-- GB / -- GB</div>
			</div>

			<div class="stat-card">
				<div class="stat-label">Videos Recorded</div>
				<div class="stat-value" id="videoCount">0</div>
				<div class="storage-text">Total videos stored</div>
			</div>

			<div class="stat-card">
				<div class="stat-label">Uptime</div>
				<div class="stat-value" id="uptime">--</div>
				<div class="storage-text">Since last boot</div>
			</div>
		</div>

		<div class="section">
			<div class="section-title">Live Stream</div>
			<div class="player-container" id="playerContainer">
				<p class="empty-state">Loading stream...</p>
			</div>
		</div>

		<div class="section">
			<div class="section-title">Recent Videos</div>
			<div id="videoList" class="video-list">
				<div class="loading">Loading videos...</div>
			</div>
		</div>
	</div>

	<div class="auth-modal" id="authModal">
		<div class="auth-form">
			<h2>Authentication Required</h2>
			<div class="form-group">
				<label>Auth Token</label>
				<input type="password" id="authToken" placeholder="Enter your auth token">
			</div>
			<button onclick="setAuthToken()">Connect</button>
		</div>
	</div>

	<script>
		// Get token from URL or localStorage
		let authToken = new URLSearchParams(window.location.search).get('token') || localStorage.getItem('authToken');

		async function setAuthToken() {
			const token = document.getElementById('authToken').value;
			if (token) {
				localStorage.setItem('authToken', token);
				authToken = token;
				document.getElementById('authModal').classList.remove('active');
				window.location.href = '?token=' + token;
			}
		}

		function showError(message) {
		const box = document.getElementById('errorBox');
		box.innerHTML = '<div class="error">' + message + '</div>';
		setTimeout(() => {
		box.innerHTML = '';
		}, 5000);
		}

		async function apiCall(endpoint, options = {}) {
			const headers = options.headers || {};
			if (authToken) {
				headers['Authorization'] = 'Bearer ' + authToken;
			}

			const url = new URL(endpoint, window.location.origin);
			if (authToken && !endpoint.includes('?')) {
				url.searchParams.set('token', authToken);
			}

			const response = await fetch(url, {
				method: options.method || 'GET',
				headers,
				...options
			});

			if (response.status === 401) {
				document.getElementById('authModal').classList.add('active');
				throw new Error('Unauthorized');
			}

			if (!response.ok) {
			throw new Error('API error: ' + response.status);
			}

			return await response.json();
		}

		async function loadStatus() {
			try {
				const data = await apiCall('/api/status');
				
				// Update stats
				if (!data || !data.storage || !data.videos) {
					throw new Error('Invalid status response');
				}
				document.getElementById('storageUsed').textContent = data.storage.used_gb.toFixed(2) + ' GB';
				document.getElementById('storageText').textContent = 
					data.storage.used_gb.toFixed(2) + ' GB / ' + data.storage.cap_gb + ' GB';
				document.getElementById('videoCount').textContent = data.videos.length || 0;
				document.getElementById('uptime').textContent = data.uptime;
				document.getElementById('statusText').textContent = 'Recording';

				// Update storage bar
				const fill = document.getElementById('storageFill');
				const percent = data.storage.percent;
				fill.style.width = percent + '%';
				fill.classList.remove('warning', 'critical');
				if (percent > 90) fill.classList.add('critical');
				else if (percent > 75) fill.classList.add('warning');

				// Load videos
				loadVideos();
			} catch (err) {
				console.error('Failed to load status:', err);
				showError('Failed to connect to dashcam');
			}
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
				(video.size / (1024 * 1024)).toFixed(2) + ' MB • ' + Math.floor(video.duration / 60) + ' min • ' +
				new Date(video.mod_time).toLocaleString() +
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

		function downloadVideo(filename) {
		const url = '/api/video/download?file=' + filename + '&token=' + authToken;
		const a = document.createElement('a');
		a.href = url;
		a.download = filename;
		a.click();
		}

		function loadStream() {
		const container = document.getElementById('playerContainer');
		container.innerHTML = '<img id="live-stream" class="stream-viewer" src="" alt="Live stream">';
		
		const img = document.getElementById('live-stream');
		
		// Poll for frames every 500ms (2 FPS for preview)
		let lastUpdate = 0;
		setInterval(() => {
			const now = Date.now();
			if (now - lastUpdate < 500) return;
			lastUpdate = now;
			
			const url = '/api/stream/frame?token=' + authToken + '&t=' + now;
			img.src = url;
		}, 100);
		}

		// Initial load
		if (!authToken) {
			document.getElementById('authModal').classList.add('active');
		} else {
			loadStatus();
			loadStream();
			setInterval(loadStatus, 5000); // Update every 5 seconds
		}
	</script>
</body>
</html>`
}
