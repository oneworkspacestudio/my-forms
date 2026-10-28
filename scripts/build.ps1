param([string]$Go='go',[string]$Version='0.1.0-preview.1',[string]$Output='', [switch]$Release)
$ErrorActionPreference='Stop'
$root=Split-Path $PSScriptRoot -Parent
if(-not $Output){$Output=Join-Path $root 'dist'}
$Output=[IO.Path]::GetFullPath($Output)
New-Item -ItemType Directory -Force -Path $Output|Out-Null
$env:CGO_ENABLED='0';$env:GOOS='windows';$env:GOARCH='amd64';$env:GOWORK='off'
Push-Location $root
try {
 $source=(& git rev-parse HEAD).Trim()
 if($Release -and (& git status --porcelain)){throw 'Release builds require clean committed source'}
 & $Go test ./...;if($LASTEXITCODE){throw 'Tests failed'}
 & $Go vet ./...;if($LASTEXITCODE){throw 'Vet failed'}
 & $Go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $Output 'MyForms.exe') ./cmd/my-forms
 if($LASTEXITCODE){throw 'Build failed'}
 $stage=Join-Path $Output 'package'
 if(Test-Path -LiteralPath $stage){throw 'Use a fresh output directory'}
 New-Item -ItemType Directory -Path $stage|Out-Null
 Copy-Item -LiteralPath (Join-Path $Output 'MyForms.exe') -Destination $stage
 foreach($name in @('LICENSE','NOTICE','README.md','SECURITY.md','compatibility.json')){Copy-Item -LiteralPath (Join-Path $root $name) -Destination $stage}
 Copy-Item -LiteralPath (Join-Path $root 'docs') -Destination $stage -Recurse
 $notice=[Text.StringBuilder]::new();$components=[Collections.Generic.List[object]]::new()
 $modules=& $Go list -buildvcs=false -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}' ./cmd/my-forms | Where-Object {$_}|Sort-Object -Unique
 if($LASTEXITCODE){throw 'Dependency inventory failed'}
 foreach($line in $modules){$parts=$line.Split('|');if($parts[0] -eq 'github.com/oneworkspacestudio/my-forms'){continue};$components.Add(@{name=$parts[0];version=$parts[1]});$files=@(Get-ChildItem -LiteralPath $parts[2] -File|Where-Object Name -Match '^(LICENSE|LICENCE|COPYING|NOTICE)');if(-not $files.Count){throw ('Missing license: '+$parts[0])};[void]$notice.AppendLine(('===== '+$parts[0]+' '+$parts[1]+' ====='));foreach($file in $files){[void]$notice.AppendLine([IO.File]::ReadAllText($file.FullName))}}
 $goroot=(& $Go env GOROOT).Trim();[void]$notice.AppendLine('===== Go runtime =====').AppendLine([IO.File]::ReadAllText((Join-Path $goroot 'LICENSE')))
 [IO.File]::WriteAllText((Join-Path $stage 'THIRD-PARTY-LICENSES.txt'),$notice.ToString(),[Text.UTF8Encoding]::new($false))
 [IO.File]::WriteAllText((Join-Path $stage 'dependencies.json'),(@{schema=1;components=$components}|ConvertTo-Json -Depth 8),[Text.UTF8Encoding]::new($false))
 [IO.File]::WriteAllText((Join-Path $stage 'build-info.json'),(@{repository='oneworkspacestudio/my-forms';version=$Version;sourceCommit=$source;go=(& $Go version).Trim();architecture='windows-amd64';githubActionsUsed=$false;signing='unsigned preview'}|ConvertTo-Json),[Text.UTF8Encoding]::new($false))
 Add-Type -AssemblyName System.IO.Compression.FileSystem
 $archive=Join-Path $Output ('my-forms-'+$Version+'-windows-amd64.zip')
 [IO.Compression.ZipFile]::CreateFromDirectory($stage,$archive,[IO.Compression.CompressionLevel]::Optimal,$false)
 $sums=foreach($file in @((Join-Path $Output 'MyForms.exe'),$archive)){(Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant()+'  '+[IO.Path]::GetFileName($file)}
 [IO.File]::WriteAllLines((Join-Path $Output 'SHA256SUMS.txt'),$sums,[Text.UTF8Encoding]::new($false))
 Write-Output ('Packaged '+$archive)
} finally {Pop-Location}
