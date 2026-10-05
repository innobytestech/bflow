# Instala la última versión de bflow en Windows, sin Go:
#
#   irm https://raw.githubusercontent.com/innobytestech/bflow/main/install.ps1 | iex
#
# BFLOW_INSTALL_DIR cambia la carpeta (por defecto %LOCALAPPDATA%\Programs\bflow)
# y se agrega al PATH del usuario. Verifica el SHA-256 contra checksums.txt
# antes de instalar. Después: bflow update.
#
# BFLOW_VERSION=v0.1.0-rc.9 fija la versión (con o sin la v). Sin ella se
# instala la última; si solo hay prereleases publicadas, la más reciente.
# BFLOW_INSTALL_NO_PATH=1 no toca el PATH del usuario.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$api = if ($env:BFLOW_RELEASES_URL) { $env:BFLOW_RELEASES_URL } else { 'https://api.github.com/repos/innobytestech/bflow' }
$dir = if ($env:BFLOW_INSTALL_DIR) { $env:BFLOW_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\bflow' }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }

function Get-StatusCode($err) {
    try { return [int]$err.Exception.Response.StatusCode } catch { return 0 }
}

if ($env:BFLOW_VERSION) {
    $want = "v$($env:BFLOW_VERSION.TrimStart('v'))"
    if ($want -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$') { throw "bflow: BFLOW_VERSION no es una versión válida: $($env:BFLOW_VERSION)" }
    try { $rel = Invoke-RestMethod "$api/releases/tags/$want" }
    catch {
        if ((Get-StatusCode $_) -eq 404) { throw "bflow: no existe la versión $want" }
        throw
    }
} else {
    try { $rel = Invoke-RestMethod "$api/releases/latest" }
    catch {
        if ((Get-StatusCode $_) -ne 404) { throw }
        # GitHub no cuenta las prereleases como "latest": con solo prereleases
        # publicadas se toma la más reciente que no sea borrador.
        $list = Invoke-RestMethod "$api/releases?per_page=10"
        $rel = foreach ($r in $list) { if (-not $r.draft) { $r; break } }
        if (-not $rel) { throw 'bflow: no se encontró una versión publicada' }
    }
}
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
    $sha = [System.Security.Cryptography.SHA256]::Create()
    $fs = [System.IO.File]::OpenRead($zip)
    try { $got = ([BitConverter]::ToString($sha.ComputeHash($fs)) -replace '-', '').ToLower() } finally { $fs.Dispose(); $sha.Dispose() }
    if (-not $want -or $got -ne $want.ToLower()) { throw "bflow: $asset no coincide con checksums.txt; no se instala" }

    Expand-Archive $zip -DestinationPath (Join-Path $tmp 'x') -Force
    New-Item -ItemType Directory $dir -Force | Out-Null
    $exe = Join-Path $dir 'bflow.exe'
    if (Test-Path $exe) { Move-Item $exe "$exe.old" -Force }  # si está en uso
    Copy-Item (Join-Path $tmp 'x\bflow.exe') $exe -Force
    Remove-Item "$exe.old" -Force -ErrorAction SilentlyContinue
    Write-Host "bflow $tag instalado en $exe"

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($env:BFLOW_INSTALL_NO_PATH -ne '1' -and ($userPath -split ';') -notcontains $dir) {
        [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
        Write-Host "PATH actualizado con $dir; abre una terminal nueva para usar bflow."
    }
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
