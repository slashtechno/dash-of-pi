// Live MJPEG-ish stream: polls /api/stream/frame and swaps an <img>.
import { state } from './state.js';

export function startStream() {
	const c = document.getElementById('playerContainer');
	c.innerHTML = '<div class="rec-pill"><span class="dot"></span>LIVE</div><img id="liveStream" class="stream-viewer" alt="Live stream">';
	const img = document.getElementById('liveStream');
	let loading = false;

	if (state.streamInterval) clearInterval(state.streamInterval);
	state.streamInterval = setInterval(() => {
		if (loading) return;
		loading = true;
		const cam = state.streamCameraId ? `&camera=${encodeURIComponent(state.streamCameraId)}` : '';
		const next = new Image();
		next.onload = () => { img.src = next.src; loading = false; };
		next.onerror = () => { loading = false; };
		next.src = `/api/stream/frame?token=${state.authToken}${cam}&t=${Date.now()}`;
	}, 40);
}

export function switchStreamCamera() {
	state.streamCameraId = document.getElementById('streamCamera').value;
}