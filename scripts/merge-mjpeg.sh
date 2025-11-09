#!/bin/bash

# Merge all MJPEG files in a directory into a single video file
# Usage: ./merge-mjpeg.sh [directory] [output-file]
# On the same machine: ./scripts/merge-mjpeg.sh ~/.local/state/dash-of-pi merged_dashcam.mp4

VIDEO_DIR="${1:-$HOME/.local/state/dash-of-pi/videos}"
OUTPUT_FILE="${2:-merged_dashcam.mp4}"

# Expand ~ to home directory if used
VIDEO_DIR="${VIDEO_DIR/#\~/$HOME}"

# Remove trailing slash if present
VIDEO_DIR="${VIDEO_DIR%/}"

# Check if directory exists
if [ ! -d "$VIDEO_DIR" ]; then
    echo "Error: Directory does not exist: $VIDEO_DIR"
    exit 1
fi

# Find all MJPEG files
MJPEG_FILES=()
while IFS= read -r -d '' file; do
    MJPEG_FILES+=("$file")
done < <(find "$VIDEO_DIR" -maxdepth 1 -name "*.mjpeg" -print0 | sort -z)

if [ ${#MJPEG_FILES[@]} -eq 0 ]; then
    echo "Error: No MJPEG files found in $VIDEO_DIR"
    exit 1
fi

echo "Found ${#MJPEG_FILES[@]} MJPEG file(s)"
echo "Output: $OUTPUT_FILE"
echo ""

# Create concat file for ffmpeg
CONCAT_FILE=$(mktemp)
trap "rm -f $CONCAT_FILE" EXIT

for file in "${MJPEG_FILES[@]}"; do
    echo "file '$file'" >> "$CONCAT_FILE"
done

echo "Merging files with MPEG-4 encoding..."
echo "(This will take a while for large videos...)"
echo ""

# Use MPEG-4 encoding with high quality (same as server export)
# Matches server.go MP4 generation exactly
# -fflags +discardcorrupt = discard corrupted frames instead of warning
# -q:v 2 = quality 2 (lower=better, range 1-31)
# -r 30 = force 30 fps output
# -fps_mode cfr = constant framerate
# -loglevel error = only show actual errors, not format warnings
ffmpeg -y \
    -loglevel error \
    -fflags +discardcorrupt \
    -err_detect ignore_err \
    -f concat \
    -safe 0 \
    -i "$CONCAT_FILE" \
    -c:v mpeg4 \
    -q:v 2 \
    -r 30 \
    -fps_mode cfr \
    -movflags +faststart \
    -stats \
    -f mp4 \
    "$OUTPUT_FILE"

if [ $? -eq 0 ]; then
    SIZE=$(du -h "$OUTPUT_FILE" | cut -f1)
    echo ""
    echo "✓ Merge complete: $OUTPUT_FILE ($SIZE)"
else
    echo ""
    echo "✗ Merge failed"
    exit 1
fi
