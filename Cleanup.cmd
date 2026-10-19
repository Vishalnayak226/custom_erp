@echo off
rem Cleanup.cmd: run the server cleanup now; it also runs daily at 03:30 on its own (Stage 109.3)
pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0deploy\cleanup_now.ps1" %*
echo.
pause
