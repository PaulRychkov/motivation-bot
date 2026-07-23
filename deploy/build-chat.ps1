$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot

Write-Host "== chat frontend =="
Push-Location "$repo\chat-desktop\frontend"
npm install
npm run build
Pop-Location

Write-Host "== chat-service webdist =="
$webdist = "$repo\cmd\chat-service\webdist"
New-Item -ItemType Directory -Force $webdist | Out-Null
Remove-Item "$webdist\assets" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item "$webdist\index.html" -Force -ErrorAction SilentlyContinue
Copy-Item "$repo\chat-desktop\frontend\dist\*" $webdist -Recurse -Force

Write-Host "== chat-service (linux/amd64, для VM) =="
$env:GOOS = "linux"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
go build -o "$repo\cmd\chat-service\chat-service.bin" ./cmd/chat-service
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED

Write-Host "== Windows chat (Wails) =="
Push-Location "$repo\chat-desktop"
& "$env:USERPROFILE\go\bin\wails.exe" build
Pop-Location

Write-Host "Готово:"
Write-Host "  chat-service бинарь:  $repo\cmd\chat-service\chat-service.bin (для VM)"
Write-Host "  Windows-чат:          $repo\chat-desktop\build\bin\motivator-chat.exe"
Write-Host ""
Write-Host "Деплой chat-service на VM: scp chat-service.bin + Dockerfile.prebuilt, docker compose up -d chat-service"
