param(
    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'

if ($env:TEACRUSH_UNINSTALL -eq '1') {
    $Uninstall = $true
}

$repo = if ($env:TEACRUSH_REPO) { $env:TEACRUSH_REPO } else { 'zeozeozeo/teacrush' }
$tag = if ($env:TEACRUSH_TAG) { $env:TEACRUSH_TAG } else { 'nightly' }
$installDir = $env:TEACRUSH_INSTALL_DIR
if ([string]::IsNullOrWhiteSpace($installDir)) {
    $localAppData = [Environment]::GetEnvironmentVariable('LOCALAPPDATA')
    if ([string]::IsNullOrWhiteSpace($localAppData)) {
        $localAppData = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
    }
    if ([string]::IsNullOrWhiteSpace($localAppData)) {
        throw 'Could not determine the Windows local application data directory'
    }
    $installDir = Join-Path $localAppData 'Programs\teacrush'
}

if ($Uninstall) {
    $binaryPath = Join-Path $installDir 'teacrush.exe'
    if (Test-Path -LiteralPath $binaryPath) {
        Remove-Item -LiteralPath $binaryPath -Force
        Write-Host "Removed $binaryPath"
    } else {
        Write-Host "teacrush is not installed at $binaryPath"
    }

    if (Test-Path -LiteralPath $installDir -PathType Container) {
        # This fails harmlessly when another file is present.
        Remove-Item -LiteralPath $installDir -Force -ErrorAction SilentlyContinue
    }

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $normalizedInstallDir = $installDir.TrimEnd('\')
    $remainingPathEntries = @()
    if (-not [string]::IsNullOrWhiteSpace($userPath)) {
        foreach ($pathEntry in ($userPath -split ';')) {
            if (-not [string]::IsNullOrWhiteSpace($pathEntry) -and -not [string]::Equals($pathEntry.Trim().TrimEnd('\'), $normalizedInstallDir, [StringComparison]::OrdinalIgnoreCase)) {
                $remainingPathEntries += $pathEntry
            }
        }
    }
    [Environment]::SetEnvironmentVariable('Path', ($remainingPathEntries -join ';'), 'User')
    Write-Host "Removed $installDir from the user PATH. Open a new terminal to apply the change."
    return
}

$architecture = $env:PROCESSOR_ARCHITEW6432
if ([string]::IsNullOrWhiteSpace($architecture)) {
    $architecture = $env:PROCESSOR_ARCHITECTURE
}
if ([string]::IsNullOrWhiteSpace($architecture)) {
    throw 'Could not determine the Windows processor architecture'
}
switch ($architecture.ToUpperInvariant()) {
    'AMD64' { $arch = 'amd64' }
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

    $checksumLine = Get-Content -LiteralPath $checksumsPath | Where-Object { $_ -match "\s$([regex]::Escape($asset))$" } | Select-Object -First 1
    if ([string]::IsNullOrWhiteSpace([string]$checksumLine)) { throw "Checksum for $asset was not found" }
    $expected = [string](($checksumLine -split '\s+')[0])
    $actual = [string](Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash
    if (-not [string]::Equals($expected, $actual, [StringComparison]::OrdinalIgnoreCase)) { throw 'Checksum verification failed' }

    Expand-Archive -LiteralPath $archivePath -DestinationPath $temporaryDir -Force
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    Copy-Item -LiteralPath (Join-Path $temporaryDir 'teacrush.exe') -Destination (Join-Path $installDir 'teacrush.exe') -Force

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathEntries = if ($userPath) { $userPath -split ';' | Where-Object { $_ } } else { @() }
    $normalizedInstallDir = $installDir.TrimEnd('\')
    $hasInstallPath = $false
    foreach ($pathEntry in @($pathEntries)) {
        if (-not [string]::IsNullOrWhiteSpace($pathEntry) -and [string]::Equals($pathEntry.TrimEnd('\'), $normalizedInstallDir, [StringComparison]::OrdinalIgnoreCase)) {
            $hasInstallPath = $true
            break
        }
    }
    if (-not $hasInstallPath) {
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
