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
echo [DownGo] Pulling latest changes before push...
%GIT_CMD% pull --ff-only
if errorlevel 1 (
  echo.
  echo Error: git pull failed. Resolve the issue before pushing.
  pause
  exit /b 1
)

echo.
echo [DownGo] Staging all local changes...
%GIT_CMD% add -A
if errorlevel 1 (
  echo.
  echo Error: git add failed.
  pause
  exit /b 1
)

%GIT_CMD% diff --cached --quiet
if not errorlevel 1 (
  echo.
  echo [DownGo] No local changes to commit.
  echo [DownGo] Pushing current branch anyway...
  %GIT_CMD% push origin HEAD
  if errorlevel 1 (
    echo.
    echo Error: git push failed.
    pause
    exit /b 1
  )
  echo.
  echo [DownGo] Push completed.
  pause
  exit /b 0
)

echo.
set /p COMMIT_MSG=Enter commit message: 
if "%COMMIT_MSG%"=="" (
  echo Error: commit message cannot be empty.
  pause
  exit /b 1
)

echo.
echo [DownGo] Creating commit...
%GIT_CMD% commit -m "%COMMIT_MSG%"
if errorlevel 1 (
  echo.
  echo Error: git commit failed.
  pause
  exit /b 1
)

echo.
echo [DownGo] Pushing current branch to origin...
%GIT_CMD% push origin HEAD
if errorlevel 1 (
  echo.
  echo Error: git push failed.
  pause
  exit /b 1
)

echo.
echo [DownGo] Push completed.
pause
