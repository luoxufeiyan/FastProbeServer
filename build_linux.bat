@echo off
echo Building FastProbeServer for Linux (amd64)...

set GOOS=linux
set GOARCH=amd64

FOR /F "tokens=*" %%g IN ('git rev-parse --short HEAD') do (SET VERSION=%%g)
if "%VERSION%"=="" (SET VERSION=dev)

go build -ldflags "-X FastProbeServer/config.Version=%VERSION%" -o index.fcgi .

if %errorlevel% neq 0 (
    echo Build failed!
    exit /b %errorlevel%
)

echo Build complete: index.fcgi
