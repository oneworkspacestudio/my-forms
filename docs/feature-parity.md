# Preview 1 acceptance and remaining scope

This is a functional evaluation preview, not completion of every item in the
development brief. The user requested installation on the existing Windows PC
instead of a clean Windows VM. Browser UI verification was blocked by the native
Computer Use URL-identification guard; no visual or accessibility pass is claimed.

| Area | Preview status |
|---|---|
| Native single EXE, embedded UI | Built on Windows with CGO disabled; no external application runtime |
| Real OneAccount and Drive | Native isolated fixture: OIDC, create, publish, submit, read and export pass |
| Durable restart | Immutable SHA256 records, startup reindex, response recovery checked |
| Builder | Gallery/templates, reorder, bulk duplicate, copy/paste, undo/redo, autosave/conflicts |
| Questions | All 16 defined types; server validation and branch/quiz tests |
| Responses | Summary/question/individual, search, pagination, grading, CSV/XLSX/JSON |
| Security | IDOR/CSRF/anonymous isolation, uncertain-save retry and formula-injection tests pass |
| Performance | Automated in-memory adapter test: 100 forms, 1,000 concurrent writes and rebuild; not a native Drive benchmark |
| Collaboration | OneAccount subject grants, independent editor/response permissions; no invitation inbox, groups or owner transfer yet |
| Organisation | Logical folder labels and archive/restore; no physical Drive move UI |
| Retention | Response tombstones retain history; no automatic retention/permanent erasure yet |
| Media | PNG/JPEG uploads; safe external video links, not embedded playback |
| Sheets | CSV/XLSX export; no live Sheets connector found or claimed |
| Accessibility and visual parity | Semantic labels, keyboard reorder/focus and responsive CSS implemented; browser/axe/screenshot gates remain open |
| Clean host/reboot/restore | Not performed; host-native isolated testing and restart checks only |

Do not use this preview as a replacement for a regulated retention or erasure
system. Do not assume full Google Forms parity. No production user data was used
as a test fixture. GitHub Actions is disabled. Windows binaries are unsigned.
