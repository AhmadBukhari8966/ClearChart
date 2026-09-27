# Progress — 2026-09-26

## Current task
Judge mode (2026-09-26). Builds and vets; temporary unit checks passed and were removed. NOT yet run against Supabase or clicked through in a browser.

## Cleanup (latest, 2026-09-27)
- Deleted `docs/` (stale: memory store, demo login, body map, removed methods), `invitation_check_test.go` (temporary; passed before removal), local `clearchart.exe`. README rewritten as the single current reference (config, migrations 001–004, care-team linking SQL, limits, Fly deploy).
- Removed dead CSS for the old Apple Watch/biometrics card, `mock-tag`, `notif-review-link` (34 rules, 4 shared selector lists trimmed; ~2.5 KB). staticcheck U1000 found no unused Go code.
- Verified vet/build/test. Kept: `.tools/` (1.2 GB local test PostgreSQL, git-ignored), seeds, `.vscode/launch.json`, Fly GitHub workflow (needs `FLY_API_TOKEN` repo secret or it fails on push).

## Fly.io deploy (2026-09-27)
- `Dockerfile` (distroless, HOST=0.0.0.0, PORT=8080), `.dockerignore` (excludes .env, .tools, tests, seeds), `fly.toml` (app `clearchart-proud-shell-6676`, region sjc near Supabase us-west-2, 1 always-on machine). Secrets on Fly: DATABASE_URL, NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY.
- `deploy.env` (committed, non-secret) is copied to `/app/.env` in the image; currently `JUDGE_MODE=true`. Fly secrets/env override it. Never put secrets there.
- Linux cross-compile verified; Docker image build not verified locally (no Docker).

## Visual polish (2026-09-26)
- CSS-only block at the end of `static/styles.css` ("Visual polish"): softer panel shadows/radii, stronger section titles, serif stat numerals with doctor stats as separate cards, subtle header gradient, sticky topbar (solid bg, no backdrop-filter), hover lift on patient/attention/report cards, selected-patient accent, gradient buttons with press state, input focus rings and focus-visible outlines, rounded graded healing bars, timeline entry hover, thin scrollbars, one 0.35 s load entrance on page-level containers only (SSE patches never re-trigger it; reduced-motion honored).
- No JS, images, fonts or templates changed. Screenshots checked at 1440px and 920px.

## Layout balance (2026-09-26)
- Dashboards use golden-ratio columns (`1.618fr / 1fr`, ≥821px) instead of a fixed narrow side column.
- Doctor: "Invite a patient" moved into the header beside "Add a care update"; Patient reports center and profile card moved to the right column (Needs attention, Healing status, Reports, Profile, quote); doctor timeline scrolls inside its panel (max 640px).
- Patient: plain-language card and Your documents moved to the right column (Healing, Clarity, Documents, Care team, privacy note); timeline has the left column to itself.
- Side-column tweaks: heading icons stay on the title row, report "Patient upload" badges hidden, compact dropzone.
- Verified with headless-Edge screenshots of real data at 1440px and 920px (judge pane width); temporary render test removed.

## Toast fix
- Floating `#form-feedback` toast never hid. Now fades via CSS after 4 s (errors 8 s, paused on hover). All `#form-feedback` patches use Datastar `replace` mode (confirmed in v1.0.3 bundle) so each message restarts the timer. vet/build pass; not browser-tested.

## Doctor fixes (2026-09-26)
- **Needs migration 004** (`migrations/004_notifications_seen.sql`, also appended to schema.sql): adds `profiles.notifications_seen_at`. Server refuses to start until applied.
- Doctor notification badge resets: opening the panel calls `MarkNotificationsSeen`; count = responses since `max(now-30d, seen_at)`. Patient badge unchanged (pending invites clear when answered).
- Body-area checkbox pills: override global `input{width:100%}` so checkboxes are 13px and labels don't wrap.
- Doctor Apple Watch card and biometric query removed (mock_biometric_data table untouched); judge no longer seeds it. Replaced by live "Needs attention" panel (top of right column, sidebar link with count): linked patients whose latest check-in is pain ≥6 or mobility/energy ≤4, 6 most severe shown, click selects patient. Thresholds chosen because seed data never exceeds pain 6 / below mobility 4 (84 of 307 flagged on current data).
- Plain-language ("AI") card removed from the doctor side; patient-only now. Judge tour steps 4 and 6 updated.
- Verified: vet/build, template render checks, read-only run of the new dashboard SQL against Supabase (~0.3 s, 307 patients). Not browser-tested.

## Judge mode (previous change)
- `JUDGE_MODE=true` (added to `.env`): at startup `JudgeWorkspace()` (judge.go) upserts doctor profile "Dr. Judge Demo" (`judge@clearchart.demo`, no auth_user_id), links it to every patient in `care_team` (ON CONFLICT DO NOTHING; additive, no migration), adds one mock biometric row, and caches linked patient ID→email in `app.judgePatients`. Rerun on each judge entry to pick up new signups.
- Sign-in page shows "Enter judge mode" (POST /judge, auth CSRF cookie) only when enabled. Issues a 4 h opaque session with `Judge: true` as the judge doctor. Normal Supabase login unchanged.
- `sessionFor(r, "patient")` for judge sessions acts as the patient named by the `{id}` path value or an already-parsed `patient_id` form field, only if linked to the judge doctor (fail closed). Stateless, so several patient tabs work at once.
- GET /judge: split view (templates/judge.html) with doctor and patient dashboards in iframes; a small judge-only inline script makes the patient pane follow the doctor's selected patient (exception to the no-custom-JS rule, limited to this page). Always side by side, filling the viewport (stacks under 900px). An 8-step guided tour bar (Back/Next, step dots, remembered in localStorage) highlights which pane to use and scrolls to and outlines the relevant section inside each iframe (outline reapplied after SSE patches). Hide/show guide, open-in-new-tab links, exit button.
- `head` template links `styles.css?v=<content hash>` (`cssVersion` func in newApp) so new CSS is never masked by the 1 h static cache.
- `X-Frame-Options` is SAMEORIGIN when judge mode is on (DENY otherwise). Top-level `/` and `/login` send judge sessions to /judge (`landing`, uses `Sec-Fetch-Dest`). Sidebar logout targets `_top`.
- Known gap: the notification bell inside the patient pane shows the judge doctor's notifications (notificationsPanel uses currentSession role).

## Performance (previous change)
Performance pass (2026-09-26). Verified locally; no schema change, no migration needed. Previous task (timeline category filters) still awaits migration 003 on Supabase — see Next steps.

- `store.go` `Dashboard()`: one SQL statement (CTEs + `json_build_object`/`json_agg`) replaces the 8–10 sequential queries in `dashboardData` (profile, patients, care-team check, biometrics, reports, invitation count, patient, doctors, records, uploads, healing). Selected-patient resolution and care-team authorization happen in SQL; requested-but-unlinked or malformed patient IDs → `ErrForbidden`. Patient directory order now `name,id` (deterministic ties).
- lib/pq `binary_parameters=yes` added to the DSN automatically (`singleRoundTripDSN`): 1 network round trip per parameterized query instead of 2. Pool keeps all 10 connections idle-ready.
- Handlers: `addRecord` drops the redundant `IsCareTeam` pre-check (`AddRecord` already authorizes in its INSERT) and reuses dashboard counts instead of re-querying all records/uploads; `uploadReport` uses one `Counts()` query; `addHealing` loads only `Healing()` instead of a full dashboard; notifications panel no longer loads an unused profile; `RespondInvitation` merges lock + eligibility into one query and returns the doctor ID (removed `InvitationByID` and the follow-up invitation lookup); invitation/review pages and startup schema checks load in parallel; patient notification after invite runs after the response.
- Removed now-unused store methods: `Patients`, `CareTeam`, `Uploads`, `Reports`, `Biometrics`, `InvitationByID`; `Dashboard.PendingInvitations/InvitationActivity` (only the count was used).
- HTTP (`perf.go`): gzip (BestSpeed, pooled) for HTML/CSS/SVG/JS; SSE streams excluded. Static assets get content ETags + `max-age=3600` (304 revalidation). Mock scans cacheable `private, max-age=3600`. SSE `patch` no longer flushes per fragment (one flush per batch). Label map and plain-language replacer built once.

## Performance verification
- `go vet ./...`, `go build`, `go test ./...` pass.
- Temporary test (isolated PostgreSQL 16 on 127.0.0.1:55432 via Homebrew, schema + seed + seed_large; DB dropped, cluster and test file deleted): new `Dashboard()` output identical to the old sequential implementation for 707 dashboards (every doctor × linked patient, unlinked patient, every patient, with categories/biometrics/invitation counts); HTTP checks for gzip page, static ETag/304, malformed/unlinked patient 403, add-record (linked/unlinked/malformed), healing panel, notifications, scans (patient/doctor/unlinked/unknown), SSE initial snapshot + live update, SSE not compressed. `invitation_check_test.go` passed too.
- Through a 40 ms-RTT proxy: doctor dashboard 1077 ms → 76 ms, patient 623 ms → 46 ms. Doctor page 164 KB → 20 KB gzip.
- NOT done: real-browser check against Supabase.

## Timeline filters (previous change)
- `categories.go`: 14 categories reused from the former body map (brain, heart, lungs, liver, stomach, kidneys, spine, shoulders, knees, ankles, hips, blood, nervous-system, muscles); `timelineFilter{Type, Category}` combines both rows. No keyword guessing.
- `migrations/003_record_categories.sql` (+ appended to `schema.sql`): additive `medical_record_categories(record_id, category)` join table, CHECK on known IDs, cascade on record delete, RLS on. Existing records untouched → uncategorized, visible under "All categories".
- `store.go`: `Records` returns `Categories`; `AddRecord` inserts record + categories in one transaction (care-team authorization unchanged).
- Doctor record form: optional body-area checkboxes; server validates via `normalizeCategories`.
- Filtering is server-side: Datastar signals `timelinetype`/`timelinecat` reopen the existing `/events` SSE stream (both roles), which keeps the filter for later live snapshots. `?type=&category=` still works for initial page loads.
- Doctor: `chartpatient` signal means filter changes do not replace `#new-record` (drafts kept); switching patient still does. `addRecord` re-renders the timeline using the doctor's active filters (hidden `filter_type`/`filter_category` bound to signals).
- Shared templates `timeline-filters` (two rows, selected state, "Showing X of Y", pills, Clear filters) and `timeline-items` (filtered empty state). Record cards show category chips. Styles appended to `static/styles.css`.
- Removed: `bodyhealth.go`, `bodymap.go`, `bodymap_test.go`, `templates/body_map.html`, `static/body-map.css`, `/dashboard/{role}/{id}/body-map` route, sidebar link, `Page`/`BodyHealth` fields, `bodyPercent` func, `icon-body`.

## Verification done
- `go vet ./...`, `go build`, `go test ./...` pass; binary rebuilt.
- Temporary integration test (isolated local PostgreSQL on 127.0.0.1:55432, created/dropped DB; file since deleted) passed: migration 003 on pre-change schema preserves existing record and is re-runnable; CHECK rejects unknown category; combined filters (Imaging+Brain, All+Blood, Imaging+All, All+All) on page and both SSE streams; empty state; live update keeps patient filters and excludes non-matching new records; doctor add response respects doctor filters; filter change does not resend doctor form, patient switch does; unlinked doctor stream 403, invalid category 400, foreign dashboard 403; add-record rejects bad CSRF, cross-origin, unknown category, unlinked patient, wrong role—no rows inserted. Mutation check confirmed the test detects broken category filtering.
- Local test cluster stopped afterward.
- NOT done: real-browser check (needs Supabase login). Hidden filter inputs rely on Datastar `data-attr:value`; confirm in browser.

## Next steps
1. Apply `migrations/003_record_categories.sql` in Supabase SQL editor (additive; also confirm 002 is applied — still unverified).
2. Restart server; browser-check filter rows, Clear filters, category tagging on new record, draft preservation when changing filters, patient live update.
3. Invitations: local integration test passed (CSRF, consent, replay, decline/revoke/expiry) before it was deleted. Browser check still pending.

## File map
`main.go` startup/routes/schema checks; `auth.go` sessions; `supabase_auth.go` provider; `models.go` contracts; `categories.go` record categories/filters; `store.go` SQL; `handlers.go` dashboards/mutations/scans; `live.go` SSE + timeline patches; `perf.go` gzip/static caching/parallel helper; `invitations.go`; `notifications.go`. Templates/static embedded.

## Operational notes
- `.env` private; never echo values. Sessions/event hub process-local; restart requires login.
- No SMTP, password recovery, real file storage, credential verification, or care-access removal UI. Filename-only uploads, synthetic scans, rule-based summaries remain intentional limitations.
