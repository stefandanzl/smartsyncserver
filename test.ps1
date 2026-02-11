# Test script for SmartSyncServer (PowerShell)

$ErrorActionPreference = "Stop"

Write-Host "=== SmartSyncServer Test Script ===" -ForegroundColor Cyan
Write-Host ""

# Build the server
Write-Host "1. Building server..." -ForegroundColor Yellow
go build -o build/smartsyncserver.exe .
if ($LASTEXITCODE -ne 0) {
    Write-Host "   Build failed!" -ForegroundColor Red
    exit 1
}
Write-Host "   Build complete" -ForegroundColor Green
Write-Host ""

# Start the server in background
Write-Host "2. Starting server..." -ForegroundColor Yellow
$server = Start-Process -FilePath "./build/smartsyncserver.exe" -PassThru -WindowStyle Hidden
Write-Host "   Server started (PID: $($server.Id))" -ForegroundColor Green
Write-Host ""

# Wait for server to be ready
Write-Host "3. Waiting for server to be ready..." -ForegroundColor Yellow
Start-Sleep -Seconds 2
Write-Host ""

# Test status endpoint (no auth required)
Write-Host "4. Testing GET /status..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "http://127.0.0.1:8080/status" -Method Get
    Write-Host "   $response" -ForegroundColor Green
} catch {
    Write-Host "   Failed: $_" -ForegroundColor Red
}
Write-Host ""

# Test file upload
Write-Host "5. Testing PUT /file/notes/test.md..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "http://127.0.0.1:8080/file/notes/test.md" `
        -Method Put `
        -ContentType "text/plain" `
        -Body "This is a test file from PowerShell"
    Write-Host "   $response" -ForegroundColor Green
} catch {
    Write-Host "   Failed: $_" -ForegroundColor Red
}
Write-Host ""

# Test file download
Write-Host "6. Testing GET /file/notes/welcome.md..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "http://127.0.0.1:8080/file/notes/welcome.md" -Method Get
    Write-Host "   Downloaded $($response.Length) bytes" -ForegroundColor Green
} catch {
    Write-Host "   Failed: $_" -ForegroundColor Red
}
Write-Host ""

# Test folder creation
Write-Host "7. Testing PUT /folder/notes/test-folder..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "http://127.0.0.1:8080/folder/notes/test-folder" -Method Put
    Write-Host "   $response" -ForegroundColor Green
} catch {
    Write-Host "   Failed: $_" -ForegroundColor Red
}
Write-Host ""

# Test rename
Write-Host "8. Testing POST /rename..." -ForegroundColor Yellow
try {
    $body = @{
        type = "file"
        from = "notes/welcome.md"
        to = "notes/renamed.md"
    } | ConvertTo-Json
    $response = Invoke-RestMethod -Uri "http://127.0.0.1:8080/rename" `
        -Method Post `
        -ContentType "application/json" `
        -Body $body
    Write-Host "   $response" -ForegroundColor Green
} catch {
    Write-Host "   Failed: $_" -ForegroundColor Red
}
Write-Host ""

# Final status check
Write-Host "9. Final status check..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "http://127.0.0.1:8080/status" -Method Get
    Write-Host "   $response" -ForegroundColor Green
} catch {
    Write-Host "   Failed: $_" -ForegroundColor Red
}
Write-Host ""

# Cleanup
Write-Host "10. Stopping server..." -ForegroundColor Yellow
Stop-Process -Id $server.Id -ErrorAction SilentlyContinue
Write-Host "   Done" -ForegroundColor Green
Write-Host ""

Write-Host "=== Tests complete ===" -ForegroundColor Cyan
