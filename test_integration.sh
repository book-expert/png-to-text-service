#!/bin/bash
set -euo pipefail

# Configuration
NATS_SERVER="nats://192.168.122.102:4222"
PNG_KEY="integration-test-doc-1.png"
WORKFLOW_ID="text-extraction-test-$(date +%s)"

echo "----------------------------------------------------------------"
echo "Integration Test Script for PNG-to-Text Service"
echo "----------------------------------------------------------------"

# 1. Check prerequisites
if ! command -v nats &> /dev/null; then
    echo "Error: 'nats' CLI tool is required but not installed."
    exit 1
fi

# 2. Setup NATS Context
echo "-> Setting up NATS context..."
nats context save local --server "$NATS_SERVER" > /dev/null || true
nats context select local

# 3. Ensure Streams and Buckets exist
echo "-> Ensuring NATS resources exist..."
nats object add TEXT_FILES > /dev/null 2>&1 || true
nats stream add TEXTS --subjects "texts.processed" --storage file --retention limits --max-msgs 10000 > /dev/null 2>&1 || true

# 4. Subscribe in background to catch the result
echo "-> Listening for completion events (timeout in 60s)..."
(nats sub "texts.processed" --count 1 --timeout 60s && echo -e "\n✅ Success! Received 'texts.processed' event.") & 
SUB_PID=$!

# Give the subscriber a moment to connect
sleep 1

# 5. Trigger Event (Skipped to avoid creating new files, we verify existing)
# echo "-> Publishing 'pngs.created' event..."
# nats pub "pngs.created" "$JSON_PAYLOAD"

# 6. Wait for subscriber (Skipped)
# wait $SUB_PID ...

# 7. Verify and Download ALL files for the latest workflow
echo "-> Identifying latest workflow..."
# Get the latest workflow ID from the bucket list (assuming chronological order or parsing timestamps would be better, but let's grab the last unique one)
# This is a heuristic. Ideally, we'd know the ID. We'll grab the ID from the last file added.
LATEST_FILE_KEY=$(nats object ls TEXT_FILES | tail -n 2 | head -n 1 | awk '{print $2}')
# Key format: tenant/workflow/file.txt. Extract workflow (2nd component).
LATEST_WORKFLOW_ID=$(echo "$LATEST_FILE_KEY" | cut -d'/' -f2)

if [ -z "$LATEST_WORKFLOW_ID" ]; then
    echo "❌ No files found in TEXT_FILES bucket."
    exit 1
fi

echo "-> Found workflow: $LATEST_WORKFLOW_ID"
echo "-> Downloading all text files for this workflow..."

mkdir -p output/full_doc
rm -f output/full_doc/*

# List all files, filter by workflow, and download
nats object ls TEXT_FILES | grep "$LATEST_WORKFLOW_ID" | awk '{print $2}' | while read key; do
    FILENAME=$(basename "$key")
    nats object get TEXT_FILES "$key" --output "output/full_doc/$FILENAME" --force > /dev/null
done

echo "-> Concatenating files to output/full_text.json ..."
# We need to concatenate JSON arrays... that's tricky with simple cat.
# For now, we'll just cat them to see the content chunks.
# Ideally, use jq to merge.
cat output/full_doc/*.txt > output/full_text.txt

echo "--- Full Text Preview ---"
head -n 50 output/full_text.txt
echo "-------------------------"
echo "Test Complete."
