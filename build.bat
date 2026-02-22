@echo off
setlocal enabledelayedexpansion

cd /d %~dp0

echo ========================================
echo   ArcheFriend Build Script
echo ========================================
echo.

set BUILD_DIR=archefriend-release
set EXE_NAME=archefriend.exe

REM Defaults: all features ON
set F_LOOT=true
set F_PATCHES=true
set F_REACTIONS=true
set F_BUFFS=true
set F_ESP=true
set F_BOT=true
set F_KEYSPAM=true

echo Selecione o modo de build:
echo.
echo   [A] All features (tudo ligado)
echo   [C] Custom (escolher features)
echo   [M] Minimal (so overlay + patches)
echo.
choice /c ACM /n /m "Escolha [A/C/M]: "

if %ERRORLEVEL%==1 goto :BUILD
if %ERRORLEVEL%==2 goto :CUSTOM
if %ERRORLEVEL%==3 goto :MINIMAL

:MINIMAL
set F_LOOT=false
set F_REACTIONS=false
set F_BUFFS=false
set F_ESP=false
set F_BOT=false
set F_KEYSPAM=false
goto :BUILD

:CUSTOM
echo.
echo Escolha as features (Y=sim, N=nao):
echo.

choice /c YN /n /m "  Loot/Doodad bypass?   [Y/N]: "
if %ERRORLEVEL%==2 set F_LOOT=false

choice /c YN /n /m "  Patches (mount+GCD)?  [Y/N]: "
if %ERRORLEVEL%==2 set F_PATCHES=false

choice /c YN /n /m "  Reactions (buff/deb)? [Y/N]: "
if %ERRORLEVEL%==2 set F_REACTIONS=false

choice /c YN /n /m "  Buff injector?        [Y/N]: "
if %ERRORLEVEL%==2 set F_BUFFS=false

choice /c YN /n /m "  ESP overlay?          [Y/N]: "
if %ERRORLEVEL%==2 set F_ESP=false

choice /c YN /n /m "  Bot?                  [Y/N]: "
if %ERRORLEVEL%==2 set F_BOT=false

choice /c YN /n /m "  Keyspam/autospam?     [Y/N]: "
if %ERRORLEVEL%==2 set F_KEYSPAM=false

goto :BUILD

:BUILD
echo.
echo ========================================
echo   Features selecionadas:
echo ========================================
echo   Loot:      %F_LOOT%
echo   Patches:   %F_PATCHES%
echo   Reactions: %F_REACTIONS%
echo   Buffs:     %F_BUFFS%
echo   ESP:       %F_ESP%
echo   Bot:       %F_BOT%
echo   Keyspam:   %F_KEYSPAM%
echo ========================================
echo.

REM Remove pasta de build antiga
if exist %BUILD_DIR% (
    echo Removendo build anterior...
    rmdir /s /q %BUILD_DIR%
)
mkdir %BUILD_DIR%

REM Montar ldflags
set LDFLAGS=-s -w
if "%F_LOOT%"=="false"      set LDFLAGS=%LDFLAGS% -X main.featureLoot=false
if "%F_PATCHES%"=="false"   set LDFLAGS=%LDFLAGS% -X main.featurePatches=false
if "%F_REACTIONS%"=="false" set LDFLAGS=%LDFLAGS% -X main.featureReactions=false
if "%F_BUFFS%"=="false"     set LDFLAGS=%LDFLAGS% -X main.featureBuffs=false
if "%F_ESP%"=="false"       set LDFLAGS=%LDFLAGS% -X main.featureESP=false
if "%F_BOT%"=="false"       set LDFLAGS=%LDFLAGS% -X main.featureBot=false
if "%F_KEYSPAM%"=="false"   set LDFLAGS=%LDFLAGS% -X main.featureKeyspam=false

echo Compilando projeto...
echo   ldflags: %LDFLAGS%
echo.
go build -ldflags "%LDFLAGS%" -o %BUILD_DIR%\%EXE_NAME% main.go

if %ERRORLEVEL% neq 0 (
    echo.
    echo [ERRO] Falha na compilacao!
    pause
    exit /b 1
)

REM Copia apenas os JSONs necessarios
echo.
echo Copiando configs...
copy settings.json %BUILD_DIR%\ >nul 2>&1
copy keybinds.json %BUILD_DIR%\ >nul 2>&1

if "%F_REACTIONS%"=="true" (
    copy reactions.json %BUILD_DIR%\ >nul 2>&1
)
if "%F_BUFFS%"=="true" (
    copy buff_presets.json %BUILD_DIR%\ >nul 2>&1
)
if "%F_ESP%"=="true" (
    copy aimbot_config.json %BUILD_DIR%\ >nul 2>&1
    copy houses.json %BUILD_DIR%\ >nul 2>&1
)
if "%F_BOT%"=="true" (
    copy bot_config.json %BUILD_DIR%\ >nul 2>&1
)
if "%F_REACTIONS%"=="true" (
    copy fishing_config.json %BUILD_DIR%\ >nul 2>&1
    copy skill_reactions.json %BUILD_DIR%\ >nul 2>&1
)

echo.
echo ========================================
echo   Build concluido com sucesso!
echo ========================================
echo.
echo Pasta: %BUILD_DIR%
echo Arquivos:
dir /b %BUILD_DIR%
echo.
echo ========================================
pause
