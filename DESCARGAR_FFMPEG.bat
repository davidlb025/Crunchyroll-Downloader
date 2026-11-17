@echo off
setlocal

set "ASSETS_DIR=%~dp0crunchyroll-workstation\assets"
set "TEMP_ZIP=%TEMP%\ffmpeg-release-essentials.zip"
set "TEMP_DIR=%TEMP%\ffmpeg-download"
set "DOWNLOAD_URL=https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"

echo Descargando FFmpeg para Windows...
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ErrorActionPreference='Stop'; " ^
  "Remove-Item -LiteralPath $env:TEMP_DIR -Recurse -Force -ErrorAction SilentlyContinue; " ^
  "Remove-Item -LiteralPath $env:TEMP_ZIP -Force -ErrorAction SilentlyContinue; " ^
  "Invoke-WebRequest -Uri $env:DOWNLOAD_URL -OutFile $env:TEMP_ZIP; " ^
  "Expand-Archive -LiteralPath $env:TEMP_ZIP -DestinationPath $env:TEMP_DIR -Force"
if errorlevel 1 goto download_failed

set "TEMP_FFMPEG="
set "TEMP_FFPROBE="
for /r "%TEMP_DIR%" %%F in (ffmpeg.exe) do if not defined TEMP_FFMPEG set "TEMP_FFMPEG=%%F"
for /r "%TEMP_DIR%" %%F in (ffprobe.exe) do if not defined TEMP_FFPROBE set "TEMP_FFPROBE=%%F"

if not defined TEMP_FFMPEG goto download_failed
if not defined TEMP_FFPROBE goto download_failed

if not exist "%ASSETS_DIR%" mkdir "%ASSETS_DIR%"
copy /y "%TEMP_FFMPEG%" "%ASSETS_DIR%\ffmpeg.exe" >nul
copy /y "%TEMP_FFPROBE%" "%ASSETS_DIR%\ffprobe.exe" >nul

echo FFmpeg instalado en "%ASSETS_DIR%"
echo.
dir /b "%ASSETS_DIR%\ffmpeg.exe" "%ASSETS_DIR%\ffprobe.exe"
goto cleanup

:download_failed
echo.
echo ERROR: No se pudieron descargar o encontrar ffmpeg.exe y ffprobe.exe.
echo Comprueba la conexion y vuelve a ejecutar este archivo.
exit /b 1

:cleanup
if exist "%TEMP_ZIP%" del /q "%TEMP_ZIP%"
if exist "%TEMP_DIR%" rmdir /s /q "%TEMP_DIR%"
endlocal
exit /b 0
