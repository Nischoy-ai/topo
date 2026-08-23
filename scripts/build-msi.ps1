param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][ValidateSet("amd64", "arm64")][string]$Architecture,
    [Parameter(Mandatory = $true)][string]$RawArchive,
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [Parameter(Mandatory = $true)][string]$WixPath
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

if ($Version -notmatch '^v(?<major>[0-9]+)\.(?<minor>[0-9]+)\.(?<patch>[0-9]+)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$') {
    throw "Version must be a semantic tag such as v1.2.3"
}
if (Test-Path -LiteralPath $OutputDirectory) {
    throw "Output directory already exists: $OutputDirectory"
}
if (-not (Test-Path -LiteralPath $RawArchive -PathType Leaf)) {
    throw "Raw Windows archive does not exist: $RawArchive"
}
if (-not (Test-Path -LiteralPath $WixPath -PathType Leaf)) {
    throw "Pinned WiX executable does not exist: $WixPath"
}

function New-DeterministicGuid([string]$Value) {
    $namespace = [Guid]::Parse("cd5f19c6-924f-5f8b-9de4-c1b5e37346fe")
    $namespaceBytes = $namespace.ToByteArray()
    [Array]::Reverse($namespaceBytes, 0, 4)
    [Array]::Reverse($namespaceBytes, 4, 2)
    [Array]::Reverse($namespaceBytes, 6, 2)
    $valueBytes = [Text.Encoding]::UTF8.GetBytes($Value)
    $input = [byte[]]::new($namespaceBytes.Length + $valueBytes.Length)
    [Array]::Copy($namespaceBytes, 0, $input, 0, $namespaceBytes.Length)
    [Array]::Copy($valueBytes, 0, $input, $namespaceBytes.Length, $valueBytes.Length)
    $digest = [Security.Cryptography.SHA1]::HashData($input)
    $guidBytes = [byte[]]::new(16)
    [Array]::Copy($digest, $guidBytes, 16)
    $guidBytes[6] = ($guidBytes[6] -band 0x0f) -bor 0x50
    $guidBytes[8] = ($guidBytes[8] -band 0x3f) -bor 0x80
    [Array]::Reverse($guidBytes, 0, 4)
    [Array]::Reverse($guidBytes, 4, 2)
    [Array]::Reverse($guidBytes, 6, 2)
    return [Guid]::new($guidBytes).ToString("B").ToUpperInvariant()
}

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$work = Join-Path ([IO.Path]::GetTempPath()) ("topo-msi-" + [Guid]::NewGuid().ToString("N"))
try {
    New-Item -ItemType Directory -Path $work | Out-Null
    $expanded = Join-Path $work "raw"
    Expand-Archive -LiteralPath $RawArchive -DestinationPath $expanded
    $binary = @(Get-ChildItem -LiteralPath $expanded -Filter topo.exe -File -Recurse)
    if ($binary.Count -ne 1) {
        throw "Raw archive must contain exactly one topo.exe, found $($binary.Count)"
    }

    New-Item -ItemType Directory -Path $OutputDirectory | Out-Null
    $filenameVersion = $Version.Substring(1)
    $output = Join-Path $OutputDirectory "topo_${filenameVersion}_windows_${Architecture}.msi"
    $wixArchitecture = if ($Architecture -eq "amd64") { "x64" } else { "arm64" }
    $upgradeCode = if ($Architecture -eq "amd64") {
        "{6EEC7209-21AA-515A-922D-F51DC01932C1}"
    } else {
        "{08A76E0C-E9E7-56F8-962E-6DC4F2CC88BD}"
    }
    $packageVersion = "$($Matches.major).$($Matches.minor).$($Matches.patch).0"

    & $WixPath build (Join-Path $repositoryRoot "packaging/windows/topo.wxs") `
        -arch $wixArchitecture `
        -d "ProductCode=$(New-DeterministicGuid "product/$Version/$Architecture")" `
        -d "UpgradeCode=$upgradeCode" `
        -d "ComponentCode=$(New-DeterministicGuid "binary/$Architecture")" `
        -d "LicenseComponentCode=$(New-DeterministicGuid "license/$Architecture")" `
        -d "ReadmeComponentCode=$(New-DeterministicGuid "readme/$Architecture")" `
        -d "PackageVersion=$packageVersion" `
        -d "BinaryPath=$($binary.FullName)" `
        -d "LicensePath=$(Join-Path $repositoryRoot 'LICENSE')" `
        -d "ReadmePath=$(Join-Path $repositoryRoot 'README.md')" `
        -out $output
    if ($LASTEXITCODE -ne 0) {
        throw "WiX failed with exit code $LASTEXITCODE"
    }

    $payloadDigest = (Get-FileHash -Algorithm SHA256 -LiteralPath $binary.FullName).Hash.ToLowerInvariant()
    Set-Content -LiteralPath "$output.payload.sha256" -Value "$payloadDigest  topo.exe" -Encoding ascii -NoNewline
} catch {
    if (Test-Path -LiteralPath $OutputDirectory) {
        Remove-Item -LiteralPath $OutputDirectory -Recurse -Force
    }
    throw
} finally {
    if (Test-Path -LiteralPath $work) {
        Remove-Item -LiteralPath $work -Recurse -Force
    }
}
