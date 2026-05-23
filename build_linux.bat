@echo off
echo Building FastProbeServer for Linux (amd64)...

set GOOS=linux
set GOARCH=amd64

go build -o index.fcgi .

if %errorlevel% neq 0 (
    echo Build failed!
    exit /b %errorlevel%
)

echo Build complete: index.fcgi
