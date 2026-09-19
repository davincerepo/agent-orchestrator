# Windows integration smoke: scratch application folders and scratch desktop only.
# Run: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install-fleet.test.ps1
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. "$PSScriptRoot\install-fleet.ps1"

function Assert($Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}
function Must-Fail([scriptblock]$Action, [string]$Pattern) {
    $message = ''
    try { & $Action } catch { $message = $_.Exception.Message }
    Assert ($message -match $Pattern) "Expected failure /$Pattern/, got: $message"
}
function Read-Shortcut([string]$Path) {
    $shell = New-Object -ComObject WScript.Shell
    $link = $null
    try {
        $link = $shell.CreateShortcut($Path)
        return @{ Target = $link.TargetPath; WorkingDirectory = $link.WorkingDirectory; Arguments = $link.Arguments; Icon = $link.IconLocation }
    } finally {
        if ($null -ne $link) { [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($link) }
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell)
    }
}

$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('fleet installer test ' + [Guid]::NewGuid().ToString('N'))
$source = Join-Path $testRoot 'package'
$destination = Join-Path $testRoot 'installed app'
$desktop = Join-Path $testRoot 'redirected desktop'
$repository = Join-Path $testRoot 'repo'
$junction = Join-Path $testRoot 'junction'
$process = $null
$originalBuild = ${function:Invoke-FleetBuild}
$originalHome = $env:AO_FLEET_HOME
$originalPath = $env:PATH
$originalCLIValidation = ${function:Assert-FleetCLI}
$originalUserPath = ${function:Get-FleetUserPath}
$originalMachinePath = ${function:Get-FleetMachinePath}
$originalSetUserPath = ${function:Set-FleetUserPath}
$originalBroadcast = ${function:Send-FleetEnvironmentChange}
$originalDefaultInstallDir = $script:FleetDefaultInstallDir
try {
    foreach ($directory in @($source, $desktop, $repository)) { [void][IO.Directory]::CreateDirectory($directory) }
    foreach ($file in $script:FleetFiles) {
        $path = Join-Path $source $file
        [void][IO.Directory]::CreateDirectory((Split-Path -Parent $path))
        Set-Content -LiteralPath $path -Value 'version one'
    }
    Set-Content -LiteralPath (Join-Path $source 'README-Fleet.txt') -Value 'Fleet test - Windows x64 portable'
    Set-Content -LiteralPath (Join-Path $desktop 'Official AO.lnk') -Value 'leave alone'
    Install-FleetPackage $source $destination $repository $desktop
    $shortcutPath = Join-Path $desktop 'AO Fleet.lnk'
    $link = Read-Shortcut $shortcutPath
    Assert ($link.Target -eq (Join-Path $destination 'fleet.exe')) 'Shortcut must directly launch fleet.exe.'
    Assert ($link.WorkingDirectory -eq $destination) 'Shortcut working directory is incorrect.'
    Assert ($link.Icon -eq ((Join-Path $destination 'fleet.exe') + ',0')) 'Shortcut icon is incorrect.'
    Write-Host 'PASS: first install and shortcut, including spaces in paths'

    # Wrong and malformed shortcuts must both be repaired on subsequent installs.
    $shell = New-Object -ComObject WScript.Shell
    $broken = $shell.CreateShortcut($shortcutPath)
    $broken.TargetPath = "$env:WINDIR\System32\cmd.exe"
    $broken.Arguments = '/c echo wrong'
    $broken.WorkingDirectory = $testRoot
    $broken.Save()
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($broken)
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell)
    Set-Content -LiteralPath (Join-Path $destination 'obsolete.dll') -Value 'obsolete'
    Set-Content -LiteralPath (Join-Path $source 'fleet.exe') -Value 'version two'
    Install-FleetPackage $source $destination $repository $desktop
    $link = Read-Shortcut $shortcutPath
    Assert ($link.Target -eq (Join-Path $destination 'fleet.exe') -and $link.Arguments -eq '') 'Broken shortcut was not repaired.'
    Assert (-not (Test-Path -LiteralPath (Join-Path $destination 'obsolete.dll'))) 'Old package files remain.'
    Assert ((Get-Content -LiteralPath (Join-Path $destination 'fleet.exe')) -eq 'version two') 'New package was not installed.'
    Assert (@(Get-ChildItem -LiteralPath $testRoot -Directory | Where-Object Name -Match '^installed app\.(install|previous)-').Count -eq 0) 'Temporary/historical folders remain.'
    Write-Host 'PASS: update replaces the full package and repairs shortcut without version archives'

    Set-Content -LiteralPath $shortcutPath -Value 'not a shortcut'
    Install-FleetPackage $source $destination $repository $desktop
    Assert ((Read-Shortcut $shortcutPath).Target -eq (Join-Path $destination 'fleet.exe')) 'Malformed shortcut was not repaired.'
    Remove-Item -LiteralPath $shortcutPath
    Install-FleetPackage $source $destination $repository $desktop
    Assert (Test-Path -LiteralPath $shortcutPath) 'Missing shortcut was not recreated.'
    Assert ((Get-Content -LiteralPath (Join-Path $desktop 'Official AO.lnk')) -eq 'leave alone') 'An unrelated shortcut was modified.'
    Write-Host 'PASS: malformed/missing Fleet shortcut repaired; other shortcuts preserved'

    $foreign = Join-Path $testRoot 'unmanaged'
    [void][IO.Directory]::CreateDirectory($foreign)
    Set-Content -LiteralPath (Join-Path $foreign 'keep.txt') -Value 'keep'
    Must-Fail { Install-FleetPackage $source $foreign $repository $desktop } 'unmanaged directory'
    Assert ((Get-Content -LiteralPath (Join-Path $foreign 'keep.txt')) -eq 'keep') 'Unmanaged data changed.'
    Must-Fail { Assert-FleetDestination (Get-FullPath ([IO.Path]::GetPathRoot($testRoot))) $repository } 'drive root'
    Must-Fail { Assert-FleetDestination $repository $repository } 'overlaps'
    Must-Fail { Assert-FleetDestination (Join-Path ([Environment]::GetFolderPath('UserProfile')) '.ao\fleet') $repository } 'overlaps'
    $env:AO_FLEET_HOME = Join-Path $testRoot 'custom fleet data'
    Must-Fail { Assert-FleetDestination $env:AO_FLEET_HOME $repository } 'overlaps'
    Write-Host 'PASS: source, data, drive roots and unmanaged directories protected'

    New-Item -ItemType Junction -Path $junction -Target $foreign | Out-Null
    Must-Fail { Install-FleetPackage $source $junction $repository $desktop } 'Junctions'
    # Directory.Delete removes only this junction, never its target.
    [IO.Directory]::Delete($junction)
    Assert (Test-Path -LiteralPath (Join-Path $foreign 'keep.txt')) 'Junction target changed.'
    Write-Host 'PASS: junction destinations rejected'

    $requiredFile = Join-Path $source 'resources\app.asar'
    Remove-Item -LiteralPath $requiredFile
    Must-Fail { Install-FleetPackage $source $destination $repository $desktop } 'Incomplete Fleet package'
    Assert ((Get-Content -LiteralPath (Join-Path $destination 'fleet.exe')) -eq 'version two') 'Incomplete package replaced existing app.'
    Set-Content -LiteralPath $requiredFile -Value 'restored'
    Write-Host 'PASS: incomplete package leaves installation intact'

    # Simulate a promotion failure after the old directory has been renamed.
    function Move-Item([string]$LiteralPath, [string]$Destination) {
        if ((Split-Path -Leaf $LiteralPath) -like 'installed app.install-*') { throw 'simulated promotion failure' }
        Microsoft.PowerShell.Management\Move-Item -LiteralPath $LiteralPath -Destination $Destination
    }
    try { Must-Fail { Install-FleetPackage $source $destination $repository $desktop } 'simulated promotion failure' }
    finally { Remove-Item Function:\Move-Item }
    Assert ((Get-Content -LiteralPath (Join-Path $destination 'fleet.exe')) -eq 'version two') 'Previous installation was not restored.'
    Assert (@(Get-ChildItem -LiteralPath $testRoot -Directory | Where-Object Name -Match '^installed app\.(install|previous)-').Count -eq 0) 'Failed promotion left temporary folders.'
    Write-Host 'PASS: failed directory switch restores previous installation'

    # Real process in the target folder, with no application or user-data startup.
    Copy-Item -LiteralPath (Get-Command node.exe -CommandType Application | Select-Object -First 1).Source -Destination (Join-Path $destination 'fleet.exe') -Force
    $process = Start-Process -FilePath (Join-Path $destination 'fleet.exe') -ArgumentList '-e', 'setInterval(()=>{},1000)' -WindowStyle Hidden -PassThru
    Must-Fail { Install-FleetPackage $source $destination $repository $desktop } 'Still running'
    Assert (-not $process.HasExited) 'Installer terminated a running process.'
    Stop-Process -Id $process.Id
    $process.WaitForExit()
    $process = $null
    Install-FleetPackage $source $destination $repository $desktop
    Write-Host 'PASS: active installation refused; process left running; retry succeeds after exit'

    # Registry/environment broadcasts are fake; PATH resolution uses real files
    # in the scratch installation, without executing the placeholder ao.exe.
    $official = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs\agent-orchestrator\resources\daemon'
    $other = Join-Path $testRoot 'unrelated tools'
    [void][IO.Directory]::CreateDirectory($other)
    $script:testUserPath = "$official;$other"
    $script:testMachinePath = ''
    $script:testPathWrites = 0
    $script:testBroadcasts = 0
    function Get-FleetUserPath { return $script:testUserPath }
    function Get-FleetMachinePath { return $script:testMachinePath }
    function Set-FleetUserPath([string]$Value) { $script:testUserPath = $Value; $script:testPathWrites++ }
    function Send-FleetEnvironmentChange { $script:testBroadcasts++ }
    function Assert-FleetCLI([string]$Directory) {
        Assert (Test-Path -LiteralPath (Join-Path $Directory 'resources\daemon\ao.exe')) 'CLI is missing.'
    }
    Register-FleetCLI $destination
    $expectedCLI = Join-Path $destination 'resources\daemon\ao.exe'
    Assert ((Get-Command ao.exe -CommandType Application | Select-Object -First 1).Source -eq $expectedCLI) 'ao resolves to another installation.'
    Assert ($script:testUserPath -eq "$(Split-Path -Parent $expectedCLI);$other") 'User PATH must replace the official CLI entry and preserve unrelated paths.'
    Assert ($script:testPathWrites -eq 1 -and $script:testBroadcasts -eq 1) 'PATH change was not saved and broadcast.'
    Register-FleetCLI $destination
    Assert ($script:testPathWrites -eq 1) 'Repeated install duplicated or rewrote PATH.'

    $moved = Join-Path $testRoot 'moved fleet'
    [void][IO.Directory]::CreateDirectory((Join-Path $moved 'resources\daemon'))
    Copy-Item -LiteralPath $expectedCLI -Destination (Join-Path $moved 'resources\daemon\ao.exe')
    Register-FleetCLI $moved
    Assert ($script:testUserPath -eq "$(Join-Path $moved 'resources\daemon');$other") 'Relocated install left its previous managed CLI on PATH.'
    Assert (Test-Path -LiteralPath $expectedCLI) 'Registration modified the old installation files.'

    $script:testMachinePath = Split-Path -Parent $expectedCLI
    Must-Fail { Assert-FleetCLIPathPriority $moved } 'Machine PATH'
    Assert ($script:testPathWrites -eq 2) 'Machine conflict modified User PATH.'
    $script:testMachinePath = ''
    $beforeFailedPath = $env:PATH
    function Set-FleetUserPath([string]$Value) { throw 'simulated registry write failure' }
    Must-Fail { Register-FleetCLI $destination } 'registry write failure'
    Assert ($env:PATH -eq $beforeFailedPath) 'Failed persistence modified process PATH.'
    ${function:Set-FleetUserPath} = $originalSetUserPath
    # No later test registers PATH; restore the real process PATH now.
    $env:PATH = $originalPath
    Write-Host 'PASS: Fleet CLI selection, official PATH replacement, idempotence, relocation and failed registration; no real user PATH changes'

    & git -C $repository init --initial-branch=main-fleet --quiet
    if ($LASTEXITCODE -ne 0) { throw 'Could not create test repository.' }
    function Invoke-FleetBuild([string]$RepositoryRoot) { throw 'simulated build failure' }
    Must-Fail { Invoke-FleetInstall $repository $destination } 'simulated build failure'
    Assert ((Get-Content -LiteralPath (Join-Path $destination 'fleet.exe')) -eq 'version two') 'Failed build changed the installation.'
    [void][IO.Directory]::CreateDirectory((Join-Path $repository 'scripts'))
    $localConfig = Join-Path $repository 'scripts\install-fleet.local.json'
    Assert ($script:FleetDefaultInstallDir -eq 'C:\ao') 'The built-in default must be C:\ao.'
    $script:FleetDefaultInstallDir = Join-Path $testRoot 'default ao'
    Assert ((Resolve-FleetInstallDirectory $repository '') -eq $script:FleetDefaultInstallDir) 'Missing JSON must use the built-in default.'
    $nestedInstall = Join-Path $script:FleetDefaultInstallDir 'Fleet'
    $savedData = Join-Path $script:FleetDefaultInstallDir 'data\keep.txt'
    [void][IO.Directory]::CreateDirectory((Split-Path -Parent $savedData))
    Set-Content -LiteralPath $savedData -Value 'existing data'
    Install-FleetPackage $source $nestedInstall $repository $desktop
    @{ installDir = $nestedInstall } | ConvertTo-Json | Set-Content -LiteralPath $localConfig
    Remove-Item -LiteralPath $localConfig
    Assert ((Resolve-FleetInstallDirectory $repository '') -eq $nestedInstall) 'Deleting JSON must retain the existing managed Fleet child.'
    Must-Fail { Invoke-FleetInstall $repository '' } 'simulated build failure'
    Install-FleetPackage $source (Resolve-FleetInstallDirectory $repository '') $repository $desktop
    Assert ((Get-Content -LiteralPath $savedData) -eq 'existing data') 'Nested update changed sibling data.'
    Must-Fail { Invoke-FleetInstall $repository $script:FleetDefaultInstallDir } 'unmanaged directory'
    @{ installDir = $script:FleetDefaultInstallDir } | ConvertTo-Json | Set-Content -LiteralPath $localConfig
    Must-Fail { Invoke-FleetInstall $repository '' } 'unmanaged directory'
    @{ installDir = '' } | ConvertTo-Json | Set-Content -LiteralPath $localConfig
    Assert ((Resolve-FleetInstallDirectory $repository '') -eq $nestedInstall) 'Empty installDir must use the default lookup.'
    $nestedMarker = Join-Path $nestedInstall $script:FleetMarker
    foreach ($badMarker in @('not json', '{}', '{"kind":"another-app"}')) {
        Set-Content -LiteralPath $nestedMarker -Value $badMarker
        Must-Fail { Invoke-FleetInstall $repository '' } 'unmanaged directory'
    }
    @{ kind = 'ao-fleet-local-install' } | ConvertTo-Json | Set-Content -LiteralPath $nestedMarker
    @{ kind = 'ao-fleet-local-install' } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $script:FleetDefaultInstallDir $script:FleetMarker)
    Assert ((Resolve-FleetInstallDirectory $repository '') -eq $script:FleetDefaultInstallDir) 'A directly managed default installation must take precedence.'
    $script:FleetDefaultInstallDir = $originalDefaultInstallDir
    Write-Host 'PASS: built-in default, deleted/empty JSON, existing Fleet child, sibling data preservation and invalid marker refusal'
    @{ installDir = $foreign } | ConvertTo-Json | Set-Content -LiteralPath $localConfig
    Must-Fail { Invoke-FleetInstall $repository '' } 'unmanaged directory'
    @{ installDir = $destination } | ConvertTo-Json | Set-Content -LiteralPath $localConfig
    Must-Fail { Invoke-FleetInstall $repository '' } 'simulated build failure'
    Must-Fail { Invoke-FleetInstall $repository $foreign } 'unmanaged directory'
    Write-Host 'PASS: local installation path respected; explicit command-line path takes precedence'
    & git -C $repository symbolic-ref HEAD refs/heads/fleet/feat/portable
    Must-Fail { Invoke-FleetInstall $repository $destination } 'main-fleet checkout'
    Write-Host 'PASS: build failure preserves installation; feature-only checkout refused'
    Write-Host 'All Fleet installer smoke checks passed.'
} finally {
    ${function:Invoke-FleetBuild} = $originalBuild
    $env:AO_FLEET_HOME = $originalHome
    $env:PATH = $originalPath
    ${function:Assert-FleetCLI} = $originalCLIValidation
    ${function:Get-FleetUserPath} = $originalUserPath
    ${function:Get-FleetMachinePath} = $originalMachinePath
    ${function:Set-FleetUserPath} = $originalSetUserPath
    ${function:Send-FleetEnvironmentChange} = $originalBroadcast
    $script:FleetDefaultInstallDir = $originalDefaultInstallDir
    if ($null -ne $process -and -not $process.HasExited) { Stop-Process -Id $process.Id; $process.WaitForExit() }
    if (Test-Path -LiteralPath $junction) { [IO.Directory]::Delete($junction) }
    $resolved = Get-FullPath $testRoot
    $temp = Get-FullPath ([IO.Path]::GetTempPath())
    if (-not (Test-Within $resolved $temp) -or (Split-Path -Leaf $resolved) -notlike 'fleet installer test *') { throw 'Unsafe test cleanup path.' }
    Assert-NoLinks $resolved -Recurse
    if (Test-Path -LiteralPath $resolved) { Remove-Item -LiteralPath $resolved -Recurse -Force }
}
