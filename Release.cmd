@echo off
rem Release.cmd: one click - every release gate, then commit and push (Stage 109.1)
pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0deploy\release.ps1" %*
echo.
pause
