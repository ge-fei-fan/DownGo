@echo off
setlocal
cd /d "%~dp0"

set "OUTPUT_EXE=%CD%\DownGo.exe"
set "EXIT_CODE=0"

echo [DownGo] Checking build tools...
where go >nul 2>&1
if errorlevel 1 (
  echo Error: Go is not installed or not available in PATH.
  goto fail
)

where npm >nul 2>&1
if errorlevel 1 (
  echo Error: npm is not installed or not available in PATH.
  goto fail
)

if not exist "frontend\package.json" (
  echo Error: frontend\package.json was not found.
  goto fail
)

echo.
echo [DownGo] Installing frontend dependencies if needed...
if not exist "frontend\node_modules" (
  pushd frontend
  call npm install
  if errorlevel 1 (
    popd
    echo.
    echo Error: npm install failed.
    goto fail
  )
  popd
) else (
  echo [DownGo] frontend\node_modules already exists, skipping npm install.
)

echo.
echo [DownGo] Building frontend assets...
pushd frontend
call npm run build
if errorlevel 1 (
  popd
  echo.
  echo Error: frontend build failed.
  goto fail
)
popd

echo.
echo [DownGo] Running Go tests...
go test ./...
if errorlevel 1 (
  echo.
  echo Error: go test failed.
  goto fail
)

echo.
echo [DownGo] Building latest DownGo.exe...
if exist "%OUTPUT_EXE%" del /f /q "%OUTPUT_EXE%"
go build -buildvcs=false -ldflags="-H windowsgui" -o "%OUTPUT_EXE%" ./cmd/server
if errorlevel 1 (
  echo.
  echo Error: go build failed.
  goto fail
)

if not exist "%OUTPUT_EXE%" (
  echo.
  echo Error: build command finished, but output file was not found:
  echo %OUTPUT_EXE%
  goto fail
)

echo.
echo [DownGo] Build completed:
echo %OUTPUT_EXE%
goto done

:fail
set "EXIT_CODE=1"
echo.
echo [DownGo] Build failed. See messages above.
goto done

:done
echo.
echo Press any key to close this window...
pause >nul
exit /b %EXIT_CODE%
