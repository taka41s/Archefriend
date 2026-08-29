# Build "limpo": nome de binario ALEATORIO, 32-bit (386), sem nenhuma string
# "archefriend" no binario. A guarda no final ESCANEIA o exe e ABORTA o build
# (apagando o output) se qualquer token proibido aparecer — de forma que essas
# strings nunca possam vazar, mesmo que alguem reintroduza uma no futuro.
#
# Uso:
#   .\build_random.ps1                 # nome aleatorio, pasta "dist"
#   .\build_random.ps1 -Name Foo       # nome fixo "Foo.exe"
#   .\build_random.ps1 -OutDir release # pasta de saida customizada

param(
    [string]$Name = "",
    [string]$OutDir = "dist"
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

# Tokens que NUNCA podem aparecer no binario final (case-insensitive).
$ForbiddenTokens = @("archefriend")

# --- Gera um nome pronunciavel aleatorio (ex: "Kalore7f", "Bimuta3c") ---
function New-RandomName {
    $vowels = 'aeiou'.ToCharArray()
    $cons   = 'bcdfghjklmnprstvwz'.ToCharArray()
    $sb = ''
    $syl = Get-Random -Minimum 3 -Maximum 5   # 3-4 silabas
    for ($i = 0; $i -lt $syl; $i++) {
        $sb += $cons[(Get-Random -Maximum $cons.Count)]
        $sb += $vowels[(Get-Random -Maximum $vowels.Count)]
    }
    $suffix = -join ((Get-Random -Count 3 -InputObject ([char[]]'0123456789abcdef')))
    return ($sb.Substring(0,1).ToUpper() + $sb.Substring(1) + $suffix)
}

# --- Guarda: falha o build se algum token proibido estiver no binario ---
function Assert-CleanBinary {
    param([string]$Path, [string[]]$Tokens)
    $bytes = [System.IO.File]::ReadAllBytes($Path)
    # ISO-8859-1: 1 byte = 1 char, preserva ASCII para busca de substring.
    $text = [System.Text.Encoding]::GetEncoding('ISO-8859-1').GetString($bytes)
    foreach ($t in $Tokens) {
        $idx = $text.IndexOf($t, [System.StringComparison]::OrdinalIgnoreCase)
        if ($idx -ge 0) {
            Remove-Item $Path -Force
            throw "GUARDA: token proibido '$t' encontrado no binario (offset $idx). Build abortado e binario removido."
        }
    }
    Write-Host "Guarda OK: nenhuma string proibida no binario ($($Tokens -join ', '))."
}

if ([string]::IsNullOrWhiteSpace($Name)) {
    $Name = New-RandomName
}
$exeName = "$Name.exe"

Write-Host "========================================"
Write-Host "  Build limpo (nome aleatorio)"
Write-Host "  Binario: $exeName"
Write-Host "  Pasta:   $OutDir"
Write-Host "  Arch:    386 (32-bit)"
Write-Host "========================================"

# --- Ambiente de compilacao 32-bit ---
$env:GOARCH = "386"
$env:GOOS = "windows"
$env:CGO_ENABLED = "0"

if (Test-Path $OutDir) { Remove-Item -Recurse -Force $OutDir }
New-Item -ItemType Directory -Force $OutDir | Out-Null

$outPath = Join-Path $OutDir $exeName
Write-Host "Compilando (-trimpath -s -w)..."
# -trimpath: remove o prefixo de path absoluto embutido (pclntab).
& go build -trimpath -ldflags "-s -w" -o $outPath main.go
if ($LASTEXITCODE -ne 0) {
    throw "Falha na compilacao (exit $LASTEXITCODE)"
}

# --- Guarda intrinseca: valida o binario ANTES de considerar o build OK ---
Assert-CleanBinary -Path $outPath -Tokens $ForbiddenTokens

# --- Copia os configs necessarios (mesmos do build.bat) ---
$configs = @(
    "settings.json", "keybinds.json", "reactions.json", "buff_presets.json",
    "aimbot_config.json", "houses.json", "bot_config.json",
    "fishing_config.json", "skill_reactions.json"
)
foreach ($c in $configs) {
    if (Test-Path $c) { Copy-Item $c $OutDir -Force }
}

Write-Host ""
Write-Host "Build OK -> $outPath"
Write-Host "Rode como Administrador com o ArcheAge aberto."
