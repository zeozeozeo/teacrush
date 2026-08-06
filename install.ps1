$ErrorActionPreference = 'Stop'

$repo = if ($env:TEACRUSH_REPO) { $env:TEACRUSH_REPO } else { 'zeozeozeo/teacrush' }
$tag = if ($env:TEACRUSH_TAG) { $env:TEACRUSH_TAG } else { 'nightly' }
$installDir = if ($env:TEACRUSH_INSTALL_DIR) {
    $env:TEACRUSH_INSTALL_DIR
} else {
    Join-Path $env:LOCALAPPDATA 'Programs\teacrush'
}

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
switch ($architecture) {
    'X64'   { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { throw "Unsupported Windows architecture: $architecture" }
}

$asset = "teacrush-$tag-windows-$arch.zip"
$baseUrl = "https://github.com/$repo/releases/download/$tag"
$temporaryDir = Join-Path ([System.IO.Path]::GetTempPath()) ("teacrush-" + [guid]::NewGuid().ToString())
$archivePath = Join-Path $temporaryDir $asset
$checksumsPath = Join-Path $temporaryDir 'SHA256SUMS'

try {
    New-Item -ItemType Directory -Path $temporaryDir -Force | Out-Null
    Write-Host "Downloading $asset..."
    Invoke-WebRequest -Uri "$baseUrl/$asset" -OutFile $archivePath
    Invoke-WebRequest -Uri "$baseUrl/SHA256SUMS" -OutFile $checksumsPath

    $checksumLine = Get-Content $checksumsPath | Where-Object { $_ -match "\s$([regex]::Escape($asset))$" } | Select-Object -First 1
    if (-not $checksumLine) { throw "Checksum for $asset was not found" }
    $expected = ($checksumLine -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
    if ($expected -ne $actual) { throw 'Checksum verification failed' }

    Expand-Archive -LiteralPath $archivePath -DestinationPath $temporaryDir -Force
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    Copy-Item -LiteralPath (Join-Path $temporaryDir 'teacrush.exe') -Destination (Join-Path $installDir 'teacrush.exe') -Force

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathEntries = if ($userPath) { $userPath -split ';' | Where-Object { $_ } } else { @() }
    if (-not ($pathEntries | Where-Object { $_.TrimEnd('\') -ieq $installDir.TrimEnd('\') })) {
        $newPath = (($pathEntries + $installDir) -join ';')
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host "Added $installDir to the user PATH. Open a new terminal to use it."
    }

    Write-Host "Installed teacrush to $(Join-Path $installDir 'teacrush.exe')"
    Write-Host 'Run this installer again at any time to update the nightly build.'
} finally {
    if (Test-Path -LiteralPath $temporaryDir) {
        Remove-Item -LiteralPath $temporaryDir -Recurse -Force
    }
}
