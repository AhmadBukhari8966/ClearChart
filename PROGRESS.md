# Progress — 2026-09-26

## Current task
Performance pass (2026-09-26). Verified locally; no schema change, no migration needed. Previous task (timeline category filters) still awaits migration 003 on Supabase — see Next steps.

## Performance (this change)
- `store.go` `Dashboard()`: one SQL statement (CTEs + `json_build_object`/`json_agg`) replaces the 8–10 sequential queries in `dashboardData` (profile, patients, care-team check, biometrics, reports, invitation count, patient, doctors, records, uploads, healing). Selected-patient resolution and care-team authorization happen in SQL; requested-but-unlinked or malformed patient IDs → `ErrForbidden`. Patient directory order now `name,id` (deterministic ties).
- lib/pq `binary_parameters=yes` added to the DSN automatically (`singleRoundTripDSN`): 1 network round trip per parameterized query instead of 2. Pool keeps all 10 connections idle-ready.
- Handlers: `addRecord` drops the redundant `IsCareTeam` pre-check (`AddRecord` already authorizes in its INSERT) and reuses dashboard counts instead of re-querying all records/uploads; `uploadReport` uses one `Counts()` query; `addHealing` loads only `Healing()` instead of a full dashboard; notifications panel no longer loads an unused profile; `RespondInvitation` merges lock + eligibility into one query and returns the doctor ID (removed `InvitationByID` and the follow-up invitation lookup); invitation/review pages and startup schema checks load in parallel; patient notification after invite runs after the response.
- Removed now-unused store methods: `Patients`, `CareTeam`, `Uploads`, `Reports`, `Biometrics`, `InvitationByID`; `Dashboard.PendingInvitations/InvitationActivity` (only the count was used).
- HTTP (`perf.go`): gzip (BestSpeed, pooled) for HTML/CSS/SVG/JS; SSE streams excluded. Static assets get content ETags + `max-age=3600` (304 revalidation). Mock scans cacheable `private, max-age=3600`. SSE `patch` no longer flushes per fragment (one flush per batch). Label map and plain-language replacer built once.
- `docs/` still describes removed methods (it was already stale: memoryStore/demo); not updated.

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
3. Invitations: `invitation_check_test.go` passed in this session's `go test ./...` against the local cluster (CSRF, consent, replay, decline/revoke/expiry). Browser check and live 002 confirmation still pending; remove that temporary test when the user agrees.

## File map
`main.go` startup/routes/schema checks; `auth.go` sessions; `supabase_auth.go` provider; `models.go` contracts; `categories.go` record categories/filters; `store.go` SQL; `handlers.go` dashboards/mutations/scans; `live.go` SSE + timeline patches; `perf.go` gzip/static caching/parallel helper; `invitations.go`; `notifications.go`. Templates/static embedded.

## Operational notes
- `.env` private; never echo values. Sessions/event hub process-local; restart requires login.
- No SMTP, password recovery, real file storage, credential verification, or care-access removal UI. Filename-only uploads, synthetic scans, rule-based summaries remain intentional limitations.
