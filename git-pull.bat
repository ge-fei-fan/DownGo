@echo off
setlocal
cd /d "%~dp0"
set "GIT_CMD=git -c safe.directory=%CD:\=/%"

echo [DownGo] Checking repository...
%GIT_CMD% rev-parse --is-inside-work-tree >nul 2>&1
if errorlevel 1 (
  echo Error: current directory is not a git repository.
  pause
  exit /b 1
)

echo [DownGo] Current status:
%GIT_CMD% status --short --branch

echo.
echo [DownGo] Pulling latest changes...
%GIT_CMD% pull --ff-only
if errorlevel 1 (
  echo.
  echo Error: git pull failed.
  pause
  exit /b 1
)

echo.
echo [DownGo] Pull completed.
pause
