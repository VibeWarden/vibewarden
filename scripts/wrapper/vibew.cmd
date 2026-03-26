@echo off
:: vibew.cmd — thin batch wrapper that calls vibew.ps1
::
:: Allows running `vibew <args>` from CMD or batch files on Windows.
:: Delegates all work to vibew.ps1; preserves exit code.

setlocal

set "SCRIPT_DIR=%~dp0"

powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%vibew.ps1" %*
exit /b %ERRORLEVEL%
