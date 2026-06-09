#Requires -Version 5.1
# Release build: builds the Rust sidecars (overlay + denoise), embeds them into
# the Go binary with the Windows icon/manifest, and produces the release exe.
[CmdletBinding()]
param(
    [switch]$Test,
    [switch]$Clean,
    [string]$OutDir,
    [string]$Version
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot     = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$OverlayDir   = Join-Path $RepoRoot "runtime\overlay"
$OverlayEmbed = Join-Path $RepoRoot "internal\platform\overlaybinary\penguin-translate-overlay.exe"
$DenoiseDir   = Join-Path $RepoRoot "runtime\denoise"
$DenoiseEmbed = Join-Path $RepoRoot "internal\platform\denoisebinary\penguin-translate-denoise.exe"
if (-not $OutDir) { $OutDir = Join-Path $RepoRoot "build" }
$OutExe       = Join-Path $OutDir "penguin-translate.exe"
$AppIconPng   = Join-Path $RepoRoot "build\appicon.png"
$WinIconIco   = Join-Path $RepoRoot "build\windows\icon.ico"
$WinManifest  = Join-Path $RepoRoot "build\windows\wails.exe.manifest"
$SysoOut      = Join-Path $RepoRoot "cmd\app\rsrc_windows_amd64.syso"
$WebAppIcon   = Join-Path $RepoRoot "web\ui\appicon.png"

if (-not $Version) {
    $wailsMeta = Get-Content (Join-Path $RepoRoot "wails.json") -Raw | ConvertFrom-Json
    $Version = $wailsMeta.info.productVersion
}
if (-not $Version) { $Version = "0.1.0" }

$script:Timings = [System.Collections.Generic.List[object]]::new()

function Invoke-Step {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][scriptblock]$Action
    )
    Write-Host ""
    Write-Host "==> $Name" -ForegroundColor Cyan
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        & $Action
    } finally {
        $sw.Stop()
        $script:Timings.Add([pscustomobject]@{
            Step    = $Name
            Seconds = [math]::Round($sw.Elapsed.TotalSeconds, 2)
        })
        Write-Host ("    done ({0:N2}s)" -f $sw.Elapsed.TotalSeconds) -ForegroundColor DarkGray
    }
}

function Stop-RunningTranslationOverlay {
    param([string]$Reason = "pre-build")
    $images = @(
        "penguin-translate.exe",
        "penguin-translate-overlay.exe",
        "penguin-translate-denoise.exe"
    )
    $names = $images | ForEach-Object { $_ -replace '\.exe$','' }
    # taskkill.exe costs ~2s per call even on no match (walks the whole process
    # table); Get-Process is ~90ms, so check first and only pay for taskkill if running.
    $running = Get-Process -Name $names -ErrorAction SilentlyContinue
    if (-not $running) { return }
    Write-Host "Force-stopping translation-overlay processes ($Reason)..."
    # One taskkill for all images; /T also takes down the child Python engine.
    $tkArgs = @("/F", "/T")
    foreach ($im in $images) { $tkArgs += "/IM"; $tkArgs += $im }
    & taskkill.exe @tkArgs 2>$null | Out-Null
    $running | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 250
}

function Find-Rsrc {
    if (Get-Command rsrc -ErrorAction SilentlyContinue) { return "rsrc" }
    $p = Join-Path $env:USERPROFILE "go\bin\rsrc.exe"
    if (Test-Path -LiteralPath $p) { return $p }
    return $null
}

function Ensure-Rsrc {
    $rsrc = Find-Rsrc
    if ($null -ne $rsrc) { return $rsrc }
    Write-Host "Installing rsrc (Windows icon/manifest embedder)..."
    go install github.com/akavel/rsrc@latest
    if ($LASTEXITCODE -ne 0) { throw "go install github.com/akavel/rsrc failed" }
    $rsrc = Find-Rsrc
    if ($null -eq $rsrc) { throw "rsrc not found after install" }
    return $rsrc
}

function Sync-WebAppIcon {
    if (-not (Test-Path -LiteralPath $AppIconPng)) {
        throw "Missing $AppIconPng - add a 512x512 PNG app icon at build/appicon.png"
    }
    $copy = $true
    if (Test-Path -LiteralPath $WebAppIcon) {
        $copy = (Get-Item -LiteralPath $AppIconPng).LastWriteTimeUtc -gt (Get-Item -LiteralPath $WebAppIcon).LastWriteTimeUtc
    }
    if ($copy) {
        Copy-Item -Force -LiteralPath $AppIconPng -Destination $WebAppIcon
    }
}

function Ensure-WindowsResources {
    if (-not (Test-Path -LiteralPath $AppIconPng)) { throw "Missing $AppIconPng" }
    $regenIco = $true
    if (Test-Path -LiteralPath $WinIconIco) {
        $regenIco = (Get-Item -LiteralPath $AppIconPng).LastWriteTimeUtc -gt (Get-Item -LiteralPath $WinIconIco).LastWriteTimeUtc
    }
    if ($regenIco) {
        Write-Host "Generating build/windows/icon.ico from appicon.png..."
        go run ./tools/png2ico $AppIconPng $WinIconIco
        if ($LASTEXITCODE -ne 0) { throw "png2ico failed (exit $LASTEXITCODE)" }
    }
    $regenSyso = $true
    if (Test-Path -LiteralPath $SysoOut) {
        $icoTime  = (Get-Item -LiteralPath $WinIconIco).LastWriteTimeUtc
        $manTime  = (Get-Item -LiteralPath $WinManifest).LastWriteTimeUtc
        $sysoTime = (Get-Item -LiteralPath $SysoOut).LastWriteTimeUtc
        $regenSyso = $icoTime -gt $sysoTime -or $manTime -gt $sysoTime
    }
    if ($regenSyso) {
        Write-Host "Embedding icon + manifest into $SysoOut..."
        $rsrc = Ensure-Rsrc
        & $rsrc -arch amd64 -ico $WinIconIco -manifest $WinManifest -o $SysoOut
        if ($LASTEXITCODE -ne 0) { throw "rsrc embed failed (exit $LASTEXITCODE)" }
    }
}

function Build-RustSidecar {
    param(
        [Parameter(Mandatory)][string]$CrateDir,
        [Parameter(Mandatory)][string]$BinaryName,
        [Parameter(Mandatory)][string]$EmbedPath,
        [Parameter(Mandatory)][string]$FailureNote
    )
    Push-Location $CrateDir
    try {
        cargo build --release
        $exit = $LASTEXITCODE
    } finally {
        Pop-Location
    }
    if ($exit -ne 0) {
        Write-Warning "$FailureNote (cargo exit $exit)."
        return $false
    }
    $built = Join-Path $CrateDir ("target\release\" + $BinaryName)
    if (-not (Test-Path -LiteralPath $built)) {
        Write-Warning "Rust binary missing after build: $built"
        return $false
    }
    Stop-RunningTranslationOverlay -Reason "before embedding $BinaryName"
    New-Item -ItemType Directory -Force -Path (Split-Path $EmbedPath) | Out-Null
    Copy-Item -Force $built $EmbedPath
    Write-Host "Embedded into Go build: $EmbedPath"
    return $true
}

function Invoke-GoRelease {
    param(
        [string]$CgoEnabled = "0",
        [bool]$OverlayEmbedded = $false,
        [bool]$DenoiseEmbedded = $false
    )
    # Same tags as `wails build` production.
    $wailsTags = "desktop,production,wv2runtime.download"
    if ($OverlayEmbedded) { $wailsTags += ",overlay_embedded" }
    if ($DenoiseEmbedded) { $wailsTags += ",denoise_embedded" }
    $env:CGO_ENABLED = $CgoEnabled
    $ld = @(
        "-s", "-w", "-H", "windowsgui",
        "-X", "translation-overlay/internal/platform/version.Version=$Version"
    ) -join " "
    Write-Host "go build (release, CGO_ENABLED=$CgoEnabled, tags=$wailsTags, v$Version)..."
    go build -trimpath -buildvcs=false -tags $wailsTags -ldflags $ld -o $OutExe ./cmd/app/
    if ($LASTEXITCODE -ne 0) { throw "go build failed (exit $LASTEXITCODE)" }
}

Set-Location $RepoRoot
$totalSw = [System.Diagnostics.Stopwatch]::StartNew()
$ok = $false
try {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "go not found on PATH - install Go 1.25+"
    }
    $haveCargo = [bool](Get-Command cargo -ErrorAction SilentlyContinue)
    if (-not $haveCargo) {
        Write-Warning "cargo not found - desktop caption overlay and noise cancellation will be skipped"
    }

    Stop-RunningTranslationOverlay

    if ($Clean -and (Test-Path -LiteralPath $OutExe)) {
        Write-Host "Removing prior output: $OutExe"
        Remove-Item -Force -LiteralPath $OutExe
    }
    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

    Sync-WebAppIcon

    Invoke-Step "Windows resources (icon + manifest)" { Ensure-WindowsResources }

    $OverlayEmbedded = $false
    $DenoiseEmbedded = $false
    if ($haveCargo) {
        Invoke-Step "Rust overlay sidecar (build + embed)" {
            $script:OverlayEmbedded = Build-RustSidecar `
                -CrateDir $OverlayDir `
                -BinaryName "penguin-translate-overlay.exe" `
                -EmbedPath $OverlayEmbed `
                -FailureNote "Desktop overlay (Rust) build failed; install MSVC v143 build tools so link.exe is on PATH. Main app still builds; desktop/SteamVR captions unavailable until fixed"
        }
        Invoke-Step "Rust denoise sidecar (build + embed)" {
            $script:DenoiseEmbedded = Build-RustSidecar `
                -CrateDir $DenoiseDir `
                -BinaryName "penguin-translate-denoise.exe" `
                -EmbedPath $DenoiseEmbed `
                -FailureNote "Denoise sidecar (Rust) build failed. Main app still builds; noise cancellation unavailable (audio passes through) until fixed"
        }
    }

    # Portable: the Go binary. CGO stays off (no cgo in the tree; OpenVR lives
    # in the Rust sidecar), so no MinGW toolchain is required.
    Stop-RunningTranslationOverlay -Reason "before go build"
    Invoke-Step "Go build (release)" {
        Invoke-GoRelease -CgoEnabled "0" -OverlayEmbedded:$OverlayEmbedded -DenoiseEmbedded:$DenoiseEmbedded
    }

    if (-not (Test-Path -LiteralPath $OutExe)) {
        throw "Build did not produce: $OutExe"
    }
    $sizeMb = [math]::Round((Get-Item -LiteralPath $OutExe).Length / 1MB, 2)
    $overlayNote = if ($OverlayEmbedded) { "overlay embedded" } else { "overlay NOT embedded" }
    $denoiseNote = if ($DenoiseEmbedded) { "denoise embedded" } else { "denoise NOT embedded" }
    Write-Host ""
    Write-Host "Release: $OutExe ($sizeMb MB, v$Version, icon embedded, $overlayNote, $denoiseNote)" -ForegroundColor Green

    if ($Test) {
        Invoke-Step "Go tests" {
            $testTags = @()
            if ($OverlayEmbedded) { $testTags += "overlay_embedded" }
            if ($DenoiseEmbedded) { $testTags += "denoise_embedded" }
            if ($testTags.Count -gt 0) {
                go test -count=1 ("-tags=" + ($testTags -join ',')) ./internal/platform/... ./internal/composition/...
            } else {
                go test -count=1 ./internal/platform/... ./internal/composition/...
            }
            if ($LASTEXITCODE -ne 0) { throw "go test failed (exit $LASTEXITCODE)" }
        }
        Write-Host "go test OK" -ForegroundColor Green
    }

    $ok = $true
} finally {
    $totalSw.Stop()
    $script:Timings.Add([pscustomobject]@{ Step = "TOTAL"; Seconds = [math]::Round($totalSw.Elapsed.TotalSeconds, 2) })
    Write-Host ""
    Write-Host "Build timing:" -ForegroundColor Cyan
    $script:Timings | Format-Table -AutoSize | Out-String | Write-Host
}

if (-not $ok) { exit 1 }
