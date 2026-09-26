# ClearChart

A shared patient and provider workspace built with Go net/http, html/template, PostgreSQL, and Datastar. Go renders the HTML; Datastar applies server-sent fragments for records, uploads, healing check-ins, and live patient selection. There is no custom application JavaScript.

## Run locally

Use Go 1.23 or newer from the active project folder:

```powershell
Set-Location D:\projects\ClearChart
go mod download
go run .
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080). You arrive at **sign in**, with a link to create an account. Sign up, confirm your email if requested, sign in, then complete your patient or provider profile. Returning accounts go to their own dashboard.

PostgreSQL and Supabase Auth configuration are required. The server loads a local `.env` when present, and also accepts process environment variables. Keep your existing `DATABASE_URL`. The connection and Auth project must refer to the same intended Supabase project:

```dotenv
DATABASE_URL=postgresql://YOUR_DATABASE_CONNECTION?sslmode=require
SUPABASE_URL=https://YOUR_PROJECT_REF.supabase.co
SUPABASE_PUBLISHABLE_KEY=YOUR_PROJECT_PUBLISHABLE_KEY
HOST=127.0.0.1
PORT=8080
COOKIE_SECURE=false
```

`NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` and `SUPABASE_ANON_KEY` are supported key aliases. If `SUPABASE_URL` is omitted, the server can infer it from a standard Supabase database URL; set it explicitly for other connection formats. Use `COOKIE_SECURE=true` behind an HTTPS reverse proxy. Database passwords remain on the server. Tailwind, Datastar, and fonts load from CDNs.

For an **existing database**, apply [migrations/001_auth_identity.sql](migrations/001_auth_identity.sql) in the Supabase SQL editor before running this version. It adds the unique account-to-profile mapping without deleting profiles, records, or care-team links. For a new database, apply [schema.sql](schema.sql). SQL is not automatically applied at startup.

## Existing synthetic data

Your seeded database rows remain available. `seed.sql` and `seed_large.sql` remain optional, repeatable fixture scripts for database testing: two doctors, 303 patients, and each patient's notes, prescriptions, scan references, reports, and recovery scores. They are not executed by the running server.

New accounts start without care-team links. An administrator must explicitly connect a doctor's account to patients. [Authentication and testing setup](docs/authentication.md#connect-your-account-to-the-existing-synthetic-patients) includes SQL to connect your signed-in provider to the existing synthetic patients. Seeded profiles have no login credentials and are not automatically claimed by email.

The runtime memory store, automatic Go fixture seeding, demo account switcher, and automatic care-team assignments have been removed. Memory fixtures remain in files ending `_test.go` so regression tests can run without your database; Go excludes those files from the server build.

Keep `mock_scans.go`, its template, and `static/ct-scan.svg`: existing database records reference these images. Reports still save filenames only; AI assistance is rule-based; Apple Watch cards contain sample data with no device connected.

## Checks and handoff

```powershell
go test ./...
go vet ./...
go build -o clearchart.exe .
```

Tests use isolated fixtures and fake authentication responses. Optional PostgreSQL integration tests use a separately configured local test server and create/drop their own test database. Do not point them at your live Supabase database.

Start with [the current maintainer handoff](docs/README.md), then [authentication setup and flow](docs/authentication.md). Older exhaustive function references and XML/SVG diagrams are retained with explicit historical labels where startup, authentication, and runtime fixture behavior changed.

If port 8080 is occupied, stop your previous ClearChart process or set `$env:PORT='8081'` before starting a second instance. Always run `go run .`, not `go run main.go`, so Go includes the whole package. Restart after editing embedded templates or CSS.
