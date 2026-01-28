# ArcheFriend Build Script
# PowerShell version with auto-zip support

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   ArcheFriend Build Script" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Configurações
$BUILD_DIR = "archefriend-release"
$EXE_NAME = "archefriend.exe"
$ZIP_NAME = "archefriend-release.zip"

# Remove pasta de build antiga se existir
if (Test-Path $BUILD_DIR) {
    Write-Host "Removendo build anterior..." -ForegroundColor Yellow
    Remove-Item -Recurse -Force $BUILD_DIR
}

# Remove zip antigo se existir
if (Test-Path $ZIP_NAME) {
    Write-Host "Removendo zip anterior..." -ForegroundColor Yellow
    Remove-Item -Force $ZIP_NAME
}

# Cria pasta de build
Write-Host "Criando pasta de build..." -ForegroundColor Green
New-Item -ItemType Directory -Path $BUILD_DIR | Out-Null

# Compila o projeto
Write-Host ""
Write-Host "Compilando projeto..." -ForegroundColor Green
$env:CGO_ENABLED = "0"
go build -ldflags="-s -w" -o "$BUILD_DIR\$EXE_NAME" main.go

if ($LASTEXITCODE -ne 0) {
    Write-Host ""
    Write-Host "[ERRO] Falha na compilacao!" -ForegroundColor Red
    Read-Host "Pressione Enter para sair"
    exit 1
}

# Copia arquivos JSON
Write-Host ""
Write-Host "Copiando arquivos de configuracao..." -ForegroundColor Green

$jsonFiles = @("reactions.json", "buff_presets.json", "skill_reactions.json", "aimbot_config.json")
foreach ($file in $jsonFiles) {
    if (Test-Path $file) {
        Copy-Item $file "$BUILD_DIR\" -Force
        Write-Host "  - $file copiado" -ForegroundColor Gray
    } else {
        Write-Host "  - $file nao encontrado (pulando)" -ForegroundColor Yellow
    }
}

# Cria README
Write-Host ""
Write-Host "Criando README..." -ForegroundColor Green
$readme = @"
ArcheFriend - Assistente para ArcheAge
=======================================

Como usar:
1. Execute archefriend.exe
2. Pressione F7 para abrir configuracoes de reacoes
3. Pressione F8 para abrir gerenciador de buffs
4. Pressione F9 para acesso rapido

Teclas de atalho:
- F1: Loot bypass
- F2: Doodad bypass
- F3: Auto spam
- F4: Auto tudo
- F5: Reload configs
- F6: Toggle reactions
- F7: Config window
- F8: Buff window
- F9: Quick access
- END: Hide/Show overlay

Configuracao:
- reactions.json: Reacoes de buffs/debuffs
  * Suporta multiplas sequencias de teclas (use & ou ,)
  * Exemplo: "ALT+Q & ALT+E" executa ALT+Q e depois ALT+E
  * Campos: onStart (quando ganha), onEnd (quando perde)

- buff_presets.json: Presets de buffs para monitoramento

- skill_reactions.json: Reacoes de skills
  * Configura teclas a pressionar quando uma skill e usada
  * Suporte a aimbot antes ou durante o cast

Desenvolvido com Win32 API puro em Go
"@

Set-Content -Path "$BUILD_DIR\README.txt" -Value $readme -Encoding UTF8

# Lista arquivos criados
Write-Host ""
Write-Host "Arquivos no build:" -ForegroundColor Cyan
Get-ChildItem $BUILD_DIR | ForEach-Object {
    $size = if ($_.PSIsContainer) { "DIR" } else { "{0:N0} KB" -f ($_.Length / 1KB) }
    Write-Host "  - $($_.Name) ($size)" -ForegroundColor Gray
}

# Pergunta se quer zipar
Write-Host ""
$zip = Read-Host "Deseja criar arquivo ZIP? (S/N)"

if ($zip -eq "S" -or $zip -eq "s") {
    Write-Host ""
    Write-Host "Criando arquivo ZIP..." -ForegroundColor Green
    Compress-Archive -Path "$BUILD_DIR\*" -DestinationPath $ZIP_NAME -Force

    $zipSize = (Get-Item $ZIP_NAME).Length / 1KB
    Write-Host "ZIP criado: $ZIP_NAME ({0:N0} KB)" -f $zipSize -ForegroundColor Green
}

# Conclusão
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   Build concluido com sucesso!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Pasta: $BUILD_DIR" -ForegroundColor White
if (Test-Path $ZIP_NAME) {
    Write-Host "ZIP: $ZIP_NAME" -ForegroundColor White
}
Write-Host ""
Write-Host "Pronto para distribuicao!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

Read-Host "Pressione Enter para sair"
