# Installs the hibira CLI on Windows from the GitHub releases of hibira-io/cli.
#
#   irm https://raw.githubusercontent.com/hibira-io/cli/main/install.ps1 | iex
#
# Environment:
#   HIBIRA_VERSION      version to install, such as v0.1.0 (default: latest)
#   HIBIRA_INSTALL_DIR  where to put hibira.exe (default: %LOCALAPPDATA%\Programs\hibira)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Install-Hibira {
    $repo = 'hibira-io/cli'

    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "Unsupported CPU $($env:PROCESSOR_ARCHITECTURE). Build from source with: go install github.com/$repo/cmd/hibira@latest" }
    }

    $version = $env:HIBIRA_VERSION
    if (-not $version) {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ 'User-Agent' = 'hibira-install' }
        $version = $release.tag_name
    }
    if (-not $version.StartsWith('v')) { $version = "v$version" }

    $archive = "hibira_$($version.TrimStart('v'))_windows_$arch.zip"
    $base = "https://github.com/$repo/releases/download/$version"
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("hibira-" + [Guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null

    try {
        Write-Host "Downloading hibira $version for windows/$arch..."
        Invoke-WebRequest -Uri "$base/$archive" -OutFile (Join-Path $tmp $archive) -UseBasicParsing
        Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

        $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { ($_ -split '\s+')[1] -eq $archive } | Select-Object -First 1
        if (-not $line) { throw "$archive is not listed in checksums.txt" }
        $want = ($line -split '\s+')[0].ToLower()
        $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $archive)).Hash.ToLower()
        if ($want -ne $got) { throw "Checksum mismatch for $archive (expected $want, got $got)" }

        Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath $tmp -Force

        $dir = $env:HIBIRA_INSTALL_DIR
        if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA 'Programs\hibira' }
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
        Copy-Item (Join-Path $tmp 'hibira.exe') (Join-Path $dir 'hibira.exe') -Force

        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not (($userPath -split ';') -contains $dir)) {
            [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $dir).TrimStart(';'), 'User')
            $env:Path = "$env:Path;$dir"
            Write-Host "Added $dir to your PATH (open a new terminal to pick it up)."
        }

        Write-Host "Installed $(& (Join-Path $dir 'hibira.exe') version) to $dir\hibira.exe"
        Write-Host ''
        Write-Host 'Next: hibira config set --app-id <id> --key <key>   (then run: hibira)'
    }
    finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}

Install-Hibira
