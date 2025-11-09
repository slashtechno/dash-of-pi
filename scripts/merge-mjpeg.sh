#!/bin/bash

# Merge all MJPEG files in a directory into a single video file
# Usage: ./merge-mjpeg.sh [directory] [output-file]

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

echo "Merging files..."
ffmpeg -f concat -safe 0 -i "$CONCAT_FILE" -c copy -framerate 24 "$OUTPUT_FILE"

if [ $? -eq 0 ]; then
    SIZE=$(du -h "$OUTPUT_FILE" | cut -f1)
    echo ""
    echo "✓ Merge complete: $OUTPUT_FILE ($SIZE)"
else
    echo "✗ Merge failed"
    exit 1
fi
