go build -ldflags "-H=windowsgui" -o monitor.exe .
if (Test-Path monitor.exe) { Write-Host "Build successful!" } else { Write-Host "Build failed!" }
