#requires -Version 7
<#
.SYNOPSIS
  Release a new version of RustDesk Manager.

.DESCRIPTION
  Run it on the develop branch with a clean working tree. It:
    1. checks git (clean tree, on develop, not behind the remote) and works out the new version;
    2. turns the changelog's "## [Unreleased]" notes into the new version's section
       (if that section is empty, it offers the commit messages since the last release);
    3. runs the web build and the tests;
    4. updates VERSION, CHANGELOG.md and web/package.json;
    5. after you confirm: commits on develop, merges develop into main, tags vX.Y.Z;
    6. builds the Docker image and pushes it as <image>:X.Y.Z and <image>:latest;
    7. pushes main, develop and the tag;
    8. creates a GitHub release for the tag with the changelog notes and the image
       file attached (needs the GitHub CLI, `gh`, logged in once with `gh auth login`).

  The image is pushed before git, so a version tag on the remote always has its image.
  Log in to the registry once beforehand with `docker login`.

.EXAMPLE
  ./scripts/release.ps1 -Bump patch            # 0.1.0 -> 0.1.1
.EXAMPLE
  ./scripts/release.ps1 -Bump minor -DryRun    # show what would happen, change nothing
.EXAMPLE
  ./scripts/release.ps1 -Version 1.0.0 -Image myname/rustdesk-manager
#>
[CmdletBinding()]
param(
    # Which part of the version to raise. Use this or -Version.
    [ValidateSet('patch', 'minor', 'major')]
    [string]$Bump,

    # An exact new version, e.g. 1.0.0.
    [string]$Version,

    # Docker Hub repository (user/name). Can also be set with the RDM_IMAGE environment variable.
    [string]$Image = $(if ($env:RDM_IMAGE) { $env:RDM_IMAGE } else { 'burnacid/rustdesk-manager' }),

    # The branch you work on; it is merged into main.
    [string]$Branch = 'develop',

    # The git remote to push to.
    [string]$Remote = 'origin',

    # Show every step without changing files, git or the registry.
    [switch]$DryRun,

    # Skip go vet, go test and the web build (not recommended).
    [switch]$SkipTests,

    # Don't ask for confirmation (for automation).
    [switch]$Yes,

    # Don't create a GitHub release (only push the tag).
    [switch]$NoGitHubRelease,

    # Mark the GitHub release as a pre-release.
    [switch]$Prerelease
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$Main = 'main'

# --- helpers -----------------------------------------------------------------

function Step([string]$Message) { Write-Host "`n==> $Message" -ForegroundColor Cyan }
function Note([string]$Message) { Write-Host "    $Message" -ForegroundColor DarkGray }

# Hints printed when a later step fails, so a half-finished release can be completed by hand.
$script:Recovery = @()

function Fail([string]$Message) {
    Write-Host "`nERROR: $Message" -ForegroundColor Red
    if ($script:Recovery.Count -gt 0) {
        Write-Host "`nThe release was committed and tagged locally but not fully published. After fixing the problem, finish with:" -ForegroundColor Yellow
        $script:Recovery | ForEach-Object { Write-Host "    $_" -ForegroundColor Yellow }
    }
    exit 1
}

# Run a command; mutating commands are only shown in a dry run.
function Run {
    param([Parameter(Mandatory)][string]$Command, [string[]]$Arguments = @(), [switch]$Mutating, [string]$Hint)
    $line = (@($Command) + $Arguments) -join ' '
    if ($DryRun -and $Mutating) { Note "(dry run) $line"; return }
    Note $line
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        $msg = "'$line' failed (exit code $LASTEXITCODE)."
        if ($Hint) { $msg += " $Hint" }
        Fail $msg
    }
}

# Run a read-only command and return its trimmed output, or $null if it fails.
function Query([string]$Command, [string[]]$Arguments) {
    $out = & $Command @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) { return $null }
    return (($out | Out-String).Trim())
}

function Confirm([string]$Question) {
    if ($Yes -or $DryRun) { return $true }
    $answer = Read-Host "$Question [y/N]"
    return $answer -match '^(y|yes|j|ja)$'
}

function RefExists([string]$Ref) { return $null -ne (Query git @('rev-parse', '--verify', '--quiet', $Ref)) }

# --- 0. preconditions ----------------------------------------------------------

Set-Location (Split-Path $PSScriptRoot -Parent)
if ($DryRun) { Write-Host 'DRY RUN: nothing will be changed.' -ForegroundColor Yellow }

foreach ($tool in 'git', 'docker', 'go', 'npm') {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { Fail "'$tool' was not found on PATH." }
}
if (-not $NoGitHubRelease) {
    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
        Fail "The GitHub CLI 'gh' is needed for the GitHub release (https://cli.github.com), or use -NoGitHubRelease."
    }
    & gh auth status *> $null
    if ($LASTEXITCODE -ne 0) { Fail "The GitHub CLI isn't logged in. Run 'gh auth login' once, or use -NoGitHubRelease." }
}
if (-not $Bump -and -not $Version) { Fail 'Say which version to release: -Bump patch|minor|major, or -Version X.Y.Z.' }
if ($Bump -and $Version) { Fail 'Use either -Bump or -Version, not both.' }
if ($Image -notmatch '^[a-z0-9][a-z0-9._:/-]*[a-z0-9]$') { Fail "'$Image' is not a valid image name (use lower case, e.g. myname/rustdesk-manager)." }

Step 'Checking git'
$dirty = Query git @('status', '--porcelain')
if ($dirty) { Fail "There are uncommitted changes. Commit or stash them first:`n$dirty" }
$current = Query git @('rev-parse', '--abbrev-ref', 'HEAD')
if ($current -ne $Branch) { Fail "Run this on the '$Branch' branch (you're on '$current'). Switch with: git switch $Branch" }
if (-not (RefExists $Main)) { Fail "There is no local '$Main' branch." }

Run git @('fetch', $Remote, '--tags', '--prune') -Hint "Check your network and access to '$Remote'."
$remoteBranch = "$Remote/$Branch"
$remoteMain = "$Remote/$Main"
if (RefExists $remoteBranch) {
    $behind = [int](Query git @('rev-list', '--count', "HEAD..$remoteBranch"))
    if ($behind -gt 0) { Fail "'$Branch' is $behind commit(s) behind $remoteBranch. Pull first: git pull --ff-only" }
}
if (RefExists $remoteMain) {
    # main may be behind its remote (we fast-forward it), but must not have diverged.
    $mainAhead = [int](Query git @('rev-list', '--count', "$remoteMain..$Main"))
    $mainBehind = [int](Query git @('rev-list', '--count', "$Main..$remoteMain"))
    if ($mainAhead -gt 0 -and $mainBehind -gt 0) { Fail "'$Main' and $remoteMain have diverged. Sort that out first." }
}

# --- 1. version -------------------------------------------------------------------

Step 'Working out the version'
$old = (Get-Content VERSION -Raw).Trim()
if ($old -notmatch '^(\d+)\.(\d+)\.(\d+)$') { Fail "VERSION contains '$old', not a version like 1.2.3." }
$maj, $min, $pat = [int]$Matches[1], [int]$Matches[2], [int]$Matches[3]
if ($Bump) {
    switch ($Bump) {
        'major' { $new = "$($maj + 1).0.0" }
        'minor' { $new = "$maj.$($min + 1).0" }
        'patch' { $new = "$maj.$min.$($pat + 1)" }
    }
} else {
    $new = $Version.Trim().TrimStart('v')
    if ($new -notmatch '^\d+\.\d+\.\d+$') { Fail "'$Version' is not a version like 1.2.3." }
    if ([version]$new -le [version]$old) { Fail "$new is not newer than the current version $old." }
}
$tag = "v$new"
if (RefExists "refs/tags/$tag") { Fail "Tag $tag already exists." }
Note "$old -> $new"

# --- 2. changelog ------------------------------------------------------------------

Step 'Preparing the changelog'
$changelog = Get-Content CHANGELOG.md -Raw
$unreleased = [regex]::Match($changelog, '(?ms)^## \[Unreleased\][^\n]*\n(.*?)(?=^## \[|^\[[^\]]+\]:|\z)')
if (-not $unreleased.Success) { Fail "CHANGELOG.md has no '## [Unreleased]' section." }
$notes = $unreleased.Groups[1].Value.Trim()

if (-not $notes) {
    $prevTag = "v$old"
    $range = if (RefExists "refs/tags/$prevTag") { "$prevTag..HEAD" } else { 'HEAD' }
    $commits = @(& git log $range --no-merges --format='- %s' | Where-Object { $_ -notmatch '^- Release \d' })
    if ($commits.Count -eq 0) { Fail "Nothing to release: the Unreleased section is empty and there are no commits since $prevTag." }
    Write-Host "`nThe Unreleased section is empty. Commits since ${prevTag}:" -ForegroundColor Yellow
    $commits | ForEach-Object { Write-Host "    $_" }
    if (-not (Confirm 'Use these as the release notes?')) {
        Fail 'Stopped. Describe the changes under "## [Unreleased]" in CHANGELOG.md, commit, and run this again.'
    }
    $notes = "### Changed`n" + ($commits -join "`n")
}

$date = Get-Date -Format 'yyyy-MM-dd'
$section = "## [Unreleased]`n`n## [$new] - $date`n`n$notes`n`n"
$changelog = $changelog.Substring(0, $unreleased.Index) + $section + $changelog.Substring($unreleased.Index + $unreleased.Length)

# Rebuild the version links at the bottom as compare links on the repository's site.
$remoteUrl = Query git @('remote', 'get-url', $Remote)
$repoUrl = $remoteUrl -replace '^git@([^:]+):', 'https://$1/' -replace '\.git$', ''
$changelog = [regex]::Replace($changelog, '(?m)^\[[^\]]+\]:.*(\r?\n)?', '').TrimEnd()
$versions = @([regex]::Matches($changelog, '(?m)^## \[(\d+\.\d+\.\d+)\]') | ForEach-Object { $_.Groups[1].Value })
$links = @("[Unreleased]: $repoUrl/compare/v$($versions[0])...HEAD")
for ($i = 0; $i -lt $versions.Count; $i++) {
    if ($i + 1 -lt $versions.Count) { $links += "[$($versions[$i])]: $repoUrl/compare/v$($versions[$i + 1])...v$($versions[$i])" }
    else { $links += "[$($versions[$i])]: $repoUrl/releases/tag/v$($versions[$i])" }
}
$changelog = $changelog + "`n`n" + ($links -join "`n") + "`n"

Write-Host "`nRelease notes for ${new}:" -ForegroundColor Green
$notes -split "`n" | ForEach-Object { Write-Host "    $_" }

# --- 3. tests (before any file changes, so a failure leaves everything as it was) --

if ($SkipTests) {
    Step 'Skipping tests (-SkipTests)'
} else {
    Step 'Running the web build and tests'
    # The Go binary embeds web/dist, so build the web console first.
    Push-Location web
    try {
        if (-not (Test-Path node_modules)) { Run npm @('ci') }
        Run npm @('run', 'build')
    } finally { Pop-Location }
    Run go @('vet', './...')
    Run go @('test', './...') -Hint 'Fix the failing tests before releasing.'
}

# --- 4. update files -----------------------------------------------------------

Step 'Updating VERSION, CHANGELOG.md and web/package.json'
if ($DryRun) {
    Note '(dry run) files not written'
} else {
    # LF line endings and no BOM, as in the repository.
    [IO.File]::WriteAllText((Join-Path $PWD 'VERSION'), "$new`n")
    [IO.File]::WriteAllText((Join-Path $PWD 'CHANGELOG.md'), ($changelog -replace "`r`n", "`n"))
    Push-Location web
    try { Run npm @('version', $new, '--no-git-tag-version', '--allow-same-version') } finally { Pop-Location }
}

# --- 5. confirm --------------------------------------------------------------------

Write-Host "`nReady to release ${new}:" -ForegroundColor Green
Note "commit 'Release $new' on $Branch, merge $Branch into $Main, tag $tag"
Note "build and push ${Image}:$new and ${Image}:latest (linux/amd64)"
Note "push $Main, $Branch and $tag to $Remote ($repoUrl)"
if (-not $NoGitHubRelease) { Note "create the GitHub release $tag with the notes above and the image file" }
if (-not (Confirm 'Go ahead?')) {
    if (-not $DryRun) {
        & git checkout -- VERSION CHANGELOG.md web/package.json web/package-lock.json 2>$null
    }
    Fail 'Stopped; the file changes were undone.'
}

# --- 6. commit, merge, tag ----------------------------------------------------------

Step "Committing on $Branch"
Run git @('add', 'VERSION', 'CHANGELOG.md', 'web/package.json', 'web/package-lock.json') -Mutating
Run git @('commit', '-m', "Release $new") -Mutating

Step "Merging $Branch into $Main and tagging $tag"
Run git @('switch', $Main) -Mutating
if (RefExists $remoteMain) { Run git @('merge', '--ff-only', $remoteMain) -Mutating -Hint "Local $Main can't be fast-forwarded to $remoteMain." }
Run git @('merge', '--no-ff', $Branch, '-m', "Release $new") -Mutating -Hint 'Resolve the merge conflict, commit, then tag and push by hand.'
Run git @('tag', '-a', $tag, '-m', "RustDesk Manager $new") -Mutating
# Bring develop up to the merge commit so both branches agree.
Run git @('switch', $Branch) -Mutating
Run git @('merge', '--ff-only', $Main) -Mutating

$asset = "release/rustdesk-manager-$new-linux-amd64.tar.gz"
$notesFile = Join-Path ([IO.Path]::GetTempPath()) "rustdesk-manager-$new-notes.md"
if (-not $DryRun) { [IO.File]::WriteAllText($notesFile, $notes + "`n`nDocker image: ``docker pull ${Image}:$new```n") }
$releaseCommand = "gh release create $tag --title `"RustDesk Manager $new`" --notes-file `"$notesFile`" --verify-tag $asset"

$script:Recovery = @(
    "docker build --platform linux/amd64 --build-arg VERSION=$new -t ${Image}:$new -t ${Image}:latest .",
    "docker push ${Image}:$new",
    "docker push ${Image}:latest",
    "git push --atomic $Remote $Main $Branch refs/tags/$tag"
)
if (-not $NoGitHubRelease) { $script:Recovery += $releaseCommand }

# --- 7. Docker image ------------------------------------------------------------

Step "Building ${Image}:$new"
Run docker @('build', '--platform', 'linux/amd64', '--build-arg', "VERSION=$new", '-t', "${Image}:$new", '-t', "${Image}:latest", '.') -Mutating -Hint 'Is Docker Desktop running?'

Step 'Pushing the image'
Run docker @('push', "${Image}:$new") -Mutating -Hint "Log in first with 'docker login', and check that '$Image' is your repository."
Run docker @('push', "${Image}:latest") -Mutating

if (-not $NoGitHubRelease) {
    Step "Saving the image to $asset (attached to the GitHub release)"
    if ($DryRun) {
        Note "(dry run) docker save ${Image}:$new | gzip > $asset"
    } else {
        New-Item -ItemType Directory -Force release | Out-Null
        $tar = "release/rustdesk-manager-$new-linux-amd64.tar"
        Run docker @('save', '-o', $tar, "${Image}:$new", "${Image}:latest")
        $in = [IO.File]::OpenRead((Resolve-Path $tar))
        $out = [IO.File]::Create((Join-Path $PWD $asset))
        $gz = [IO.Compression.GZipStream]::new($out, [IO.Compression.CompressionLevel]::Optimal)
        try { $in.CopyTo($gz) } finally { $gz.Dispose(); $out.Dispose(); $in.Dispose() }
        Remove-Item $tar
        Note "$asset ($([math]::Round((Get-Item $asset).Length / 1MB, 1)) MB)"
    }
}

# --- 8. push git ----------------------------------------------------------------

Step "Pushing $Main, $Branch and $tag to $Remote"
Run git @('push', '--atomic', $Remote, $Main, $Branch, "refs/tags/$tag") -Mutating -Hint 'Check your access to the remote.'

if (-not $NoGitHubRelease) {
    $script:Recovery = @($releaseCommand)
    Step "Creating the GitHub release $tag"
    $ghArgs = @('release', 'create', $tag, '--title', "RustDesk Manager $new", '--notes-file', $notesFile, '--verify-tag', $asset)
    if ($Prerelease) { $ghArgs += '--prerelease' }
    Run gh $ghArgs -Mutating -Hint "Check 'gh auth status'."
    if (-not $DryRun) { Remove-Item $notesFile -ErrorAction SilentlyContinue }
}
$script:Recovery = @()

Write-Host "`nReleased $new." -ForegroundColor Green
Note "Image:  docker pull ${Image}:$new"
Note "Release: $repoUrl/releases/tag/$tag"
if ($DryRun) { Write-Host "`nDRY RUN finished: nothing was changed." -ForegroundColor Yellow }
