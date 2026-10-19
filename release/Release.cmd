@echo off
rem Release.cmd: one click - every release gate, then commit and push (Stage 109.1)
pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\Invoke-Release.ps1" %*
echo.
pause
