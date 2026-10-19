@echo off
rem Cleanup.cmd: run the server cleanup now; it also runs daily at 03:30 on its own (Stage 109.3)
pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\Invoke-Cleanup.ps1" %*
echo.
pause
