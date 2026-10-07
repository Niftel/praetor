$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

docker compose version | Out-Null

function New-Hex([int]$Bytes) {
  $buffer = New-Object byte[] $Bytes
  [Security.Cryptography.RandomNumberGenerator]::Fill($buffer)
  return [Convert]::ToHexString($buffer).ToLowerInvariant()
}

if (-not (Test-Path .env)) {
  $content = Get-Content .env.template -Raw
  $content = $content.Replace("POSTGRES_PASSWORD=GENERATE_ME", "POSTGRES_PASSWORD=$(New-Hex 16)")
  $content = $content.Replace("JWT_SECRET=GENERATE_ME", "JWT_SECRET=$(New-Hex 32)")
  $content = $content.Replace("PRAETOR_SECRET_KEY=GENERATE_ME", "PRAETOR_SECRET_KEY=$(New-Hex 16)")
  $content = $content.Replace("PRAETOR_INTERNAL_TOKEN=GENERATE_ME", "PRAETOR_INTERNAL_TOKEN=$(New-Hex 32)")
  $content = $content.Replace("PRAETOR_ADMIN_PASSWORD=GENERATE_ME", "PRAETOR_ADMIN_PASSWORD=$(New-Hex 12)")
  Set-Content .env $content -NoNewline
}
if ((Get-Content .env -Raw).Contains("GENERATE_ME")) { throw ".env contains an ungenerated secret; remove it and run start.ps1 again." }
$values = @{}
Get-Content .env | Where-Object { $_ -match '^[^#=]+=' } | ForEach-Object { $key, $value = $_ -split '=', 2; $values[$key] = $value }

if (Test-Path images.tar) {
  Write-Host "Loading the bundled Praetor images..."
  docker load -i images.tar
} else {
  Write-Host "Pulling the pinned Praetor component set..."
  docker compose pull
}
docker compose up -d --remove-orphans

Write-Host "Waiting for the UI..."
$ready = $false
for ($i = 0; $i -lt 90; $i++) {
  try { Invoke-WebRequest "http://127.0.0.1:$($values.PRAETOR_UI_PORT)/api/v1/ping" -UseBasicParsing | Out-Null; $ready = $true; break } catch { Start-Sleep 2 }
}
if (-not $ready) { docker compose ps; throw "Praetor did not become ready. Run docker compose logs." }

Write-Host ""
Write-Host "Praetor is ready: http://localhost:$($values.PRAETOR_UI_PORT)"
Write-Host "Username: $($values.PRAETOR_ADMIN_USERNAME)"
Write-Host "Password: $($values.PRAETOR_ADMIN_PASSWORD)"
Write-Host "Credentials are stored locally in $PSScriptRoot\.env"
