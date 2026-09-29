# Instala la última versión de bflow en Windows, sin Go:
#
#   irm https://raw.githubusercontent.com/innobytestech/bflow/main/install.ps1 | iex
#
# BFLOW_INSTALL_DIR cambia la carpeta (por defecto %LOCALAPPDATA%\Programs\bflow)
# y se agrega al PATH del usuario. Verifica el SHA-256 contra checksums.txt
# antes de instalar. Después: bflow update.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$api = if ($env:BFLOW_RELEASES_URL) { $env:BFLOW_RELEASES_URL } else { 'https://api.github.com/repos/innobytestech/bflow' }
$dir = if ($env:BFLOW_INSTALL_DIR) { $env:BFLOW_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\bflow' }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }

$rel = Invoke-RestMethod "$api/releases/latest"
$tag = $rel.tag_name
$asset = "bflow_$($tag.TrimStart('v'))_windows_$arch.zip"
$assetUrl = ($rel.assets | Where-Object name -eq $asset).browser_download_url
$sumsUrl = ($rel.assets | Where-Object name -eq 'checksums.txt').browser_download_url
if (-not $assetUrl -or -not $sumsUrl) { throw "bflow: $tag no publica $asset" }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("bflow-" + [guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null
try {
    $zip = Join-Path $tmp $asset
    Invoke-WebRequest $assetUrl -OutFile $zip -UseBasicParsing
    $sums = (Invoke-WebRequest $sumsUrl -UseBasicParsing).Content
    if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }
    $want = ($sums -split "`n" | Where-Object { $_ -match "\s$([regex]::Escape($asset))\s*$" } | ForEach-Object { ($_ -split '\s+')[0] }) | Select-Object -First 1
    $got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
    if (-not $want -or $got -ne $want.ToLower()) { throw "bflow: $asset no coincide con checksums.txt; no se instala" }

    Expand-Archive $zip -DestinationPath (Join-Path $tmp 'x') -Force
    New-Item -ItemType Directory $dir -Force | Out-Null
    $exe = Join-Path $dir 'bflow.exe'
    if (Test-Path $exe) { Move-Item $exe "$exe.old" -Force }  # si está en uso
    Copy-Item (Join-Path $tmp 'x\bflow.exe') $exe -Force
    Remove-Item "$exe.old" -Force -ErrorAction SilentlyContinue
    Write-Host "bflow $tag instalado en $exe"

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $dir) {
        [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
        Write-Host "PATH actualizado con $dir; abre una terminal nueva para usar bflow."
    }
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
