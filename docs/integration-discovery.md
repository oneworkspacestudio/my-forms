# Integration discovery — 28 October 2026

Workspace main was fast-forwarded before implementation to cb29f96. The local
installation reports preview95 at I:/Workspace, with separate programs, private
data and files. That installation is a deployment target, never a test fixture.

## Identity

`one-workspace/pkg/apphost/auth.go` implements OneAccount OIDC authorization code
with PKCE, nonce, state cookie, issuer/audience validation and eight-hour maximum
sessions. `Identity(issuer, subject)` is SHA256 of issuer, NUL and subject.
`Host.Require` enforces session, CSRF, origin and optional account-switch guard.
My Forms requires an issuer; standalone password registration is never enabled.
Platform owner status does not grant access to another user's form or responses.

## Drive

Inspected oneworkspacestudio/drive e887bc9. Native endpoints are under `/drive/api`:
POST resources?source=...&path=...&override=false, GET resources/items, GET raw,
DELETE resources and PATCH users. Source file permissions and physical scopes
are enforced in backend/internal/web/resource.go and database/users.
Content uploads with X-File-Total-Size use a temporary file, size verification,
Sync and move before success. Immutable JSON records avoid mutable-file races.
The Forms adapter must use these endpoints, not write form data on the host.

Existing Workspace social.go demonstrates source provisioning with disabled
default permissions, issuer/subject-derived user scopes and a loopback capability.
Forms needs a separate adapter because public respondents have no Drive session.
Its server credential remains inside Workspace; Forms receives only a capability
restricted to its dedicated source. User browsing is read-only and scoped.
There is no discovered cross-app live Sheets API: CSV/XLSX are exports, not live
spreadsheets. Office history is a file feature, not a response-sheet connector.

## Packaging and operations

Catalogue process contracts provide executable, port, route, health, dependencies,
OIDC callback and environment. Runtime supervises processes; default app addition,
verified backup/rollback and independent releases are existing extension points.
Public routes need explicit gateway admission. Native data uses protected NTFS.
All form objects stay in the managed Drive source, included in suite recovery.
App-local SQLite contains OneAccount session mappings and rebuildable indexes only.
Workspace publication also requires the signed Android companion through its
guarded publisher; app publication runs locally with Actions disabled.
