# ClearChart maintainer handoff

This guide documents **D:\projects\ClearChart** as of September 26, 2026. It is written so you can continue without this chat. The older `D:\projects\hackkathon` folder is a separate copy; changes there do not change this project.

## Read in this order

1. This walkthrough: how execution starts and how the pieces fit.
2. [Diagrams](diagrams.md): four visual flows, with editable XML and offline SVG previews.
3. [Runtime reference](runtime-reference.md): every declaration and operation in `main.go`, `handlers.go`, and `live.go`.
4. [Models, storage, and database setup](storage-database.md): every model field, interface, memory/SQL method, schema object, and persistence setup.
5. [Templates, tests, and cleanup](templates-tests-cleanup.md): every template, fixture/helper/test, temporary artifact, and safe removal path.
6. [Symbol index](symbol-index.md): a complete inventory of named Go declarations linked to their reference.

These are source explanations, not a statement that unfinished features already exist. The application was checked with Go tests and desktop/mobile browser flows in demo mode. No live PostgreSQL database has been configured or integration-tested for this handoff.

## 1. Start at main()

Open [main.go](../main.go) and find `func main()`.

```text
Go initializes the package and embedded files
  main()
    create an interrupt-aware context
    inspect DATABASE_URL
      absent -> NewMemoryStore() -> base fixtures + addBulkPatients()
      present -> NewPostgresStore() -> open pool + ping
    newApp(store, mode) -> parse templates, create session map and event hub
    routes() -> register URL/method handlers
    build http.Server using HOST / PORT
    start a shutdown goroutine
    ListenAndServe() -> wait for incoming requests
```

The application's non-test Go files use `package main`. They form one application, not separate services. Files ending in `_test.go` are included by `go test`, not by `go run` or the deployed build. `go run .` compiles the whole package; `go run main.go` excludes the sibling implementation files and is the wrong command for this project.

Before `main()` runs, the build embeds `templates/*.html` and `static/*` into `assets`. There is no template-file watcher. Editing an HTML/CSS file requires a rebuild/restart to affect the running program. A broken template can prevent **the whole application** from starting because `newApp()` parses all templates together. Embedding packages files into a program at build time; see [Go embed](https://pkg.go.dev/embed).

The database decision happens once at startup. With no connection string, all profiles, records, uploads, and recovery scores live in maps/slices and vanish on stop. A nonempty connection string activates the existing SQL implementation; a connection failure stops startup rather than silently reverting to memory. Sessions and the event hub stay in memory in both modes.

`newApp()` bundles the store, parsed templates, session map, mutex, mode label, and hub into `app`. Methods such as `a.dashboard()` share that bundle. This is dependency injection through a small struct: tests supply a memory store; a running server can supply PostgreSQL.

`routes()` returns a `net/http` handler that sets common headers and dispatches requests to the right method. `ListenAndServe()` keeps serving; it does not call every route once in a predetermined order. The browser determines which route runs next.

The shutdown goroutine waits for Ctrl+C, closes the hub to release live streams, and asks the HTTP server to shut down with a five-second deadline. The present code only registers `os.Interrupt`; a Linux deployment also needs deliberate handling of its termination signal and waiting for shutdown completion. See the runtime reference for the current limitations.

## 2. Your first page request

Opening `/` calls `home()`. It reuses a valid patient session or redirects to `/demo/patient`. The doctor demo uses `/demo/doctor`.

`demo()` finds the default fixture by email through `Store.ProfileByEmail()` (or an explicitly supplied `?id=` through `Store.Profile()`), creates a session with `setSession()`, sends a role-specific cookie, and redirects to the dashboard URL. This is selectable demo identity, **not a verified login**. The doctor and patient cookie names differ so both roles can coexist in one browser.

`dashboard()` checks the session and requested ID, then calls `dashboardData()`. That function assembles a `Dashboard` value:

- The signed-in `Profile` and the selected `Patient` are different fields. A doctor's profile must never be confused with their patient's profile.
- Doctors get a linked-patient directory, all linked patients' report metadata, and their own mock biometric reading.
- The selected chart contains records, uploads, linked doctors, latest recovery values, summary text, and counts.
- Patients are always restricted to their session's patient ID; a request parameter cannot select someone else's chart.

`page()` calls `render()`, which executes a named HTML template with that value. Go templates use exported fields such as `{{.Patient.Name}}`. `html/template` escapes interpolated data according to its HTML context; keep that escaping when extending views. See [Go html/template](https://pkg.go.dev/html/template).

The resulting page is ordinary HTML. The shared head loads CSS and Datastar v1.0.3 from a CDN. Datastar then interprets `data-*` attributes to send requests and apply returned HTML. There are no application-authored JavaScript modules, React components, templ-generated files, or browser-to-database queries. Datastar itself is JavaScript, and its attributes contain small expressions. Browser-test JavaScript under `.tools` is separate from the shipped app.

## 3. One note travels through the entire system

Follow this chain while reading [handlers.go](../handlers.go), [store.go](../store.go), [live.go](../live.go), and [shared.html](../templates/shared.html):

```text
doctor-record-form
  @post('/add-record', {contentType: 'form'})
    addRecord()
      parseForm() -> authorizePost() -> IsCareTeam() -> validation
      Store.AddRecord()
      render("record", savedRecord)
      startSSE() + patch("#timeline", "prepend", ...)
      writeCounts() + feedback
      hub.publish(patientID, viewID)
        events() in other open tabs wakes
          dashboardData() -> snapshot() -> patch(...)
            Datastar updates those tabs
```

The POST includes hidden `csrf`, `doctor_id`, `patient_id`, and `view_id` values. The server verifies identity/relationship fields rather than trusting the form. The store writes the record and returns the saved data.

The POST's response is a **short SSE response** containing several fragments, then it ends. Separately, each dashboard maintains a **long-lived GET SSE stream** at `/events/...`. Those two connections serve different purposes. SSE flows server-to-browser; user actions flow browser-to-server as ordinary HTTP requests. Together they provide bidirectional interaction without a WebSocket.

`view_id` is a per-page identifier used to skip an immediate hub echo to the authoring tab, which already received the POST result. It is not authentication, a session ID, a transaction ID, or a durable subscription.

The hub carries a small notification, not the medical record itself. Each stream checks whether the patient is relevant and reloads current data from the store. A 15-second timer also refreshes it. There is no event log or replay cursor. The current implementation favors straightforward correctness over minimal database queries or bandwidth.

The [runtime reference](runtime-reference.md) shows the exact SSE event format, why each HTML line needs its own `data: elements` prefix, what flush does, and why validation failures are returned as visible HTTP-200 SSE feedback.

## 4. Why patient clicks no longer reset scrolling

In [doctor_dashboard.html](../templates/doctor_dashboard.html), a patient click changes `$selectedpatient`. A root `data-effect` opens the same GET action URL using that selected ID as a signal. The preceding request is canceled as the selection changes; the browser checks exercised rapid switching.

The server reads the `datastar` query payload, validates the relationship, then sends `doctorContext()` plus a snapshot. It updates the selected-name card, form, and timeline. It never replaces the directory or page shell, so their scroll positions survive. A regular background snapshot also leaves the note form alone.

Each form/control ID includes the patient ID. Switching charts clears the previous draft instead of morphing it into another patient's form. During a note save, selection is gated; while selection is pending, the old form is disabled. The check icon reserves its width to avoid row-height shifts.

The selection does not update browser history or the address bar. Refresh therefore uses the patient in the current URL, or the default when no patient parameter is present. A normal patient anchor remains as a navigation fallback.

These are important constraints to preserve when changing templates. See [Datastar actions](https://data-star.dev/reference/actions) and [attributes](https://data-star.dev/reference/attributes) for upstream concepts; current upstream documentation may differ from the pinned bundle, so verify upgrades with the regression tests.

## 5. What to edit for a feature

| Change | Start here | Follow-through |
| --- | --- | --- |
| Page text, layout, colors | `templates/`, `static/styles.css` | Restart; check desktop/mobile and template parsing. |
| Display a new profile field | `models.go`, `schema.sql` via a new migration | Update both stores, `profileColumns`/`scanProfile`, validation, form and display templates. Column and scan order must match. |
| Add a new operation | `Store` interface and implementations | Register route, authenticate/authorize, validate, write, render a fragment, publish patient change. |
| Add record kind | `validType`, SQL CHECK, presentation labels | Update forms, badges/icons, filtering, both stores/fixtures and tests. |
| Add care-team management | New authenticated endpoints/store methods | Replace seed-based auto-linking with authorized links; reload the directory or add a deliberate directory update that preserves scroll. |
| Save actual PDFs/images | `uploadReport()`, `Upload`, SQL and a private object store | Replace `io.Discard`, persist an object key, add authorized downloads and upload validation/lifecycle tests. Filenames alone cannot recover old files. |
| Replace demo login | `demo()`, `setSession()`, `sessionFor()`, `onboard()` | Verified identity, shared/revocable sessions, profile ownership, cookie/TLS policy, account recovery. Keep server-side care checks. |
| Scale directory/history | `Patients()`, `Records()`, `Reports()`, `dashboardData()` | Add pagination/search and smaller live refreshes; currently all relevant rows are loaded repeatedly. |

For a new write flow, copy the **sequence** from `addRecord()`, not its patient-specific assumptions. Never accept a submitted doctor ID as proof of identity. Return escaped fragments with stable target IDs and publish only after a successful write. Test the authorized case, an unrelated patient's case, invalid data, and the receiving tab.

## 6. Add a real database without rewriting the app

The [database guide](storage-database.md) contains complete Supabase and local PostgreSQL instructions, copyable commands, schema explanations, and a persistence test.

For a persistent **synthetic-data demo**:

1. Provision PostgreSQL/Supabase and apply `schema.sql`.
2. Apply `seed.sql` for the current demo entry points; add `seed_large.sql` only if you want the larger fixture directory.
3. Set `DATABASE_URL` in the same shell/process that starts Go, using the provider's actual connection string.
4. Restart Go, add a uniquely named note, verify it with SQL, stop/restart Go, and verify it remains.

`.env.example` is documentation. Copying it to `.env` does not load variables: the code only calls `os.Getenv`. Neither SQL scripts nor memory-to-database imports run automatically. Existing in-memory edits are not transferred.

The memory store and deterministic fixtures remain available for local work until Supabase is ready, and for regression tests afterward. Their presence does not stop SQL persistence. PostgreSQL supplies primary-key IDs for SQL inserts; seeds reuse profiles by case-insensitive email and construct links/image URLs from the actual saved IDs. Memory fixture IDs and PostgreSQL IDs do not have to match. Database credentials remain on the server. The code does not use Supabase Auth, Storage, or Realtime APIs. Its SQL tables have RLS enabled without user policies, and the backend uses a suitably privileged connection; that is not an end-user identity/access model.

## 7. Run, build, and move to a server

Development from PowerShell:

```powershell
Set-Location D:\projects\ClearChart
go version
go mod download
go test ./...
go vet ./...
go run .
```

If the local build cache reports inconsistent standard-library errors, retry with a fresh project-local cache in that terminal: `$env:GOCACHE = "$PWD\.tools\go-cache"`. This workaround was used successfully in this workspace; it does not install a missing/broken Go toolchain. There is no portable Go installation inside this project's `.tools`.

Build a Windows executable:

```powershell
go build -o clearchart.exe .
.\clearchart.exe
```

The executable contains templates and static assets, so deploy the rebuilt executable appropriate to the target OS/architecture. It still needs a reachable database and process environment, and the current page depends on CDN access for Datastar/Tailwind/fonts. SQL schema changes are deployed separately. Docs, test files, build caches, and source files need not be copied alongside the executable just to run it.

For an operational host, configure a process supervisor and secrets, HTTPS termination, trusted proxy/cookie behavior, graceful termination, backups/restores, and monitoring. Ensure the proxy streams SSE without buffering and permits connections to stay open. `/healthz` says the HTTP process is alive; it does not test database readiness. Default binding is `127.0.0.1:8080`; set `HOST=0.0.0.0` only when the host/network setup requires it.

Connecting PostgreSQL makes data persistent. Before entrusting actual patient data to this application, replace demo identities, implement real access management/auditing, and address the gaps listed in the database/runtime guides. This document does not claim that the current prototype is a production EHR.

## 8. Debugging map

| Symptom | Check |
| --- | --- |
| Same error after a fix | `Get-Location`, `go env GOMOD`, saved editor path, and executable being launched. ClearChart and hackkathon are different copies. |
| `unterminated quoted string` | A formatter may have split a quoted Go template literal. Patient filter comparisons use backticks inside HTML attributes; inspect the exact reported line and run `go test ./...`. Do not strip escaping globally. |
| Port already in use | Stop your previous server with Ctrl+C, or use `$env:PORT='8081'` for a second instance. Do not terminate an unidentified process. |
| Demo data returns after restart | `DATABASE_URL` is unset in the server's process, or you are running a different process/copy. |
| Connection succeeds but demo is missing | Check schema, role privileges, and default fixture emails (`sarah.chen@example.com` / `alex.morgan@example.com`). A successful ping does not load the schema. |
| Page loads but actions do nothing | Browser console/network: Datastar CDN request, `/events` response, correct MIME type, and SSE event format. |
| Scroll reset returns | Look for ordinary navigation or replacement of `#patient-directory`/the root shell. Keep stream action URL stable. |
| Wrong patient/form | Verify signal payload, care-team check, hidden patient ID and patient-specific form IDs. Run `selection_test.go` and browser check. |
| Report name appears but no file download | Expected today: bytes were discarded. Implement private file storage before promising downloads. |
| A new linked patient is absent | Reload directory; background snapshots do not rebuild the directory. |
| A second server misses immediate updates | Hub and sessions are process-local. Shared PostgreSQL alone does not distribute them. |
| Tests pass but database fails | Existing Go tests use memory storage; execute the database persistence smoke test too. |

## 9. Keep working after the chat ends

Commit the source and these docs to your own version control after reviewing them. Keep credentials, cache folders, and browser profiles out of commits. The [cleanup guide](templates-tests-cleanup.md) lists exactly what is disposable and what has callers; nothing was deleted for this handoff.

When starting a future change, read the relevant function entry, check its callers, make the smallest coherent change through model/store/handler/template, and run the appropriate existing tests. Update these docs when behavior changes.

A context note to give another developer:

> Work in D:\projects\ClearChart. This is a Go net/http + html/template + Datastar v1.0.3 prototype with no custom application JavaScript. Store selects memory or lib/pq PostgreSQL at startup through DATABASE_URL. Read docs/README.md and the function references first. Preserve server authorization, filename-only upload disclosure, stable patient-directory DOM, fixed selection stream URL, patient-specific form IDs, and cross-tab SSE behavior. Current login, scans, AI summaries, and wearable readings are demo implementations. Use the database persistence test before claiming SQL mode works in a particular environment.
