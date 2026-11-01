# Registers the companion as a Chrome native messaging host for the current
# user on Windows. Run it after building companion\bin\geomind-companion.exe.
[CmdletBinding()]
param(
  [string]$Binary,
  [switch]$DryRun
)

$ErrorActionPreference = "Stop"
$HostName = "com.geomind.companion"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if (-not $Binary) {
  $Binary = Join-Path $Root "companion\bin\geomind-companion.exe"
}
$ManifestPath = Join-Path $Root "extension\manifest.json"

if (-not (Test-Path -LiteralPath $ManifestPath)) {
  throw "extension manifest not found: $ManifestPath"
}
if (-not (Test-Path -LiteralPath $Binary)) {
  throw "companion binary not found: $Binary. Build it with: cd companion; go build -o bin/geomind-companion.exe ./cmd/geomind-companion"
}
$Binary = (Resolve-Path -LiteralPath $Binary).Path

$Manifest = Get-Content -LiteralPath $ManifestPath -Raw | ConvertFrom-Json
if (-not $Manifest.key) {
  throw "manifest key is missing"
}

$Der = [Convert]::FromBase64String($Manifest.key)
$Sha256 = [System.Security.Cryptography.SHA256]::Create()
try {
  $Hash = $Sha256.ComputeHash($Der)
} finally {
  $Sha256.Dispose()
}
$Alphabet = "abcdefghijklmnop".ToCharArray()
$IdBuilder = New-Object System.Text.StringBuilder
for ($i = 0; $i -lt 16; $i++) {
  [void]$IdBuilder.Append($Alphabet[$Hash[$i] -shr 4])
  [void]$IdBuilder.Append($Alphabet[$Hash[$i] -band 15])
}
$ExtensionId = $IdBuilder.ToString()

$HostManifest = [ordered]@{
  name            = $HostName
  description     = "GeoMind companion"
  path            = $Binary
  type            = "stdio"
  allowed_origins = @("chrome-extension://$ExtensionId/")
}
$Target = Join-Path $env:LOCALAPPDATA "GeoMind\$HostName.json"
$RegistryPath = "Software\Google\Chrome\NativeMessagingHosts\$HostName"

if ($DryRun) {
  $HostManifest | ConvertTo-Json -Depth 4
  Write-Host "Registry: HKCU:\$RegistryPath -> $Target"
  Write-Host "Extension id: $ExtensionId"
  return
}

New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Target) | Out-Null
$Utf8 = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($Target, ($HostManifest | ConvertTo-Json -Depth 4), $Utf8)

$Key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($RegistryPath)
if ($null -eq $Key) {
  throw "could not create registry key HKCU:\$RegistryPath"
}
try {
  $Key.SetValue("", $Target)
} finally {
  $Key.Close()
}

Write-Host "Installed $HostName"
Write-Host "  manifest: $Target"
Write-Host "  binary:   $Binary"
Write-Host "  extension: $ExtensionId"
Write-Host "Reload the extension in chrome://extensions and restart Chrome."
