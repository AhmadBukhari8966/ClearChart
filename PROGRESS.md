# Progress — 2026-09-26

## Current task
Body-map categories merged into the patient timeline as filters; separate body map removed. Code, tests and build verified locally. **Not deployed:** migration 003 must be applied to Supabase before restarting the server (startup fails with a clear message otherwise).

## Timeline filters (this change)
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
`main.go` startup/routes/schema checks; `auth.go` sessions; `supabase_auth.go` provider; `models.go` contracts; `categories.go` record categories/filters; `store.go` SQL; `handlers.go` dashboards/mutations/scans; `live.go` SSE + timeline patches; `invitations.go`; `notifications.go`. Templates/static embedded.

## Operational notes
- `.env` private; never echo values. Sessions/event hub process-local; restart requires login.
- No SMTP, password recovery, real file storage, credential verification, or care-access removal UI. Filename-only uploads, synthetic scans, rule-based summaries remain intentional limitations.
