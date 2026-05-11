@echo off
setlocal
cd /d "%~dp0"

echo [DownGo] Checking build tools...
where go >nul 2>&1
if errorlevel 1 (
  echo Error: Go is not installed or not available in PATH.
  pause
  exit /b 1
)

where npm >nul 2>&1
if errorlevel 1 (
  echo Error: npm is not installed or not available in PATH.
  pause
  exit /b 1
)

if not exist "frontend\package.json" (
  echo Error: frontend\package.json was not found.
  pause
  exit /b 1
)

echo.
echo [DownGo] Installing frontend dependencies if needed...
if not exist "frontend\node_modules" (
  pushd frontend
  npm install
  if errorlevel 1 (
    popd
    echo.
    echo Error: npm install failed.
    pause
    exit /b 1
  )
  popd
) else (
  echo [DownGo] frontend\node_modules already exists, skipping npm install.
)

echo.
echo [DownGo] Building frontend assets...
pushd frontend
npm run build
if errorlevel 1 (
  popd
  echo.
  echo Error: frontend build failed.
  pause
  exit /b 1
)
popd

echo.
echo [DownGo] Running Go tests...
go test ./...
if errorlevel 1 (
  echo.
  echo Error: go test failed.
  pause
  exit /b 1
)

echo.
echo [DownGo] Building latest DownGo.exe...
go build -buildvcs=false -ldflags="-H windowsgui" -o DownGo.exe ./cmd/server
if errorlevel 1 (
  echo.
  echo Error: go build failed.
  pause
  exit /b 1
)

echo.
echo [DownGo] Build completed: %CD%\DownGo.exe
pause
