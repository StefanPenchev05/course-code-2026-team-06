# Run from any directory with Docker Desktop using Linux containers.
# All databases are disposable; the developer's .env and Compose services are not used.
[CmdletBinding()]
param([switch]$NotificationsOnly, [switch]$IssuesOnly, [switch]$MonitoringOnly, [switch]$PolicyCancellationOnly, [switch]$SuspensionOnly, [switch]$AuditOnly)
$ErrorActionPreference = 'Stop'
if (@($NotificationsOnly, $IssuesOnly, $MonitoringOnly, $PolicyCancellationOnly, $SuspensionOnly, $AuditOnly).Where({ $_ }).Count -gt 1) { throw 'Choose only one targeted test mode.' }
$repo = (Resolve-Path (Join-Path $PSScriptRoot '../../..')).Path
$runId = [Guid]::NewGuid().ToString('N')
$network = "tutti-test-$runId"
$runner = "tutti-tests-$runId"
$networkCreated = $false
function Invoke-Docker {
    & docker @args
    if ($LASTEXITCODE -ne 0) { throw "Docker command failed (exit $LASTEXITCODE)." }
}
Push-Location $repo
try {
    Invoke-Docker build --target runtime -t tutti-worker-check ./backend
    Invoke-Docker build --target test -t tutti-backend-tests ./backend
    Invoke-Docker network create $network | Out-Null
    $networkCreated = $true
    $testCommand = @()
    if ($NotificationsOnly) { $testCommand = @('bash', 'tests/integration/test_identity.sh', '-race', '-run', '^TestNotificationDelivery$') }
    if ($IssuesOnly) { $testCommand = @('bash', 'tests/integration/test_identity.sh', '-race', '-run', '^TestIssueReporting$') }
    if ($MonitoringOnly) { $testCommand = @('bash', 'tests/integration/test_identity.sh', '-race', '-run', '^TestCoordinatorMonitoring$') }
    if ($PolicyCancellationOnly) { $testCommand = @('bash', 'tests/integration/test_identity.sh', '-race', '-run', '^TestPolicyCancellation$') }
    if ($SuspensionOnly) { $testCommand = @('bash', 'tests/integration/test_identity.sh', '-race', '-run', '^TestSuspensionAdministration$') }
    if ($AuditOnly) { $testCommand = @('bash', 'tests/integration/test_identity.sh', '-race', '-run', 'TestSuspensionAdministration|TestTutoringAPI|TestBookingLifecycle|TestIssueReporting|TestPolicyCancellation') }
    Invoke-Docker run --rm --name $runner --network $network `
        --mount 'type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock' `
        -e "TUTTI_TEST_DOCKER_NETWORK=$network" -e "TUTTI_TEST_CONTAINER=$runner" `
        -e TUTTI_TEST_WORKER_IMAGE=tutti-worker-check tutti-backend-tests @testCommand
}
finally {
    # IDs come only from this run's unique network; never touch Compose volumes.
    if ($networkCreated) {
        $containers = @(& docker network inspect --format '{{range $id, $c := .Containers}}{{$id}} {{end}}' $network)
        foreach ($id in (($containers -join ' ') -split '\s+')) {
            if ($id -match '^[0-9a-f]{64}$') { & docker rm -f -v $id | Out-Null }
        }
        & docker network rm $network | Out-Null
    }
    Pop-Location
}
