# Security

Report security issues privately to the repository owner through GitHub.
Do not include live form contents, credentials or response links in public issues.

OneAccount is the only author identity provider. Form ownership and grants are
checked on every protected route. Public links expose only published questions;
answer keys are withheld until an owner explicitly enables result release.
The My Drive adapter is loopback-only and capability protected with strict path
validation. User content is escaped; arbitrary HTML and embedded scripts are not
supported. File types are allow-listed and Windows Defender scanning must succeed.

Uploads are limited to 10 MiB each, five per question and 1,000 per form (including
orphans). Respondent writes are limited to 120 requests per form per minute.
Application-local storage contains protected session state, not authoritative
form or response data. Public deployments must use Workspace's HTTPS gateway.

Deletion in this preview is a reversible tombstone; historical Drive revisions
and backups are retained. It is not a permanent personal-data erasure workflow.
See the acceptance ledger before using this preview for sensitive workloads.
