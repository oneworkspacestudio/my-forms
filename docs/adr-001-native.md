# ADR 001: native Go and embedded web UI

Status: selected; real integration prototype acceptance remains a release gate.

Reviewed 2026-10-28: SurveyKing main 5de0d05 (MIT LICENSE), last push 2026-08-26;
OpnForm main 24193de (AGPLv3 core plus proprietary Enterprise directory), last push
2026-10-28. Full license/dependency evidence is retained in build discovery.
Sources: https://github.com/javahuang/SurveyKing and https://github.com/OpnForm/OpnForm.

SurveyKing advertises Java/Spring Boot 2.7, React/Ant Design and H2/MySQL. Its
published Windows distribution is not evidence of a native single executable
with OneAccount and Drive authority. OpnForm uses Laravel/PHP and Nuxt; its
Enterprise license does not permit unrestricted production redistribution.
Neither supplies the required Drive storage contract. No code from either is
copied. Their feature lists are useful comparison references, not acceptance.

Go with the existing Apache-2.0 Workspace apphost avoids a bundled language
runtime, reuses verified OIDC/session policy and compiles the UI into one Windows
executable. An immutable Drive object journal is authoritative; memory/SQLite
indexes can be discarded. This requires implementing the form engine ourselves.
The cost is substantial coverage/testing work, recorded in feature-parity.md;
we do not claim parity merely by selecting a native architecture.

Security posture must be established by pinned dependency scanning and actual
negative integration tests. Upstream repository activity alone proves neither
security nor Windows packaging. Clean VM/reboot tests are never inferred from
running a binary on the development host.
