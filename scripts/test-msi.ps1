param(
    [Parameter(Mandatory = $true)][string]$ArtifactDirectory,
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$WixPath,
    [Parameter(Mandatory = $true)][string]$OutputDirectory
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$filenameVersion = $Version.Substring(1)
$amd64Archive = Join-Path $ArtifactDirectory "topo_${filenameVersion}_windows_amd64.zip"
$arm64Archive = Join-Path $ArtifactDirectory "topo_${filenameVersion}_windows_arm64.zip"
$firstOutput = Join-Path $env:RUNNER_TEMP "topo-msi-first"
$armOutput = Join-Path $env:RUNNER_TEMP "topo-msi-arm"
$upgradeOutput = Join-Path $env:RUNNER_TEMP "topo-msi-upgrade"

& (Join-Path $PSScriptRoot "build-msi.ps1") `
    -Version $Version -Architecture amd64 -RawArchive $amd64Archive `
    -OutputDirectory $firstOutput -WixPath $WixPath
& (Join-Path $PSScriptRoot "build-msi.ps1") `
    -Version $Version -Architecture arm64 -RawArchive $arm64Archive `
    -OutputDirectory $armOutput -WixPath $WixPath

$firstMsi = Join-Path $firstOutput "topo_${filenameVersion}_windows_amd64.msi"
$armMsi = Join-Path $armOutput "topo_${filenameVersion}_windows_arm64.msi"
$installLog = Join-Path $env:RUNNER_TEMP "topo-msi-install.log"
$install = Start-Process msiexec.exe -Wait -PassThru -ArgumentList "/i `"$firstMsi`" /qn /norestart /l*v `"$installLog`""
if ($install.ExitCode -ne 0) {
    Get-Content -LiteralPath $installLog
    throw "MSI install failed with exit code $($install.ExitCode)"
}

$installedBinary = Join-Path $env:ProgramFiles "Nischoy\Topo\topo.exe"
if (& $installedBinary version) {
    $installedVersion = (& $installedBinary version).Trim()
} else {
    throw "Installed Topo binary did not run"
}
if ($installedVersion -ne $Version) {
    throw "Installed Topo reports $installedVersion, expected $Version"
}
$expectedDigest = (Get-Content -LiteralPath "$firstMsi.payload.sha256").Split(' ')[0]
$actualDigest = (Get-FileHash -Algorithm SHA256 -LiteralPath $installedBinary).Hash.ToLowerInvariant()
if ($actualDigest -ne $expectedDigest) {
    throw "Installed MSI payload does not match the raw release binary"
}
if (Get-Service -Name TopoAgent -ErrorAction SilentlyContinue) {
    throw "MSI must not register or start an unconfigured Topo Agent service"
}
$machinePath = [Environment]::GetEnvironmentVariable("Path", "Machine").Split(';') | ForEach-Object { $_.TrimEnd('\') }
if ($machinePath -notcontains (Split-Path -Parent $installedBinary).TrimEnd('\')) {
    throw "MSI did not register the Topo installation directory in machine PATH"
}

$operatorState = Join-Path $env:ProgramData "Nischoy\Topo\operator-owned"
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $operatorState) | Out-Null
Set-Content -LiteralPath $operatorState -Value "preserve" -Encoding ascii

& (Join-Path $PSScriptRoot "build-msi.ps1") `
    -Version v0.0.1-ci -Architecture amd64 -RawArchive $amd64Archive `
    -OutputDirectory $upgradeOutput -WixPath $WixPath
$upgradeMsi = Join-Path $upgradeOutput "topo_0.0.1-ci_windows_amd64.msi"
$upgradeLog = Join-Path $env:RUNNER_TEMP "topo-msi-upgrade.log"
$upgrade = Start-Process msiexec.exe -Wait -PassThru -ArgumentList "/i `"$upgradeMsi`" /qn /norestart /l*v `"$upgradeLog`""
if ($upgrade.ExitCode -ne 0) {
    Get-Content -LiteralPath $upgradeLog
    throw "MSI upgrade failed with exit code $($upgrade.ExitCode)"
}
$registrations = Get-ItemProperty "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*" |
    Where-Object { $_.DisplayName -eq "Nischoy Topo" }
if (@($registrations).Count -ne 1 -or $registrations.DisplayVersion -ne "0.0.1.0") {
    throw "MSI major upgrade did not replace the prior product registration"
}

$uninstallLog = Join-Path $env:RUNNER_TEMP "topo-msi-uninstall.log"
$uninstall = Start-Process msiexec.exe -Wait -PassThru -ArgumentList "/x `"$upgradeMsi`" /qn /norestart /l*v `"$uninstallLog`""
if ($uninstall.ExitCode -ne 0) {
    Get-Content -LiteralPath $uninstallLog
    throw "MSI uninstall failed with exit code $($uninstall.ExitCode)"
}
if (Test-Path -LiteralPath $installedBinary) {
    throw "MSI uninstall left the Topo binary behind"
}
if (-not (Test-Path -LiteralPath $operatorState)) {
    throw "MSI uninstall removed operator-owned state"
}
$machinePath = [Environment]::GetEnvironmentVariable("Path", "Machine").Split(';') | ForEach-Object { $_.TrimEnd('\') }
if ($machinePath -contains (Split-Path -Parent $installedBinary).TrimEnd('\')) {
    throw "MSI uninstall left the Topo PATH entry behind"
}

New-Item -ItemType Directory -Path $OutputDirectory | Out-Null
Copy-Item -LiteralPath $firstMsi, "$firstMsi.payload.sha256", $armMsi, "$armMsi.payload.sha256" -Destination $OutputDirectory
