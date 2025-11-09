# Performance Improvements for Pi-Dashcam

## Changes Made

### 1. **Increased Frame Rate** (24 FPS → 30 FPS)
   - **File**: `config.json`, `config.go`
   - **Change**: Updated `video_fps` from 24 to 30
   - **Impact**: 25% increase in recording smoothness and file quality

### 2. **Improved MJPEG Quality** (Quality 5 → 3)
   - **File**: `config.json`, `config.go`
   - **Change**: Lowered `mjpeg_quality` from 5 to 3 (lower = better quality)
   - **Impact**: Higher quality frames with better detail

### 3. **Faster Frame Extraction** (500ms → 33ms)
   - **File**: `camera.go`
   - **Change**: Reduced frame extraction interval from 500ms to 33ms (~30 FPS)
   - **Impact**: Much more responsive live preview with significantly reduced latency

### 4. **Optimized Frame Extraction**
   - **File**: `camera.go`
   - **Changes**:
     - Increased buffer size from 1MB to 2MB for higher quality frame extraction
     - Fixed JPEG end marker inclusion (now includes both bytes)
     - Added frame deduplication to avoid sending identical frames
   - **Impact**: Better quality frames and reduced bandwidth usage

### 5. **Added FFmpeg Auto-Overwrite Flag**
   - **File**: `camera.go`
   - **Changes**: Added `-y` flag to automatically overwrite existing files
   - **Impact**: Prevents recording interruptions when restarting

### 6. **Frontend Polling Optimization** (100ms → 50ms)
   - **File**: `frontend.go`
   - **Changes**:
     - Reduced polling interval from 100ms to 50ms (~20 FPS display rate)
     - Added image preloading to prevent flickering
     - Better error handling
   - **Impact**: Smoother live preview with less perceived latency

### 7. **Added MJPEG Multipart Streaming** (NEW!)
   - **File**: `server.go`, `frontend.go`
   - **Changes**:
     - Added new `/api/stream/mjpeg` endpoint for continuous MJPEG streaming
     - Frontend now uses native MJPEG streaming instead of polling
     - Automatic fallback to polling if streaming fails
   - **Impact**: 
     - **Significantly** reduced latency (no polling overhead)
     - More efficient bandwidth usage
     - Native browser support for smooth playback
     - ~30 FPS stream rate

## Expected Results

### Before:
- **Recording FPS**: 24 FPS
- **Stream Update Rate**: ~2 FPS (500ms polling)
- **Live Preview Latency**: 1-2 seconds
- **Video Quality**: Moderate

### After:
- **Recording FPS**: 30 FPS (+25%)
- **Recording Resolution**: 1920x1080 (up from 1280x720) 
- **Stream Update Rate**: ~25-30 FPS (MJPEG streaming from 720p preview)
- **Live Preview Latency**: 200-500ms (-60-75%)
- **Video Quality**: High quality 1080p for storage, optimized 720p for streaming

## Dual Resolution Recording

### 8. **High-Resolution Storage + Low-Resolution Streaming** (NEW!)
   - **Files**: `camera.go`, `config.go`, `config.json`
   - **Changes**:
     - **Storage**: Records at 1920x1080 @ quality 3 (high quality for archival)
     - **Streaming**: Extracts frames from 1280x720 @ quality 5 (lower res/quality for fast live preview)
     - Uses FFmpeg dual output for efficient capture
     - Preview file stored in `/tmp` (tmpfs) for fast I/O
   - **Impact**: 
     - High-quality archival footage (1080p)
     - Faster, smoother live preview (720p at ~20-30 FPS)
     - Better storage vs. performance balance
     - Configurable via `video_res_*` and `stream_res_*` settings

## Container Optimizations (for Podman/Docker)

### 9. **Optimized Frame Extraction Buffer**
   - **File**: `camera.go`
   - **Changes**:
     - Reduced buffer size from 2MB to 512KB for faster I/O
     - Added search limit to avoid scanning entire buffer
     - Improved duplicate frame detection with stall warnings
     - Extracts from lower-resolution preview file for speed
   - **Impact**: Faster frame extraction with less disk I/O overhead

### 10. **Container Performance Tuning**
   - **File**: `docker-compose.yml`
   - **Changes**:
     - Added tmpfs mount for `/tmp` (100MB) for preview files
     - Increased shared memory to 256MB for FFmpeg
     - Added optional CPU and memory limit configuration
   - **Impact**: 
     - Preview files stored in RAM (very fast)
     - Permanent recordings stored on disk
     - Better I/O performance in containerized environments

## Testing Recommendations

1. **Test the live preview** to verify the latency reduction
2. **Check CPU usage** - higher FPS may increase CPU load
3. **Verify storage usage** - better quality may use more space (monitor with the increased quality setting)
4. **Test on slow networks** - MJPEG streaming uses more bandwidth than frame polling
5. **Monitor container logs** for frame stall warnings (indicates recording issues)

## Rollback Instructions

If you experience issues, you can revert to the previous settings:

1. **Reduce FPS**: Change `video_fps` back to 24 in `config.json`
2. **Lower quality**: Change `mjpeg_quality` back to 5 in `config.json`
3. **Disable MJPEG streaming**: In `frontend.go`, set `useMJPEGStream = false`
4. **Slow down polling**: In `camera.go`, change `33 * time.Millisecond` back to `500 * time.Millisecond`

## Additional Optimization Ideas

If you need even better performance:

1. **Hardware acceleration**: Use hardware MJPEG encoding if your camera supports it
   - Add `-input_format mjpeg` to ffmpeg command (already added)
   
2. **Adjust resolution**: 
   - **Storage**: Change `video_res_width`/`video_res_height` (1920x1080 default)
   - **Streaming**: Change `stream_res_width`/`stream_res_height` (1280x720 default)
   - For lower storage: Use 1280x720 for both
   - For better preview: Keep 1080p storage, use 960x540 stream
   
3. **Adjust quality**: Fine-tune `mjpeg_quality` between 2-4 for your needs
   - 2 = highest quality, most storage
   - 4 = good quality, balanced storage

4. **Frame rate options**:
   - 60 FPS: Ultra-smooth (high CPU/storage)
   - 30 FPS: Smooth (current setting, recommended)
   - 24 FPS: Standard (previous setting)
   - 15 FPS: Low latency preview only

## Performance Tips

### Getting ~30 FPS instead of ~18 FPS:

The current implementation extracts frames from the MJPEG file being written, which is limited by disk I/O. To get closer to 30 FPS:

1. **Use MJPEG multipart streaming** (enabled by default in frontend):
   - The `/api/stream/mjpeg` endpoint streams frames directly
   - Should achieve closer to 25-30 FPS
   - Check browser console if it falls back to polling

2. **If using polling mode**, the ~18 FPS is expected due to:
   - File I/O overhead (reading from disk)
   - Frame extraction processing time
   - Container I/O performance

3. **For true 30 FPS**, consider:
   - Using FFmpeg's tee muxer to output to both file and pipe simultaneously
   - Reading from a named pipe instead of a file
   - Hardware-accelerated encoding (if available)

### Current Performance Baseline:
- **Local machine**: ~18-20 FPS (polling mode)
- **Expected with MJPEG streaming**: 25-30 FPS
- **Container performance**: Similar to local with tmpfs optimization

## Notes

- The Docker/Podman container needs to be rebuilt to apply these changes: `podman compose up --build`
- Monitor system resources (CPU, memory, storage) after deploying
- The MJPEG multipart streaming (`/api/stream/mjpeg`) is the most significant improvement for latency and FPS
- The tmpfs mount improves container I/O performance significantly
- Check logs for "Frame stall detected" warnings which indicate recording issues
