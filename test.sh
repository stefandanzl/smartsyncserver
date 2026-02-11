#!/bin/bash
# Test script for SmartSyncServer

set -e

echo "=== SmartSyncServer Test Script ==="
echo ""

# Build the server
echo "1. Building server..."
cd "$(dirname "$0")"
go build -o build/smartsyncserver.exe . || exit 1
echo "   Build complete"
echo ""

# Start the server in background
echo "2. Starting server..."
./build/smartsyncserver.exe &
SERVER_PID=$!
echo "   Server started (PID: $SERVER_PID)"
echo ""

# Wait for server to be ready
echo "3. Waiting for server to be ready..."
sleep 2
echo ""

# Test status endpoint (no auth required)
echo "4. Testing GET /status..."
curl -s http://127.0.0.1:8080/status | jq '.' || echo "   Failed (is jq installed?)"
echo ""

# Test file upload (requires auth if enabled)
echo "5. Testing PUT /file/notes/test.md..."
curl -s -X PUT http://127.0.0.1:8080/file/notes/test.md \
  -H "Content-Type: text/plain" \
  -d "This is a test file from curl"
echo "   Upload complete"
echo ""

# Test file download
echo "6. Testing GET /file/notes/welcome.md..."
curl -s http://127.0.0.1:8080/file/notes/welcome.md
echo ""
echo "7. Testing folder creation..."
curl -s -X PUT http://127.0.0.1:8080/folder/notes/test-folder
echo ""
echo "8. Testing rename..."
curl -s -X POST http://127.0.0.1:8080/rename \
  -H "Content-Type: application/json" \
  -d '{"type":"file","from":"notes/welcome.md","to":"notes/renamed.md"}'
echo ""

# Show status again
echo "9. Final status check..."
curl -s http://127.0.0.1:8080/status | jq '.' || echo "   Failed (is jq installed?)"
echo ""

# Cleanup
echo "10. Stopping server..."
kill $SERVER_PID 2>/dev/null || true
echo "   Done"
echo ""
echo "=== Tests complete ==="
