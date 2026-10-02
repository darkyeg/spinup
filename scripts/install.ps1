# Installs the latest spinup release into ~\.local\bin, checksum-verified.
#   irm https://raw.githubusercontent.com/darkyeg/spinup/main/scripts/install.ps1 | iex
$ErrorActionPreference = 'Stop'

$repo = if ($env:SPINUP_RELEASES) { $env:SPINUP_RELEASES } else { 'darkyeg/spinup' }
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "spinup: unsupported CPU $env:PROCESSOR_ARCHITECTURE" }
}
$asset = "spinup_windows_$arch.exe"
$base = "https://github.com/$repo/releases/latest/download"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("spinup-" + [guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null
try {
    Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile "$tmp\$asset"
    Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile "$tmp\checksums.txt"
    $want = (Get-Content "$tmp\checksums.txt" | Where-Object { ($_ -split '\s+')[1] -eq $asset } | ForEach-Object { ($_ -split '\s+')[0] })
    $got = (Get-FileHash -Algorithm SHA256 "$tmp\$asset").Hash.ToLower()
    if (-not $want -or $want -ne $got) { throw "spinup: checksum mismatch for $asset; nothing installed" }

    $dest = Join-Path $HOME '.local\bin'
    New-Item -ItemType Directory -Force $dest | Out-Null
    Copy-Item "$tmp\$asset" "$dest\spinup.exe" -Force
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $dest) {
        [Environment]::SetEnvironmentVariable('Path', (($userPath, $dest) -join ';').Trim(';'), 'User')
        $env:Path = "$env:Path;$dest"
        Write-Host "Added $dest to your PATH (new terminals pick it up)."
    }
    Write-Host "Installed $(& "$dest\spinup.exe" --version) to $dest\spinup.exe"
    Write-Host 'Next: spinup setup <name-for-this-machine>'
} finally {
    Remove-Item -Recurse -Force $tmp
}
