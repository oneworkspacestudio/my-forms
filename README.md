# My Forms

Create forms and quizzes, share a respondent link, and keep responses in My Drive.
Sign in with your existing OneAccount through One Workspace.

This first preview includes a template gallery, question editor, sections and
branching, quizzes, publication controls, response charts, collaborator permissions,
file uploads scanned by Windows Defender, and CSV, XLSX and JSON exports.

## Install

Use One Workspace with My Accounts, My Drive and the My Forms integration.
The Windows package contains one native `MyForms.exe`; no Node, Java, Python,
Docker or separate database installation is needed. The managed installation
provides identity, protected local session storage and the scoped My Drive API.

The first preview requires the Workspace 98 integration. Its source is available
in [One Workspace](https://github.com/oneworkspacestudio/one-workspace).
Do not run the executable without configuring its managed integrations.

See [acceptance and limitations](docs/feature-parity.md),
[operations and recovery](docs/OPERATIONS.md), [API](docs/openapi.json), and
[architecture](docs/adr-001-native.md). This is an evaluation preview, not a claim
of complete Google Forms parity. Binaries are not Authenticode signed.

## Build

Install Go 1.27.3 and run `scripts/build.ps1` from PowerShell. Run `go test ./...`
and `go vet ./...` before packaging. The frontend is embedded directly and needs
no package manager. GitHub Actions is disabled; releases are built locally.
