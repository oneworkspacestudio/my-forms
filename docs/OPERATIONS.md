# Operations, storage and recovery

Workspace launches MyForms.exe with --data, --listen, --origin and --no-browser.
APPHOST identity environment is provided by the existing shared foundation.
MYFORMS_DRIVE_URL and MYFORMS_DRIVE_TOKEN select the capability-protected loopback
Workspace Drive adapter. Never put these secrets in a form or shared folder.

`/healthz` reports process health; `/readyz` remains unavailable until all Drive
records have passed startup reconciliation. `--reindex` performs that integrity
check and exits. An owner can POST `/api/v1/reindex` with their CSRF token.

The dedicated private My Forms source is accessed through My Drive's native API.
Owners receive scoped read access in Drive. Collaborators use separately enforced
My Forms grants; sharing a form does not share its raw response folder.

Layout: `<owner-hash>/<form-id>/events/<revision>.json`,
`responses/<response-id>-<version>.json`, `uploads/<file-id>.json`, and
`exports/<export-id>.<csv|xlsx|json>`. JSON records are schema-1 SHA256 envelopes.
Uploaded file bytes are stored in those envelopes. Writes are immutable and are
read back before success is returned. A retry uses the same submission ID.
Editor conflicts return HTTP409 and require reloading/merging; never overwrite a
newer editor's revision. Indexes are rebuilt in memory after restart.

Use the normal One Workspace update/backup workflow. It includes `forms` session
state and the authoritative `forms-drive` source. Stop the managed service before
restoring a verified Workspace backup, restore matching programs/config/private
data, restart, and require `/readyz` plus form/response counts to pass. Retain the
latest verified rollback. Removing the app must not silently delete Drive data.

Keep public URLs stable by retaining the Workspace origin and form IDs.
Response edit links expire after seven days. Do not put private receipt links in
logs or public issue reports. Export before applying retention policies outside
this preview; automatic retention and permanent erasure are not implemented.
