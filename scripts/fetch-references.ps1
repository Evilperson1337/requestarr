$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$Out = Join-Path $Root "references/source/checkouts"
New-Item -ItemType Directory -Force -Path $Out | Out-Null

$Refs = @(
    @{ Id="seerr"; Url="https://github.com/seerr-team/seerr.git"; Branch="develop" },
    @{ Id="radarr"; Url="https://github.com/Radarr/Radarr.git"; Branch="develop" },
    @{ Id="sonarr"; Url="https://github.com/Sonarr/Sonarr.git"; Branch="develop" },
    @{ Id="lidarr"; Url="https://github.com/Lidarr/Lidarr.git"; Branch="develop" },
    @{ Id="ggrequestz"; Url="https://github.com/XTREEMMAK/ggrequestz.git"; Branch="main" },
    @{ Id="romm"; Url="https://github.com/rommapp/romm.git"; Branch="master" },
    @{ Id="apprise"; Url="https://github.com/caronc/apprise.git"; Branch="master" },
    @{ Id="apprise-api"; Url="https://github.com/caronc/apprise-api.git"; Branch="master" }
)

foreach ($Ref in $Refs) {
    $Dest = Join-Path $Out $Ref.Id
    if (Test-Path (Join-Path $Dest ".git")) {
        Write-Host "[$($Ref.Id)] updating"
        git -C $Dest fetch --depth 1 origin $Ref.Branch
        git -C $Dest checkout -q $Ref.Branch
        git -C $Dest reset --hard "origin/$($Ref.Branch)"
    } else {
        Write-Host "[$($Ref.Id)] cloning"
        git clone --depth 1 --single-branch --branch $Ref.Branch $Ref.Url $Dest
    }
    $Commit = git -C $Dest rev-parse HEAD
    Write-Host "$($Ref.Id): $Commit"
}
