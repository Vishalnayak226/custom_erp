@echo off
rem Deploy.cmd: one click - put the released commit on production (Stage 109.2)
pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0deploy\deploy_release.ps1" %*
echo.
pause
