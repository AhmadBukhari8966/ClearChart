# Templates, fixtures, tests, and cleanup

This guide describes the code in **`D:\projects\ClearChart`**, the active project. `D:\projects\hackkathon` is an older, separate copy. Run commands and edit files in ClearChart so that the source being edited is the source being compiled. This inventory was reviewed on September 26, 2026.

## How the frontend is assembled

The application uses Go's `html/template`, not `templ`. `main.go` embeds `templates/*.html` and `static/*` into the executable, and `newApp` parses the templates once. `{{define "name"}}` declares a reusable template; `{{template "name" .}}` renders it with the current data. A file can define multiple templates, and a template's name need not equal its filename. In particular, `mock_scan.html` defines `mock_scan.svg`.

The Go server renders complete pages and smaller HTML fragments. Datastar loads from the pinned `v1.0.3` CDN URL in `head`, evaluates its HTML attributes, sends requests, and applies the server's SSE patches. There is no application JavaScript file or custom `<script>` body. The `.tools/*.mjs` files are browser-test automation; they are not embedded or served to app users.

Changing an HTML or CSS file requires rebuilding/restarting `go run .`. Refreshing a browser cannot replace templates already embedded in a running executable. A stale `.exe` can therefore display an old template parse error even after the source is fixed.

### Template syntax and formatting

Go expressions and HTML attributes have separate quoting rules. This is valid for Go but easy for an ordinary HTML formatter to break:

```html
class="{{if eq .Filter "note"}}selected{{end}}"
```

The patient filter now uses a Go raw-string literal so that the HTML attribute has only one pair of double quotes:

```html
class="{{if eq .Filter `note`}}selected{{end}}"
```

Do not insert a newline inside a double-quoted Go string. A newline **between** arguments, such as between `"%.1f"` and `.Biometric.SleepHours`, is valid. A newline inside `"note"` is not. Some expressions in `shared.html` and `onboarding.html` still use nested double quotes; use a formatter that understands Go templates, or inspect the resulting changes and run the Go tests after formatting. Preserve UTF-8 when saving files.

### Complete named-template inventory

| File / template name | What it renders and why it exists |
| --- | --- |
| `patient_dashboard.html` / `patient_dashboard.html` | Complete patient page: welcome banner, counts, filter links, shared medical timeline, filename upload form/list, care team, healing panel, and rule-based assistance. The page starts the patient's live stream with its current filter. |
| `doctor_dashboard.html` / `doctor_dashboard.html` | Complete doctor page: persistent patient directory, selected-chart fragments, reports for all linked patients, clinician profile, mock watch card, healing, and assistance. The persistent shell owns the selected-patient signal so changing patients does not reload the page or recreate the scrolling directory. |
| `doctor_fragments.html` / `doctor-selected-patient` | `#selected-patient-summary`: selected name, date of birth, blood type, timeline shortcut, and a temporary loading label while the requested chart differs from the rendered chart. |
| `doctor_fragments.html` / `doctor-record-form` | `#new-record`: patient-specific note/prescription/imaging form. Hidden fields carry CSRF, view, doctor, and patient IDs. Form, select, and textarea IDs include the patient ID so an unsaved draft is not carried into another patient's form. The container remains present even when there is no selected patient. |
| `doctor_fragments.html` / `doctor-timeline` | `#health-timeline`: selected-patient heading and `#timeline` records. Replacing the entire section when selection changes updates both the heading and the record list. |
| `onboarding.html` / `onboarding.html` | Complete role-specific registration page. Patients enter birth date and blood type; doctors enter license and specialization. Its SSE result area provides feedback and the new dashboard link. This is demo profile creation, not identity verification. |
| `mock_scan.html` / `mock_scan.svg` | SVG illustration generated for one patient and series number. Includes patient name, deterministic reference/rotation, two alternative cross-section drawings, accessible title/description, and synthetic-image labeling. It is returned as an image, not a dashboard page. |
| `shared.html` / `head` | Shared document metadata, favicon, Google font links, Tailwind CDN, Datastar CDN, and local stylesheet. External CDN/font access is needed for those resources unless they are later bundled locally. |
| `shared.html` / `brand` | Inline SVG brand mark and ClearChart wordmark, reused in sidebar and onboarding. |
| `shared.html` / `icon-grid` | Dashboard/overview icon. |
| `shared.html` / `icon-record` | Document/medical-record icon used throughout cards, lists, and forms. |
| `shared.html` / `icon-people` | Care-team and patient-directory icon. |
| `shared.html` / `icon-heart` | Patient, healing, and biometric icon. |
| `shared.html` / `icon-upload` | Upload/document-sharing icon. |
| `shared.html` / `icon-arrow` | Action and navigation arrow. |
| `shared.html` / `icon-check` | Selection/shared/connected confirmation mark. |
| `shared.html` / `icon-spark` | Assistance and informational brand accent. |
| `shared.html` / `sidebar` | Role-aware navigation, current profile, demo role-switch links, and profile-creation link. Anchors target sections already on the page. |
| `shared.html` / `topbar` | Workspace breadcrumb, live-update label, current date, and profile initials. The label is presentation, not a monitored connection-health indicator. |
| `shared.html` / `record` | One escaped medical-record article with stable `record-{ID}` ID, type marker, timestamp, content, author, and optional scan preview. Imaging uses `ImageURL`, falling back to `/static/ct-scan.svg`. Both full pages and SSE responses reuse this template. |
| `shared.html` / `upload` | One patient's upload filename/date, with stable `upload-{ID}` ID. There is no stored file or download link behind this row. |
| `shared.html` / `report` | One doctor-facing report row, with stable `report-{ID}` ID and patient attribution. The reports center includes all patients linked to the doctor, not only the selected patient. |
| `shared.html` / `healing-panel` | `#healing-panel`: latest scores rendered as CSS bars plus explanatory labels. Lower pain is better; higher mobility/energy is better. Only the patient role receives the check-in form. |
| `shared.html` / `ai-summary` | `#ai-summary`: renders `.Summary`, which the backend supplies using rules/preset text. It does not call an LLM. |

Templates use the `initials`, `date`, `shortDate`, `clock`, `percent`, and `label` functions registered by `newApp` in `main.go`. They format presentation data; they do not fetch from the database. The dashboard data structure supplies profiles, selected patient, records, uploads/reports, healing, biometric values, counts, filters, and form/session identifiers. Keep user content as ordinary strings so `html/template` continues to escape it.

### Datastar controls and DOM contracts

| Attribute or action | Where and how it is used |
| --- | --- |
| `data-init="@get(...)"` | Patient shell opens `/events/patient/{id}` once. `view` identifies the tab; `type` keeps stream updates consistent with the page's record filter. `openWhenHidden: true` permits streaming when the tab is hidden. |
| `data-signals` | Doctor shell initializes `selectedpatient` to the rendered chart and `_savingnote` to false. These signals coordinate the directory and note form. |
| `data-effect` | Doctor shell reads `$selectedpatient` and runs `@get('/events/doctor/{doctorID}?view=...')`. The action URL stays stable while `filterSignals: {include: /^selectedpatient$/}` sends the selection as Datastar signal data. A new selection replaces the prior request rather than creating a full page navigation. The server validates the requested patient again. |
| `data-on:click__prevent` | Patient-directory links prevent normal navigation and change `$selectedpatient`. The expression refuses selection changes while `$_savingnote` is true. A normal `href` remains in the HTML for direct navigation and fallback use. |
| `data-class:chosen` | Keeps the selected directory card's CSS class aligned with `$selectedpatient`. |
| `data-attr:aria-current` | Exposes the selected directory card to assistive technology. The nonselected value is `null`, removing the attribute. |
| `data-show` | Switches the selected-patient summary between viewing/loading labels. Initial inline visibility prevents the loading state flashing before Datastar starts. The directory check mark is instead controlled by CSS through the `chosen` class, preserving its space in the card. |
| `data-on:submit__prevent` | Stops normal form navigation and runs `@post(...)`. Used for `/add-record`, `/upload-report`, `/healing`, and `/onboard/{role}`. The doctor expression additionally requires the selected signal to match the patient ID in that rendered form. |
| `contentType: 'form'` | Tells the Datastar action to submit the form fields. The upload form also has `enctype="multipart/form-data"` so the file input is sent as multipart data. |
| `data-indicator:_savingnote`, `:_savingupload`, `:_savinghealing`, `:_savingprofile` | Tracks an in-flight form request. The matching signal disables the submit button; doctor saving also blocks changing patients. These are temporary UI states, not the source of authorization. |
| `data-attr:disabled` | Applies disabled state to buttons and the doctor fieldset. The doctor form is also disabled while the chosen patient and rendered form differ, preventing submission into a stale chart. |
| `data-ignore-morph` | On `#healing-checkin`, preserves the open details element and in-progress inputs while the surrounding healing panel is refreshed. |

The client/server contract includes `#timeline`, `#uploads`, `#reports`, `#record-count`, `#upload-count`, `#ai-summary`, `#healing-panel`, `#form-feedback`, and `#onboarding-result`. Doctor selection additionally targets `#selected-patient-summary`, `#new-record`, and `#health-timeline`. If renaming an ID, update the relevant selectors in `handlers.go`/`live.go`, templates, CSS, and tests together.

`#patient-directory` must remain the same DOM element during selection. Its bounded scrolling is CSS, but preserving its scroll position depends on excluding it from selection patches. Routine live snapshots refresh data without replacing the note form, which preserves the current patient's draft. Selecting another patient intentionally replaces that form and clears its draft. The patient's timeline filter links still perform ordinary navigation; they are not the doctor's Datastar selection control.

The hidden `csrf` token prevents forged form submissions; `doctor_id` and `patient_id` specify the intended subject but are checked against the session and care team on the server. `view_id` lets the server suppress redundant live updates to the tab that already receives the mutation response. None of these browser fields should replace server authorization.

### Static assets

| File | Purpose and dependencies |
| --- | --- |
| `static/styles.css` | Main visual styling despite Tailwind also being loaded: theme variables, desktop/mobile layouts, cards, timelines, forms, CSS healing bars, focus states, reduced-motion behavior, empty states, fixed feedback notices, bounded directory/reports scrolling, and later readability overrides. Rule order matters because later overrides intentionally replace earlier sizes/colors. This is application source, not generated output. |
| `static/favicon.svg` | Small browser-tab brand icon referenced by `head`. Replace its link before removing it. |
| `static/ct-scan.svg` | Shared illustrative scan used by base fixtures, `seed.sql`, newly submitted demo imaging records, and the `record` template fallback. It is demo content with active runtime callers. |

The patient banner and doctor's weekly heart-rate drawing are inline SVG in the page templates. Healing bars use inline width styles from `percent`. The weekly watch chart is a fixed illustration, not a plot calculated from `Mock_Biometric_Data`; the numeric watch values do come from the dashboard's `.Biometric` field.

## Synthetic fixtures and image handler

### `bulk_seed.go`

`bulkPatientCount` is 300. `addBulkPatients(s *memoryStore, now time.Time)` is the file's only function. `NewMemoryStore` calls it after creating the original two doctors and three patients. It fills the in-memory maps/slices directly during construction, when no requests are running.

For each extra patient, it creates a unique profile and stable ID, links Dr. Sarah Chen, adds Dr. James Wilson for every third patient, and creates five chronological records, one upload filename, and three healing scores. Each chart has two notes, one prescription, and two image URLs. Names, IDs, birth dates, email addresses, scenario choices, and visit references are deterministic; record timestamps are offsets from the supplied `now`, so their absolute dates change with each new store. The resulting demo has 303 patients total and 302 in Sarah's directory.

| Local type/data group | What and why |
| --- | --- |
| `recoveryScenario` | Function-local struct with `area`, `review`, `baseline`, and `medication` strings. Holds six fictional knee/shoulder/ankle/wrist/spine/hip narratives so patient charts look coherent without generating clinical advice. |
| Anonymous record struct (`kind`, `content`, `image`, `hours`) | Table of the five record entries for each patient. Keeps type, text, image reference, and relative time together before building full `Record` values. |
| Anonymous healing struct (`kind`, `value`) | Table of pain, mobility, and energy values before building full `Healing` values. Each remains within 1–10. |

In-memory fixture ID families distinguish profiles (`40000000...`), records (`50000000...`), uploads (`60000000...`), and healing (`70000000...`). `seed_large.sql` matches the fixture content but deliberately uses database-generated primary keys, resolving profiles by case-insensitive email and constructing links/scan URLs from their actual saved IDs. Keep fixture shape/content aligned; do not require SQL IDs to match memory IDs. Unchanged SQL payloads are skipped on rerun through semantic existence checks, while changed payloads may add rows. It is not automatically executed by Go. Removing the SQL file does not remove already seeded database rows.

### `mock_scans.go`

`(*app).mockScan(w, r)` serves `/mock-scans/{id}/{scan}`. It accepts series 1 or 2, checks that the requester is the same patient or a doctor on that patient's care team, loads and verifies the patient profile, and renders `mock_scan.svg`. Invalid series produce 404; unauthorized requests produce 403; store/render errors follow the handler's error paths. Authentication is necessary even though the picture is synthetic because the image contains patient attribution.

The function computes a deterministic angle from the ID and series, then passes an anonymous view struct with `Patient Profile`, `Number`, `Angle`, and `Reference` to the template. `Reference` includes the last six ID characters and series. This is a local presentation struct, not a persisted database entity. The current IDs are UUID-shaped; preserve that invariant or add a length check before changing ID formats because the code slices the ID suffix.

The response sets `image/svg+xml; charset=utf-8` and a restrictive SVG content-security policy. No remote scan host, image file per patient, or clinical image storage exists. The 600 bulk scan URLs generate illustrations on demand.

## Go regression tests

All `*_test.go` files are maintained test source. Go excludes them from the normal application executable and runs them with `go test`. They use isolated in-memory stores and temporary HTTP servers, not Supabase or an existing running app. They do not execute Datastar in a browser; the browser checks below cover that separate layer.

From the actual project:

```powershell
Set-Location 'D:\projects\ClearChart'
go test -count=1 ./...
go vet ./...
```

`-count=1` requests fresh test execution instead of a cached result. The tests still use normal Go compilation caches. The test helpers parse the embedded templates, so these tests also catch malformed saved template syntax.

### `main_test.go`: helpers

| Function | What it does and why |
| --- | --- |
| `testApp(t)` | Builds a new app with `NewMemoryStore`, returns the app and routed HTTP handler, and registers cleanup of the hub/store. Gives each test independent data and sessions. |
| `demoSession(t, a, handler, role)` | Visits the role's demo endpoint, requires the redirect, checks HTTP-only/SameSite cookie properties, resolves the created session, and returns cookies, session, and dashboard URL. Lets tests exercise real session setup. |
| `serveRequest(handler, r, cookies)` | Adds supplied cookies, calls the handler through `httptest.ResponseRecorder`, and returns the captured response. Shared by both GET and POST tests. |
| `postForm(handler, path, values, cookies)` | Encodes `url.Values` as an ordinary URL-encoded form and delegates to `serveRequest`. |
| `assertSSE(t, w, selector, mode)` | Requires a successful SSE content type and Datastar patch event fields, including selector, patch mode, and elements. Catches responses that save data but fail to update the requested DOM target. |
| `uploadRequest(t, fields, filename, contents)` | Constructs a complete multipart request with form fields and a `report` file part. Allows filename, byte-content, extension, size, and authorization cases without real files on disk. |

### `main_test.go`: tests

| Test | Behavior protected |
| --- | --- |
| `TestDashboardsRenderAndDemoSessionsCoexist` | Doctor/patient cookies coexist; both pages render without failed interpolation markers, custom script bodies, or native inline JavaScript handlers; role-appropriate live attributes exist; unrelated-patient and unlinked-doctor reads are forbidden. |
| `TestDoctorRecordEscapedAndVisibleToPatient` | Doctor note returns a prepend SSE patch, persists exactly once with correct author/content, escapes script-like content in HTML, and appears on the patient's page. |
| `TestRejectedRecordsDoNotWrite` | Table-driven rejection of invalid CSRF, a patient trying to write clinician notes, an unlinked patient, an impersonated doctor, and a cross-origin request. Each must show error feedback without changing records. Its anonymous case struct holds name, role, token/IDs, and origin overrides. |
| `TestUploadStoresFilenameAndSharesWithCareTeam` | Windows-style upload paths are reduced to a safe filename, the patient receives an SSE row, a linked doctor sees it, file bytes never enter HTML, and an unrelated patient's filename is excluded. |
| `TestRejectedUploadsDoNotWrite` | Rejects a foreign patient ID, bad CSRF, a file above 5 MB, and an unsupported extension without inserting upload metadata. Its anonymous case struct holds name/ID/filename, byte size, and the bad-CSRF flag. |
| `TestOnboardingCreatesRoleSpecificProfiles` | Each role gets an onboarding CSRF cookie, escaped success output, a new usable session/profile/dashboard, only its relevant role fields, and the current demo care-team link. Its two role subtests deliberately assert the demo linking behavior that must change if onboarding becomes real. |
| `TestLivePatientStreamReceivesDoctorRecord` | Opens a real temporary-server patient SSE connection and confirms a doctor POST from another view reaches it within a five-second context. The scanner looks for a unique marker. |

### `live_test.go`: helpers, struct, and tests

| Symbol | What it does and why |
| --- | --- |
| `openTestStream(t, handler, cookies, path)` | Starts a temporary HTTP server, creates a five-second authenticated SSE GET, requires HTTP 200, registers cancellation/body/server cleanup, and returns a scanner over the live response. |
| `readTestSnapshot(t, scanner)` | Collects events until the terminating blank line of the `#upload-count` patch. This relies on `snapshot` emitting that count last; update the helper if the snapshot event order changes. |
| `timelineEvent(snapshot)` | Finds the event targeting exactly `#timeline`, allowing assertions about timeline contents without confusing them with reports or summary text. |
| `deadlineRecorder` | Test-only struct embedding `*httptest.ResponseRecorder` and holding the latest `time.Time` deadline. Gives `http.ResponseController` something observable to call. |
| `(*deadlineRecorder).SetWriteDeadline(deadline)` | Records the supplied deadline and returns nil. It simulates the response writer's deadline support; it does not delay real I/O. |
| `TestLiveDoctorStreamReceivesPatientUpload` | Patient upload reaches the doctor's live reports center exactly once, without exposing unlinked filenames or uploaded bytes. |
| `TestLivePatientStreamPreservesFilterAndPatientScope` | An imaging-filtered stream starts with imaging only and retains both type and patient scope after note and imaging mutations. Its local `checkTimeline` closure reuses the same scope assertions for initial/subsequent snapshots. |
| `TestStreamWritesClearDeadlineForIdleInterval` | `writeStream` arms a future deadline during a write, clears it before idle time, and also supports response writers that cannot set deadlines. Its write callbacks check the active deadline and emit small SSE comments. |

### Directory, fixture, and selection tests

| File / test | Behavior protected |
| --- | --- |
| `directory_test.go` / `TestDoctorCanSelectFirstMiddleAndLastPatient` | With hundreds of patients, ordinary dashboard requests for the first/middle/last entry show the matching name, hidden patient ID, and every expected record. This tests server selection and fallback links; it does not measure browser scroll. |
| `directory_test.go` / `TestMockScanIsPatientSpecificAndCareTeamScoped` | Linked doctor can fetch two distinct, valid SVG/XML series containing the intended patient's name and demo label. Anonymous, unrelated-patient, and unlinked-doctor access fail. |
| `directory_test.go` / `TestBulkDoctorReportsRemainScoped` | There are hundreds of reports and every returned report belongs to a patient linked to the requesting doctor. |
| `bulk_seed_test.go` / `TestBulkDemoPatientsHaveIndividualCompleteCharts` | Exact total/directory counts; Alex first and Zoe last; original authorization retained; unique IDs/names/emails; valid fictional DOB/email; care-team size; five correctly attributed, descending records; two notes/one prescription/two unique images; one upload; three valid healing scores; 600 distinct image URLs. It intentionally couples to fixture shape so accidental fixture drift is visible. |
| `selection_test.go` / `TestDoctorSignalSelectionPatchesOnlyTheSelectedChart` | Signal-selected Zoe chart updates summary/form/timeline with correct IDs and scan references, never patches the directory/shell, receives a later same-patient note, and preserves the form on routine live refresh. |
| `selection_test.go` / `TestDoctorSignalSelectionValidatesCareTeamAndJSON` | Rejects an unlinked signal-selected patient, a non-string patient value, malformed JSON, and an oversized payload. A valid legacy `patient` query cannot override an invalid actual selection. Its anonymous case struct pairs each signal payload with its expected status. |

### `profile_ids_test.go`: arbitrary stored IDs

| Symbol | What it does and why |
| --- | --- |
| `remapMemoryProfile(s, oldID, newID)` | Test-only helper that changes a seeded profile ID under the store lock and rewrites care links, record attribution, upload/healing references, and biometric keys. Simulates arbitrary PostgreSQL profile IDs without changing fixture identity or requiring a live database. |
| `TestDemoAndOnboardingUseStoredProfileIDs` | Remaps Sarah/Alex to fresh random IDs, then verifies default demo login, redirect/dashboard loading, and both onboarding roles use the actual stored counterpart IDs. Also checks case-insensitive email lookup, missing-email `ErrNotFound`, and canceled-context behavior. This exercises the shared contract in memory; it does not replace SQL integration testing. |

Apart from the named `deadlineRecorder`, test case structs are local anonymous tables described above. `t.Cleanup`, `t.Run`, and streaming callbacks are local closures for cleanup or those documented scenarios; they are not application entry points. There are no production mocks implemented as replacement `Store` test types in these files: tests use the real in-memory `Store` implementation.

## Optional browser regression harness

Actual `.tools` inventory is limited to `browser-cdp.mjs`, `browser-selection.mjs`, `chrome-check/`, `go-cache/`, and `screenshots/`. There is no portable `.tools/go` installation in this project. The browser harness uses Chrome DevTools Protocol (CDP) and Node's built-in APIs, with no npm package dependency. Use a Node version exposing global `fetch` and `WebSocket`; the prior check used the installed Node 24 runtime. Chrome must be started separately with a dedicated profile and debugging port, and the Go test app must already be running.

The harness defaults to app `http://127.0.0.1:8080`, debugger `http://127.0.0.1:9222`, and screenshots relative to the current directory at `.tools/screenshots`. Override `CLEARCHART_URL`/`CHROME_DEBUG_URL` for your test setup. Run it against an isolated **in-memory demo**, or a disposable fully seeded SQL test database: it submits a synthetic doctor note into the target app. It is not read-only, and should not target a shared database just to check scrolling. Patient IDs are discovered by name from the rendered directory, so database-generated IDs are supported; the expected fixture names/counts still need to be present.

```powershell
Set-Location 'D:\projects\ClearChart'
# Start a separate demo server and dedicated debugging Chrome first.
$env:CLEARCHART_URL = 'http://127.0.0.1:8081'
$env:CHROME_DEBUG_URL = 'http://127.0.0.1:9222'
node .tools/browser-selection.mjs
```

### `browser-cdp.mjs`: every helper and method

| Symbol | What it does and why |
| --- | --- |
| `sleep(ms)` | Promise-based pause used for polling, rendering settlement, and live-refresh checks. |
| `connections` | Exported list of created CDP clients so the caller can collect errors and close connections in `finally`. |
| `CDP` | Client wrapper holding the WebSocket, label, request counter, pending response map, observed network URLs, and browser errors. |
| `CDP.constructor(ws, label)` | Sets state and registers a message callback. The callback resolves/rejects pending command IDs, records network requests, and collects runtime/log errors. |
| `CDP.send(method, params)` | Sends one protocol command with a unique ID and a 15-second timeout; returns the matching response promise. Its timeout callback removes the pending entry and rejects it. |
| `CDP.evaluate(expression)` | Runs a test expression in the page, awaits promises, returns its value, and throws browser evaluation exceptions. These expressions exist only in the test harness. |
| `CDP.waitFor(expression, description, timeout)` | Polls a page condition every 100 ms until true or its default 20-second timeout, then reports collected errors on failure. |
| `CDP.capture(filename)` | Brings the target forward, captures a viewport PNG, and writes it beneath `.tools/screenshots`. |
| `openPage(label, route)` | Creates a debugging tab, connects its WebSocket, enables Page/Runtime/Network/Log domains, sets a 1440×1100 viewport, navigates to the app route, waits for `#timeline` and a live-stream request, and returns the CDP client. WebSocket open/error callbacks handle connection establishment. |

### `browser-selection.mjs`: helpers and scenarios

| Symbol or block | What it does and why |
| --- | --- |
| `patientIDByName(name)` | Finds exactly one directory card with the requested displayed name and reads its actual patient ID from the link. Avoids assuming deterministic memory IDs when the same fixtures are stored with PostgreSQL-generated IDs. |
| `selected(id)` | Builds a page predicate requiring both the note form's hidden patient ID and a scan URL to match the expected patient. Checks that related chart pieces changed together. |
| `clickPatient(id)` | Gets the target card's position using a page expression, then sends actual mouse press/release events through CDP. Exercises the click handler instead of assigning the selection signal directly. |
| Top-level `try` | Runs the scenarios described below and prints each passing assertion group. There is no additional named runner function. |
| Top-level `catch` | Prints current feedback, selected patient, disabled state, a timeline excerpt, requests, and errors before rethrowing the failure. |
| Top-level `finally` | Closes the CDP WebSocket connections. It does **not** terminate Chrome, close its tabs, or stop the Go server. |

The scenarios assert 302 selectable cards; selection of the last patient without replacing the directory/document or moving directory/page scroll; clearing a draft on patient change; rapid switching under artificial latency followed by a 16-second wait; blocking a switch while a note is saving; saving without reopening the selection stream; the new note reaching the intended patient's chart; preserving a draft across a live refresh; and selecting a middle patient at 390px width without scroll reset or horizontal overflow. It writes desktop/mobile screenshots and rejects unexpected browser errors. Inline page callbacks save DOM/time-origin references, read UI state, set test form values, and prepare scroll/viewport conditions for these assertions.

These browser files are currently under `.tools/`, which `.gitignore` excludes. For a durable handoff, move or deliberately version them under a retained test directory and update their paths/imports/output configuration before cleaning `.tools`; otherwise a fresh clone will not include this useful regression coverage. Do not confuse reusable test scripts with screenshots and caches merely because they share a directory today.

## What can be cleaned up

No cleanup is performed by this guide. The categories below describe dependencies, not an instruction to delete everything marked optional.

| Category | Files/data | Keep/remove guidance |
| --- | --- | --- |
| Required application source | `main.go`, `handlers.go`, `live.go`, `models.go`, `store.go`, `go.mod`, `go.sum`, dashboard/onboarding/shared/doctor-fragment templates, `static/styles.css` | Keep for the current app. Go files share package `main`; moving/deleting a file can remove symbols used elsewhere. Embedded templates/assets must stay consistent with their callers. |
| Database definition | `schema.sql` | Retain as the schema contract and setup reference. It is not a disposable fixture file. Schema changes should be explicit migrations, not ad hoc cleanup. |
| Maintained regression source | All `*_test.go`; the two browser `.mjs` files | Keep/version. They are excluded from production runtime already. Updating an intentional feature requires adjusting its tests, not deleting tests to make failures disappear. |
| Active demo implementation/content | `bulk_seed.go`, `mock_scans.go`, `templates/mock_scan.html`, `static/ct-scan.svg`, `seed.sql`, `seed_large.sql`, base fixture code and biometric samples in `store.go`, rule-based summary/demo links | Optional product features, but not safely deletable in isolation. Follow the staged changes below. |
| Rebuildable cache | `.tools/go-cache/` | Disposable after Go commands stop. Go recreates build cache. Confirm installed Go works and reset any environment path before relocating/removing the directory. Expect the next build to be slower. |
| Dedicated browser profile | `.tools/chrome-check/` | Disposable after the specific test Chrome instance is closed. This contains test-browser state; do not remove a profile in use or delete the user's ordinary Chrome profile. |
| Generated visual artifacts | `.tools/screenshots/` (desktop/mobile PNGs) | May be removed once any review/evidence you want to keep has been archived. The harness recreates the output directory. |
| Optional generated binaries/logs | `*.exe`, `*.test`, `coverage.out`, `*.log`, if later produced in the project | Not required source. Stop the exact running test binary first and preserve any useful diagnostics. Do not assume all executables elsewhere on the machine belong to this project. |
| Configuration/reference | `.env.example`, `.gitignore`, `README.md`, `docs/` | Retain for setup and handoff. Actual `.env` files, if created, are private configuration, not generic temporary files; the current app does not automatically load them. |
| Old project copy | `D:\projects\hackkathon` | Only conditionally redundant. It may contain unique code, Git history, notes, credentials/configuration, or test scripts. Compare/version/archive needed work, verify all tools/terminals point to ClearChart, and stop processes launched from the old copy before considering removal. Nothing in the running app requires that old folder once ClearChart's dependencies/toolchain are self-contained. |

### Safe order for generated-artifact cleanup

1. Work from `D:\projects\ClearChart`; confirm the intended absolute paths before removing any directory. Preserve/version the two browser scripts first if they are not already tracked elsewhere.
2. Stop active Go checks and the dedicated debugging Chrome instance. Close its test tabs as well; repeated open SSE tabs can otherwise accumulate browser connections. Leave the user's normal server/browser alone unless that is the process you deliberately intend to stop.
3. Check `Get-Command go` and `go version`. Inspect `go env GOCACHE GOMODCACHE GOPATH` for dependencies on old workspace paths. Clear or change only task-specific environment overrides you no longer want. This ClearChart copy has an installed Go workflow, not a portable `.tools/go` workflow.
4. Archive screenshots/logs you want, then remove only the selected generated cache/profile/screenshot paths. `chrome-check`, `go-cache`, and `screenshots` are separate from the browser test source. No recursive deletion command is supplied to avoid turning a path typo into a project-wide removal.
5. Run fresh Go tests/vet and `go run .` again. If preserving browser checks, rerun them with a fresh dedicated browser profile and confirm their output directories are recreated.

### Safe order for removing demo features

Choose the scope first: reducing the 300-patient directory, removing illustrations, or replacing all demo identity/data behavior are different changes. An empty database alone does not remove demo assumptions in the application.

1. **Preserve a working reference and separate test fixtures from runtime seeding.** `testApp` currently calls `NewMemoryStore`, and many tests depend on its base IDs and bulk patients. If production/demo setup changes, keep an explicit fixture constructor for tests or create small per-test fixtures. Preserve coverage of permissions, escaping, uploads, live updates, and selection even if fixture-specific counts change.
2. **For bulk patients only, remove the call before the function/file.** Refactor `NewMemoryStore`'s `addBulkPatients(s, now)` call before deleting `bulk_seed.go`; otherwise the package will not compile. Update `bulk_seed_test.go`, the first/middle/last and reports tests, signal-selection fixtures, browser counts/name-based fixture lookups, and README walkthrough. `seed_large.sql` is separate from memory seeding. Existing PostgreSQL rows remain until an explicit reviewed data migration; deleting or skipping the script does not remove them. Keep base seed doctors if the rest of the demo still depends on them.
3. **Replace image references before deleting image providers.** Remove or migrate `/mock-scans/...` values in memory fixtures, SQL seeds, and already persisted `medical_records.image_url` data, and decide what imaging without an image should render. Update image tests. Only then remove the route in `main.go`, `mockScan`, and `mock_scan.svg` template together. For `/static/ct-scan.svg`, also update `handlers.go`'s new-imaging default, the `record` fallback, base memory fixtures, and `seed.sql` before deleting the asset. A removed asset with surviving URLs produces broken chart images.
4. **Replace demo identity and automatic care-team linking before removing base profiles.** `/demo/{role}`, `demoDoctorID`/`demoPatientID`, the home/role-switch experience, and both stores' `CreateProfile` methods rely on demo identity or links. Add the intended sign-in/profile selection and explicit care-team invitation/assignment flow; adapt onboarding/tests. Both stores resolve automatic links by the counterpart's case-insensitive demo email and role. The memory store initializes missing care maps; a missing counterpart now leaves the new profile unlinked rather than requiring its old fixed ID. Default demo login now finds profiles by case-insensitive email via `ProfileByEmail`; the fixed UUID constants apply only to memory fixtures/tests. In PostgreSQL, onboarding demo-link inserts depend on the base profiles being present under their expected emails and roles, not predetermined UUIDs. These changes must be made before retiring base `seed.sql` data or the in-memory constructor.
5. **Retire mock biometric/assistance presentation consistently.** If removing watch samples, either retain a supported empty state or remove its template card and the dashboard/store/model calls together. Keep the schema/table until no query or migration depends on it. If removing rule-based assistance, remove the `ai-summary` calls/patches and `.Summary` preparation before deleting `plainSummary`. CSS healing graphs are presentation of stored patient check-ins; they need not be removed just because initial scores were seeded.
6. **Keep filename-only uploads honest or implement storage deliberately.** Removing fixture upload filenames does not implement file storage. Real attachments require an explicit storage/authenticated-download design and changes to the upload handler, schema/models/store, templates, and tests. Until then the current rows remain metadata only.
7. **Verify each stage before discarding files or data.** Run `go test -count=1 ./...`, `go vet ./...`, start the app from ClearChart, and exercise both roles, onboarding, selection/scroll, live notes, uploads, and any retained images. Use a disposable database for schema/migration checks. Search for old route strings, asset paths, demo constants, and removed template names to ensure no callers remain. Database cleanup needs its own reviewed migration; never treat deleting a seed script as rolling back seeded data.

The current tests primarily validate memory-mode behavior. They do not prove that a PostgreSQL migration, real authentication, real file storage, a wearable connection, or an LLM integration works. Those are separate features with separate verification needs.

## Reproduce the browser check from this folder

This is optional browser verification, separate from `go test`. Keep its fixtures in an isolated memory-mode process because it creates a note.

In **terminal A**, start the dedicated test server:

```powershell
Set-Location D:\projects\ClearChart
Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
$env:HOST = '127.0.0.1'
$env:PORT = '8081'
go run .
```

In **terminal B**, start only the dedicated test Chrome profile and run the retained scripts. Close an earlier debugging instance using that profile before launching a replacement:

```powershell
Set-Location D:\projects\ClearChart
$clearChartChrome = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
Start-Process -FilePath $clearChartChrome -WindowStyle Hidden -ArgumentList @(
  '--headless=new',
  '--disable-gpu',
  '--no-first-run',
  '--no-default-browser-check',
  '--remote-debugging-port=9222',
  '--user-data-dir=D:\projects\ClearChart\.tools\chrome-check',
  'about:blank'
)
$env:CLEARCHART_URL = 'http://127.0.0.1:8081'
$env:CHROME_DEBUG_URL = 'http://127.0.0.1:9222'
node .tools/browser-selection.mjs
```

If `node` is not on PATH, this machine also had a Node executable at `C:\Program Files\Microsoft Visual Studio\18\Community\MSBuild\Microsoft\VisualStudio\NodeJs\node.exe`. Invoke its full path with PowerShell's `&` operator, or use an installed Node version with the required built-in APIs. Confirm the executable exists on your machine instead of assuming that path is universal.

After the check, stop terminal A with Ctrl+C. Close the dedicated debugging browser, not every Chrome process. With Node available, this command talks only to the browser listening on your test debugging port; verify that port belongs to your dedicated test instance first:

```powershell
@'
const debug = 'http://127.0.0.1:9222';
const info = await (await fetch(debug + '/json/version')).json();
const ws = new WebSocket(info.webSocketDebuggerUrl);
ws.addEventListener('open', () => ws.send(JSON.stringify({id: 1, method: 'Browser.close'})));
await new Promise(resolve => ws.addEventListener('close', resolve));
'@ | node --input-type=module
```

The check's `.tools/screenshots` directory may also contain `docs-*.png` images from reviewing this handoff's diagrams. Those are disposable review screenshots. Keep the actual SVG/XML documentation under `docs/`.
