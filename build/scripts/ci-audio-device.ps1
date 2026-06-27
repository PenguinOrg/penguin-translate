#Requires -Version 5.1
[CmdletBinding()]
param([string]$Version = '3.6')

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Warn($m) { Write-Warning "ci-audio-device: $m" }

function Invoke-Bounded([string]$exe, [string[]]$cmdArgs, [int]$timeoutSec) {
    $p = Start-Process -FilePath $exe -ArgumentList $cmdArgs -PassThru -NoNewWindow
    if (-not $p.WaitForExit($timeoutSec * 1000)) {
        try { $p.Kill() } catch {}
        Warn "$exe timed out after ${timeoutSec}s — killed"
        return $false
    }
    return ($p.ExitCode -eq 0)
}

$ProgressPreference = 'SilentlyContinue'
$work = Join-Path ([System.IO.Path]::GetTempPath()) 'scream-install'
if ($env:RUNNER_TEMP) { $work = Join-Path $env:RUNNER_TEMP 'scream-install' }

$installed = $false

try {
    New-Item -ItemType Directory -Force -Path $work | Out-Null
    $zip = Join-Path $work "Scream$Version.zip"
    $url = "https://github.com/duncanthrax/scream/releases/download/$Version/Scream$Version.zip"

    Write-Host "Downloading Scream $Version from $url"
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing
    Expand-Archive -Path $zip -DestinationPath $work -Force

    $inf = Get-ChildItem -Path $work -Recurse -Filter 'Scream.inf' |
        Where-Object { $_.FullName -match '\\(x64|amd64)\\' } | Select-Object -First 1
    if (-not $inf) { $inf = Get-ChildItem -Path $work -Recurse -Filter 'Scream.inf' | Select-Object -First 1 }
    if (-not $inf) { throw "Scream.inf not found under $work" }
    $cat = Get-ChildItem -Path (Split-Path $inf.FullName) -Filter 'Scream.cat' | Select-Object -First 1
    if (-not $cat) { $cat = Get-ChildItem -Path $work -Recurse -Filter 'Scream.cat' | Select-Object -First 1 }
    if (-not $cat) { throw "Scream.cat not found under $work" }
    Write-Host "Using driver: $($inf.FullName)"

    $sig = Get-AuthenticodeSignature $cat.FullName
    if ($sig -and $sig.SignerCertificate) {
        foreach ($storeName in 'Root', 'TrustedPublisher') {
            $store = [System.Security.Cryptography.X509Certificates.X509Store]::new($storeName, 'LocalMachine')
            $store.Open('ReadWrite'); $store.Add($sig.SignerCertificate); $store.Close()
        }
        Write-Host "Imported Scream signing cert into LocalMachine\{Root,TrustedPublisher}"
    } else {
        Warn "could not read signer certificate from $($cat.FullName)"
    }

    $devcon = Get-ChildItem -Path $work -Recurse -Filter 'devcon*.exe' |
        Where-Object { $_.Name -match 'x64|amd64' } | Select-Object -First 1
    if (-not $devcon) { $devcon = Get-ChildItem -Path $work -Recurse -Filter 'devcon*.exe' | Select-Object -First 1 }

    $installed = $false
    if ($devcon) {
        Write-Host "Installing via $($devcon.Name) ..."
        $installed = Invoke-Bounded $devcon.FullName @('install', $inf.FullName, '*Scream') 60
        if (-not $installed) { Warn "devcon install did not succeed; trying pnputil" }
    }
    if (-not $installed) {
        Write-Host "Installing via pnputil ..."
        Invoke-Bounded 'pnputil.exe' @('/add-driver', $inf.FullName, '/install') 60 | Out-Null
    }

    foreach ($svc in 'AudioEndpointBuilder', 'Audiosrv') {
        try { Start-Service $svc -ErrorAction Stop; Write-Host "started $svc" }
        catch { Warn "could not start ${svc}: $($_.Exception.Message)" }
    }

    Start-Sleep -Seconds 3
    Write-Host "--- Render endpoints (Get-CimInstance Win32_SoundDevice) ---"
    Get-CimInstance Win32_SoundDevice | Select-Object Name, Status, StatusInfo | Format-Table -AutoSize | Out-String | Write-Host

    $scream = Get-CimInstance Win32_SoundDevice | Where-Object { $_.Name -match 'Scream' -and $_.Status -eq 'OK' }
    $installed = [bool]$scream
    if ($installed) { Write-Host "Scream render endpoint present and OK" }
    else { Warn 'no Scream render endpoint detected after install' }
}
catch {
    Warn $_.Exception.Message
}
finally {
    if ($env:GITHUB_OUTPUT) {
        "installed=$($installed.ToString().ToLower())" | Out-File -FilePath $env:GITHUB_OUTPUT -Append -Encoding utf8
    }
    Write-Host "ci-audio-device: installed=$installed"
}

exit 0
