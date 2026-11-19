# Explicitly authorized real mail only. This script never loads or edits .env.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$UniversityRecipient,
    [switch]$SendLiveEmail
)
$ErrorActionPreference = 'Stop'
if (-not $SendLiveEmail) { throw 'Real email requires the explicit -SendLiveEmail switch and an authorized recipient.' }
if (-not $env:SMTP_HOST -or -not $env:SMTP_FROM) { throw 'Set SMTP_HOST and SMTP_FROM in this process first.' }
if ($env:SMTP_TLS_MODE -notin @('starttls', 'implicit')) { throw 'Set SMTP_TLS_MODE to starttls or implicit.' }
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../../..')).Path
$runId = [Guid]::NewGuid().ToString('N')
$network = "tutti-smtp-probe-$runId"
$postgres = "tutti-smtp-probe-db-$runId"
$createdNetwork = $false
$createdPostgres = $false
$dsn = 'postgres://postgres:tutti_test@probe-postgres:5432/tutti_smtp_probe?sslmode=disable'
function Invoke-Docker {
    & docker @args
    if ($LASTEXITCODE -ne 0) { throw "Docker command failed (exit $LASTEXITCODE)." }
}
Push-Location $repo
try {
    Invoke-Docker build --target test -t tutti-backend-tests ./backend
    Invoke-Docker network create $network | Out-Null
    $createdNetwork = $true
    Invoke-Docker run -d --name $postgres --network $network --network-alias probe-postgres `
        --tmpfs '/var/lib/postgresql/data:rw,size=256m' `
        -e POSTGRES_PASSWORD=tutti_test -e POSTGRES_DB=tutti_smtp_probe postgres:17 | Out-Null
    $createdPostgres = $true
    $ready = $false
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        & docker exec $postgres pg_isready -h 127.0.0.1 -U postgres -d tutti_smtp_probe *> $null
        if ($LASTEXITCODE -eq 0) { $ready = $true; break }
        Start-Sleep -Milliseconds 250
    }
    if (-not $ready) { throw 'Disposable probe PostgreSQL did not start.' }
    Invoke-Docker run --rm --network $network tutti-backend-tests migrate -path migrations -database $dsn up
    $smtpArgs = @()
    foreach ($key in @('SMTP_HOST','SMTP_PORT','SMTP_FROM','SMTP_USERNAME','SMTP_PASSWORD','SMTP_TLS_MODE','SMTP_CONNECT_TIMEOUT','SMTP_SEND_TIMEOUT')) {
        if ([Environment]::GetEnvironmentVariable($key)) { $smtpArgs += @('-e', $key) }
    }
    foreach ($key in @('SMTP_PASSWORD_FILE','SMTP_CA_FILE')) {
        $path = [Environment]::GetEnvironmentVariable($key)
        if ($path) {
            $resolved = (Resolve-Path -LiteralPath $path).Path
            $target = "/run/$key"
            $smtpArgs += @('--mount', "type=bind,source=$resolved,target=$target,readonly", '-e', "$key=$target")
        }
    }
    Invoke-Docker run --rm --network $network @smtpArgs `
        -e "TUTTI_TEST_DATABASE_URL=$dsn" -e "TUTTI_LIVE_SMTP_RECIPIENT=$UniversityRecipient" `
        -e TUTTI_LIVE_SMTP_AUTHORIZE=send-one-university-test `
        tutti-backend-tests go test -count=1 -v ./tests/integration -run '^TestUniversitySMTP$'
}
finally {
    if ($createdPostgres) { & docker rm -f -v $postgres | Out-Null }
    if ($createdNetwork) { & docker network rm $network | Out-Null }
    Pop-Location
}
