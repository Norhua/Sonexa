@echo off
setlocal
rem Requires Go, Node/npm, Python 3.14+ and native Wails toolchain dependencies.
where py >nul 2>nul
if errorlevel 1 goto python
py -3 -I "%~dp0scripts\build.py" %*
exit /b %errorlevel%
:python
where python >nul 2>nul
if errorlevel 1 (
    echo Python 3.14+ is required for building. See build/README.md.
    exit /b 1
)
python -I "%~dp0scripts\build.py" %*
exit /b %errorlevel%
