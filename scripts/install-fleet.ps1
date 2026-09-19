[CmdletBinding()]
param([string]$InstallDir)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$script:FleetFiles = @('fleet.exe', 'resources\app.asar', 'resources\daemon\ao.exe', 'resources\acp-runtime\node\node.exe', 'README-Fleet.txt')
$script:FleetMarker = '.fleet-install.json'
$script:FleetDefaultInstallDir = 'C:\ao'

function Get-FullPath([string]$Path) {
    if ($Path -notmatch '^[A-Za-z]:[\\/]') { throw "Use an absolute local drive path: $Path" }
    return [IO.Path]::GetFullPath($Path).TrimEnd('\', '/')
}

function Test-Within([string]$Path, [string]$Root) {
    return $Path.Equals($Root, [StringComparison]::OrdinalIgnoreCase) -or
        $Path.StartsWith($Root + '\', [StringComparison]::OrdinalIgnoreCase)
}

function Assert-NoLinks([string]$Path, [switch]$Recurse) {
    $cursor = $Path
    while ($cursor) {
        if (Test-Path -LiteralPath $cursor) {
            if ((Get-Item -LiteralPath $cursor -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) {
                throw "Junctions and symbolic links are not supported: $cursor"
            }
        }
        $cursor = Split-Path -Parent $cursor
    }
    if ($Recurse -and (Test-Path -LiteralPath $Path -PathType Container)) {
        foreach ($child in Get-ChildItem -LiteralPath $Path -Force) {
            if ($child.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Link in application folder: $($child.FullName)" }
            if ($child.PSIsContainer) { Assert-NoLinks $child.FullName -Recurse }
        }
    }
}

function Assert-FleetDestination([string]$Destination, [string]$RepositoryRoot) {
    if ($Destination.Length -le 3) { throw 'The installation directory cannot be a drive root.' }
    $protected = @($RepositoryRoot, (Join-Path ([Environment]::GetFolderPath('UserProfile')) '.ao'))
    if ($env:AO_FLEET_HOME) { $protected += $env:AO_FLEET_HOME }
    foreach ($path in $protected) {
        $path = Get-FullPath $path
        if ((Test-Within $Destination $path) -or (Test-Within $path $Destination)) {
            throw "Installation directory overlaps source code or application data: $path"
        }
    }
    Assert-NoLinks $Destination -Recurse
    if (Test-Path -LiteralPath $Destination) {
        if (-not (Test-Path -LiteralPath $Destination -PathType Container)) { throw "Not a directory: $Destination" }
        if (@(Get-ChildItem -LiteralPath $Destination -Force).Count -gt 0) {
            $marker = Join-Path $Destination $script:FleetMarker
            if (-not (Test-Path -LiteralPath $marker -PathType Leaf) -or
                (Get-Content -LiteralPath $marker -Raw | ConvertFrom-Json).kind -ne 'ao-fleet-local-install') {
                throw "Refusing to replace an unmanaged directory: $Destination. Use an empty directory for the first installation."
            }
        }
    }
}

function Assert-FleetStopped([string]$Directory) {
    $active = @(Get-CimInstance Win32_Process | Where-Object {
        $_.ExecutablePath -and (Test-Within $_.ExecutablePath $Directory)
    })
    if ($active.Count) {
        $details = ($active | ForEach-Object { "$($_.Name) (PID $($_.ProcessId))" }) -join ', '
        throw "Close Fleet and its background agents/terminals, then run this script again. Still running in ${Directory}: $details"
    }
}

function Assert-FleetPackage([string]$Directory) {
    Assert-NoLinks $Directory -Recurse
    foreach ($file in $script:FleetFiles) {
        if (-not (Test-Path -LiteralPath (Join-Path $Directory $file) -PathType Leaf)) { throw "Incomplete Fleet package: $file" }
    }
    if ((Get-Content -LiteralPath (Join-Path $Directory 'README-Fleet.txt') -TotalCount 1) -notmatch '^Fleet .+ - Windows x64 portable$') {
        throw "Not a Fleet portable package: $Directory"
    }
}

function Repair-FleetShortcut([string]$Directory, [string]$DesktopDirectory) {
    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $null
    try {
        if (-not $DesktopDirectory) { $DesktopDirectory = $shell.SpecialFolders.Item('Desktop') }
        if (-not (Test-Path -LiteralPath $DesktopDirectory -PathType Container)) { throw "Desktop directory is unavailable: $DesktopDirectory" }
        $path = Join-Path $DesktopDirectory 'AO Fleet.lnk'
        # Recreate even a malformed shortcut; other desktop shortcuts are untouched.
        if (Test-Path -LiteralPath $path -PathType Leaf) { Remove-Item -LiteralPath $path -Force }
        $shortcut = $shell.CreateShortcut($path)
        $shortcut.TargetPath = Join-Path $Directory 'fleet.exe'
        $shortcut.WorkingDirectory = $Directory
        $shortcut.IconLocation = (Join-Path $Directory 'fleet.exe') + ',0'
        $shortcut.Arguments = ''
        $shortcut.Description = 'AO Fleet'
        $shortcut.WindowStyle = 1
        $shortcut.Save()
        Write-Host "Desktop shortcut: $path"
    } finally {
        if ($null -ne $shortcut) { [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($shortcut) }
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell)
    }
}

# Separate the registry side effects so the installer smoke test can exercise
# real PATH selection without changing the user's persistent environment.
function Get-FleetUserPath { return [Environment]::GetEnvironmentVariable('Path', 'User') }
function Get-FleetMachinePath { return [Environment]::GetEnvironmentVariable('Path', 'Machine') }
function Set-FleetUserPath([string]$Value) { [Environment]::SetEnvironmentVariable('Path', $Value, 'User') }

function Send-FleetEnvironmentChange {
    if (-not ('FleetInstall.EnvironmentBroadcast' -as [type])) {
        Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace FleetInstall {
    public static class EnvironmentBroadcast {
        [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern IntPtr SendMessageTimeout(IntPtr window, uint message, IntPtr wParam, string lParam, uint flags, uint timeout, out UIntPtr result);
    }
}
'@
    }
    $result = [UIntPtr]::Zero
    [void][FleetInstall.EnvironmentBroadcast]::SendMessageTimeout([IntPtr]0xffff, 0x1a, [IntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
}

function Get-FleetCommandPath([string]$Directory, [string]$CurrentPath) {
    $cliDirectory = Get-FullPath (Join-Path $Directory 'resources\daemon')
    $officialDirectory = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs\agent-orchestrator\resources\daemon'
    $entries = [Collections.Generic.List[string]]::new()
    $entries.Add($cliDirectory)
    foreach ($entry in ($CurrentPath -split ';')) {
        if (-not $entry.Trim()) { continue }
        $expanded = [Environment]::ExpandEnvironmentVariables($entry.Trim().Trim('"')).TrimEnd('\', '/')
        if ($expanded.Equals($cliDirectory, [StringComparison]::OrdinalIgnoreCase) -or
            $expanded.Equals($officialDirectory, [StringComparison]::OrdinalIgnoreCase)) { continue }
        # Remove an earlier managed Fleet CLI when the installation moves.
        if ($expanded -match '[\\/]resources[\\/]daemon$') {
            $marker = Join-Path (Split-Path -Parent (Split-Path -Parent $expanded)) $script:FleetMarker
            if (Test-Path -LiteralPath $marker -PathType Leaf) {
                try { if ((Get-Content -LiteralPath $marker -Raw | ConvertFrom-Json).kind -eq 'ao-fleet-local-install') { continue } }
                catch { } # A foreign/broken marker cannot authorize a PATH removal.
            }
        }
        $entries.Add($entry)
    }
    return $entries -join ';'
}

function Assert-FleetCLIPathPriority([string]$Directory) {
    $cliDirectory = Get-FullPath (Join-Path $Directory 'resources\daemon')
    # Windows places Machine PATH before User PATH in newly opened processes.
    foreach ($entry in ((Get-FleetMachinePath) -split ';')) {
        $expanded = [Environment]::ExpandEnvironmentVariables($entry.Trim().Trim('"')).TrimEnd('\', '/')
        if (-not $expanded -or $expanded.Equals($cliDirectory, [StringComparison]::OrdinalIgnoreCase)) { continue }
        foreach ($name in @('ao.exe', 'ao.cmd', 'ao.bat', 'ao.ps1')) {
            if (Test-Path -LiteralPath (Join-Path $expanded $name) -PathType Leaf) {
                throw "Machine PATH has an ao command before User PATH: $expanded. Remove that machine-level AO PATH entry before installing the Fleet CLI."
            }
        }
    }
}

function Assert-FleetCLI([string]$Directory) {
    $binary = Join-Path $Directory 'resources\daemon\ao.exe'
    $version = & $binary -v
    if ($LASTEXITCODE -ne 0 -or "$version" -notmatch '^AO Fleet ') {
        throw "The package CLI is not a Fleet build: $binary. Rebuild with package:fleet before installing."
    }
}

function Register-FleetCLI([string]$Directory) {
    Assert-FleetCLIPathPriority $Directory
    Assert-FleetCLI $Directory
    $oldPath = Get-FleetUserPath
    $newPath = Get-FleetCommandPath $Directory $oldPath
    if ($oldPath -cne $newPath) {
        Set-FleetUserPath $newPath
        Send-FleetEnvironmentChange
    }
    $env:PATH = Get-FleetCommandPath $Directory $env:PATH
    Write-Host "Default ao command: $(Join-Path $Directory 'resources\daemon\ao.exe')"
    Write-Host 'Open a new terminal to use Fleet from other applications; existing terminals retain their old PATH.'
}

function Install-FleetPackage([string]$Source, [string]$Destination, [string]$RepositoryRoot, [string]$DesktopDirectory) {
    $Source = Get-FullPath $Source
    $Destination = Get-FullPath $Destination
    Assert-FleetDestination $Destination $RepositoryRoot
    if ((Test-Within $Source $Destination) -or (Test-Within $Destination $Source)) { throw 'Package and installation directories overlap.' }
    Assert-FleetPackage $Source
    Assert-FleetStopped $Destination
    $parent = Split-Path -Parent $Destination
    $name = Split-Path -Leaf $Destination
    $id = [Guid]::NewGuid().ToString('N')
    $staging = Join-Path $parent "$name.install-$id"
    $previous = Join-Path $parent "$name.previous-$id"
    [void][IO.Directory]::CreateDirectory($staging)
    try {
        foreach ($item in Get-ChildItem -LiteralPath $Source -Force) { Copy-Item -LiteralPath $item.FullName -Destination $staging -Recurse -Force }
        Assert-FleetPackage $staging
        @{ kind = 'ao-fleet-local-install'; installedAt = [DateTime]::UtcNow.ToString('o') } |
            ConvertTo-Json | Set-Content -LiteralPath (Join-Path $staging $script:FleetMarker) -Encoding UTF8
        # Recheck after copying: the user may have launched Fleet while the build ran.
        Assert-FleetDestination $Destination $RepositoryRoot
        Assert-FleetStopped $Destination
        if (Test-Path -LiteralPath $Destination) { Move-Item -LiteralPath $Destination -Destination $previous }
        try { Move-Item -LiteralPath $staging -Destination $Destination }
        catch {
            if ((Test-Path -LiteralPath $previous) -and -not (Test-Path -LiteralPath $Destination)) {
                Move-Item -LiteralPath $previous -Destination $Destination
            }
            throw
        }
        # Temporary rollback directory only, never a retained version archive.
        if (Test-Path -LiteralPath $previous) {
            Assert-NoLinks $previous -Recurse
            if ((Get-FullPath (Split-Path -Parent $previous)) -ne (Get-FullPath $parent) -or
                (Split-Path -Leaf $previous) -ne "$name.previous-$id") { throw 'Invalid temporary directory.' }
            Remove-Item -LiteralPath $previous -Recurse -Force
        }
        Repair-FleetShortcut $Destination $DesktopDirectory
        Write-Host "Installed AO Fleet: $(Join-Path $Destination 'fleet.exe')"
    } finally {
        if (Test-Path -LiteralPath $staging) {
            Assert-NoLinks $staging -Recurse
            if ((Get-FullPath (Split-Path -Parent $staging)) -ne (Get-FullPath $parent) -or
                (Split-Path -Leaf $staging) -ne "$name.install-$id") { throw 'Invalid temporary directory.' }
            Remove-Item -LiteralPath $staging -Recurse -Force
        }
    }
}

function Invoke-FleetBuild([string]$RepositoryRoot) {
    $config = @{}
    $configPath = Join-Path $RepositoryRoot 'scripts\install-fleet.local.json'
    if (Test-Path -LiteralPath $configPath) {
        $settings = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
        foreach ($property in $settings.PSObject.Properties) { $config[$property.Name] = $property.Value }
    }
    $toolPaths = @{}
    foreach ($tool in @('node', 'go')) {
        if ($config.ContainsKey($tool)) { $toolPaths[$tool] = Get-FullPath $config[$tool] }
        else { $toolPaths[$tool] = (Get-Command "$tool.exe" -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source }
        if (-not (Test-Path -LiteralPath $toolPaths[$tool] -PathType Leaf)) { throw "Missing ${tool}: $($toolPaths[$tool])" }
    }
    $oldPath = $env:PATH
    try {
        $env:PATH = (Split-Path -Parent $toolPaths.node) + ';' + (Split-Path -Parent $toolPaths.go) + ';' + $oldPath
        $nodeVersion = & $toolPaths.node -p 'process.versions.node'
        if ($LASTEXITCODE -ne 0 -or [int]($nodeVersion.Split('.')[0]) -lt 24) { throw 'Node.js 24+ is required. Set node in scripts/install-fleet.local.json or update PATH.' }
        $goVersion = & $toolPaths.go version
        $required = [regex]::Match((Get-Content -LiteralPath (Join-Path $RepositoryRoot 'go.work') -Raw), '(?m)^go\s+(\d+\.\d+(?:\.\d+)?)').Groups[1].Value
        $actual = [regex]::Match("$goVersion", 'go(\d+\.\d+(?:\.\d+)?)').Groups[1].Value
        if ($LASTEXITCODE -ne 0 -or -not $actual -or [version]$actual -lt [version]$required) { throw "Go $required+ is required. Set go in scripts/install-fleet.local.json or update PATH." }
        $npm = Join-Path (Split-Path -Parent $toolPaths.node) 'node_modules\npm\bin\npm-cli.js'
        if (-not (Test-Path -LiteralPath $npm)) {
            # A standalone Node runtime can use npm from another installation,
            # but always execute its JS with the selected Node 24+ binary.
            $npmCommand = (Get-Command npm.cmd -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
            $npm = Join-Path (Split-Path -Parent $npmCommand) 'node_modules\npm\bin\npm-cli.js'
        }
        if (-not (Test-Path -LiteralPath $npm)) { throw "npm CLI is missing: $npm" }
        foreach ($project in @('packages\product-ui', 'frontend')) {
            Write-Host "Preparing dependencies: $project"
            & $toolPaths.node $npm ci --prefix (Join-Path $RepositoryRoot $project)
            if ($LASTEXITCODE -ne 0) { throw "npm ci failed: $project (exit $LASTEXITCODE)" }
        }
        Write-Host 'Building Fleet from the current main-fleet checkout...'
        # npm supplies npm_execpath so nested runtime builds use this same CLI.
        & $toolPaths.node $npm --prefix (Join-Path $RepositoryRoot 'frontend') run package:fleet
        if ($LASTEXITCODE -ne 0) { throw "Fleet packaging failed (exit $LASTEXITCODE). Existing installation was not changed." }
    } finally { $env:PATH = $oldPath }
}

function Resolve-FleetInstallDirectory([string]$RepositoryRoot, [string]$Destination) {
    if (-not [string]::IsNullOrWhiteSpace($Destination)) { return Get-FullPath $Destination }
    $settingsPath = Join-Path $RepositoryRoot 'scripts\install-fleet.local.json'
    if (Test-Path -LiteralPath $settingsPath) {
        $settings = Get-Content -LiteralPath $settingsPath -Raw | ConvertFrom-Json
        if ($settings.PSObject.Properties['installDir'] -and
            -not [string]::IsNullOrWhiteSpace([string]$settings.installDir)) {
            return Get-FullPath ([string]$settings.installDir)
        }
    }
    $defaultDirectory = Get-FullPath $script:FleetDefaultInstallDir
    # Older local configurations installed under C:\ao\Fleet beside C:\ao\data.
    # Reuse only a marked child installation; never adopt/replace its parent.
    if (-not (Test-Path -LiteralPath (Join-Path $defaultDirectory $script:FleetMarker))) {
        $nestedDirectory = Join-Path $defaultDirectory 'Fleet'
        $nestedMarker = Join-Path $nestedDirectory $script:FleetMarker
        if (Test-Path -LiteralPath $nestedMarker -PathType Leaf) {
            try {
                $marker = Get-Content -LiteralPath $nestedMarker -Raw | ConvertFrom-Json
                if ($marker.kind -eq 'ao-fleet-local-install') { return $nestedDirectory }
            } catch { } # An invalid/foreign marker must not authorize replacement.
        }
    }
    return $defaultDirectory
}

function Invoke-FleetInstall([string]$RepositoryRoot, [string]$Destination) {
    $RepositoryRoot = Get-FullPath $RepositoryRoot
    $Destination = Resolve-FleetInstallDirectory $RepositoryRoot $Destination
    $branch = & git -C $RepositoryRoot branch --show-current
    if ($LASTEXITCODE -ne 0 -or $branch -ne 'main-fleet') { throw 'Run this script from the main-fleet checkout; it contains all integrated Fleet features.' }
    Assert-FleetDestination $Destination $RepositoryRoot
    Assert-FleetStopped $Destination
    Assert-FleetCLIPathPriority $Destination
    # Serialize installers targeting the same path, including different worktrees.
    $hash = [Security.Cryptography.SHA256]::Create()
    try { $key = [BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($Destination.ToLowerInvariant()))).Replace('-', '') }
    finally { $hash.Dispose() }
    $mutex = New-Object Threading.Mutex($false, "Local\AO-Fleet-Install-$key")
    $owned = $false
    try {
        try { $owned = $mutex.WaitOne(0) } catch [Threading.AbandonedMutexException] { $owned = $true }
        if (-not $owned) { throw "Another Fleet installation is already in progress: $Destination" }
        Push-Location -LiteralPath $RepositoryRoot
        try { Invoke-FleetBuild $RepositoryRoot } finally { Pop-Location }
        Assert-FleetCLI (Join-Path $RepositoryRoot 'frontend\out\Fleet-win32-x64')
        Install-FleetPackage (Join-Path $RepositoryRoot 'frontend\out\Fleet-win32-x64') $Destination $RepositoryRoot
        Register-FleetCLI $Destination
    } finally {
        if ($owned) { $mutex.ReleaseMutex() }
        $mutex.Dispose()
    }
}

# Dot-sourcing exposes the same installer functions to the isolated Windows smoke test.
if ($MyInvocation.InvocationName -ne '.') {
    try { Invoke-FleetInstall (Split-Path -Parent $PSScriptRoot) $InstallDir }
    catch { Write-Host "Fleet installation failed: $($_.Exception.Message)" -ForegroundColor Red; exit 1 }
}
