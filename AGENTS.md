# ClearChart agent instructions

- Active repo: `D:\projects\ClearChart`. Never edit the older `hackkathon` copy.
- Read `PROGRESS.md` first; inspect only files relevant to the task. Avoid rereading chat, entire repo, or historical docs.
- Stack: Go `net/http`, `html/template`, Datastar SSE, Tailwind CDN, Supabase PostgreSQL via `lib/pq`. No custom frontend JavaScript or templ migration.
- PostgreSQL only; no runtime demo login, memory store, or automatic fixture seeding. Preserve existing database rows and scan URLs.
- Auth: Supabase email/password; opaque server sessions. Resolve ownership by `auth_user_id`, never email alone. Check role, care-team membership, CSRF and origin on mutations. Invitation acceptance requires explicit patient consent and matching account email.
- Preserve doctor directory scroll and unsaved drafts: patch chart fragments, not the page shell/directory.
- Keep repo small. User removed legacy tests/fixtures. Do not recreate bulk demo scaffolding; use focused temporary verification when necessary and remove it afterward.
- Never print `.env`, passwords, API keys, tokens, or connection strings. Do not send emails/create external accounts as tests. Use isolated local databases for destructive checks.
- Start with `git status`; preserve user edits. No unsolicited commits. Use additive, reviewed migrations; never reset live data.
- PowerShell: `go run .`; verify `go vet ./...` and `go build`. Inspect tests before running: temporary integration tests may require local PostgreSQL. Rebuild after embedded template edits.
- Keep replies short. Update `PROGRESS.md` with actual changes, checks, blockers and next steps; replace stale status rather than append a diary. Do not label unverified work complete.
