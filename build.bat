@echo off
echo ========================================
echo   ArcheFriend Build Script
echo ========================================
echo.

REM Nome da pasta de build
set BUILD_DIR=archefriend-release
set EXE_NAME=archefriend.exe

REM Remove pasta de build antiga se existir
if exist %BUILD_DIR% (
    echo Removendo build anterior...
    rmdir /s /q %BUILD_DIR%
)

REM Cria pasta de build
echo Criando pasta de build...
mkdir %BUILD_DIR%

REM Compila o projeto
echo.
echo Compilando projeto...
go build -o %BUILD_DIR%\%EXE_NAME% main.go

if %ERRORLEVEL% neq 0 (
    echo.
    echo [ERRO] Falha na compilacao!
    pause
    exit /b 1
)

REM Copia arquivos JSON
echo.
echo Copiando arquivos de configuracao...
copy reactions.json %BUILD_DIR%\ >nul 2>&1
copy buff_presets.json %BUILD_DIR%\ >nul 2>&1

REM Cria um README simples
echo.
echo Criando README...
(
echo ArcheFriend - Assistente para ArcheAge
echo =======================================
echo.
echo Como usar:
echo 1. Execute archefriend.exe
echo 2. Pressione F7 para abrir configuracoes de reacoes
echo 3. Pressione F8 para abrir gerenciador de buffs
echo 4. Pressione F9 para acesso rapido
echo.
echo Teclas de atalho:
echo - F1: Loot bypass
echo - F2: Doodad bypass
echo - F3: Auto spam
echo - F4: Auto tudo
echo - F5: Reload configs
echo - F6: Toggle reactions
echo - F7: Config window
echo - F8: Buff window
echo - F9: Quick access
echo - END: Hide/Show overlay
echo.
echo Configuracao:
echo - reactions.json: Reacoes de buffs/debuffs
echo - buff_presets.json: Presets de buffs
echo.
) > %BUILD_DIR%\README.txt

echo.
echo ========================================
echo   Build concluido com sucesso!
echo ========================================
echo.
echo Pasta: %BUILD_DIR%
echo Arquivos:
dir /b %BUILD_DIR%
echo.
echo Pronto para ser zipado!
echo ========================================
pause
