$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot 'crunchyroll-workstation')
try {
    go build -mod=vendor -trimpath -ldflags '-s -w -H=windowsgui' -o 'Crunchyroll Downloader.exe' './cmd/crunchyroll'
    if ($LASTEXITCODE -ne 0) { throw 'Fallo al compilar la aplicaciÃ³n' }
    New-Item -ItemType Directory -Path assets -Force | Out-Null
    Push-Location ..\crunchyroll-downloader
    try {
        go build -mod=mod -trimpath -tags workstationworker -ldflags '-s -w' -o '..\crunchyroll-workstation\assets\crunchyroll-engine.exe' .
        if ($LASTEXITCODE -ne 0) { throw 'Fallo al compilar el motor' }
    } finally { Pop-Location }
} finally { Pop-Location }
