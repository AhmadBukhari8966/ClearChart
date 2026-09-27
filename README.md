# ClearChart

A shared patient and provider workspace built with Go net/http, html/template, PostgreSQL (Supabase), and Datastar. Go renders the HTML; Datastar applies server-sent fragments for records, uploads, healing check-ins, timeline filters, and live patient selection. There is no custom application JavaScript.

## Run locally

Use Go 1.23 or newer from the project folder:

```powershell
Set-Location D:\projects\ClearChart
go mod download
go run .
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080). You arrive at **sign in**, with a link to create an account. Sign up, confirm your email if requested, sign in, then complete your patient or provider profile. Returning accounts go to their own dashboard.

Always run `go run .`, not `go run main.go`. Templates and CSS are embedded, so restart after editing them. If port 8080 is busy, stop the old process or set `$env:PORT='8081'`.

## Configuration

The server loads a local `.env` when present; process environment variables take precedence. Keep `.env` out of source control (already git-ignored).

```dotenv
DATABASE_URL=postgresql://YOUR_DATABASE_CONNECTION?sslmode=require
SUPABASE_URL=https://YOUR_PROJECT_REF.supabase.co
SUPABASE_PUBLISHABLE_KEY=YOUR_PROJECT_PUBLISHABLE_KEY
HOST=127.0.0.1
PORT=8080
COOKIE_SECURE=false
```

| Setting | Meaning |
| --- | --- |
| `DATABASE_URL` | Required lib/pq connection URL. For hosts without IPv6, use the Supabase session pooler (port 5432). |
| `SUPABASE_URL` | Project API URL. Inferred from a standard direct (`db.REF.supabase.co`) or pooler (`postgres.REF`) URL when omitted. |
| `SUPABASE_PUBLISHABLE_KEY` | Publishable key for Auth calls. Aliases: `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY`, `SUPABASE_ANON_KEY`. Never use a service-role key. |
| `COOKIE_SECURE` | `true` behind HTTPS; `false` for plain HTTP local development. |
| `HOST`, `PORT` | Defaults `127.0.0.1`, `8080`. Use `HOST=0.0.0.0` in containers. |
| `JUDGE_MODE` | Optional `true`: adds a judge entry on the sign-in page. |
| `GEMINI_API_KEY` | Optional Google AI Studio key. Patient "A little more clarity" card explains the latest doctor note with Gemini (note text is sent to Google). Unset or failing → rule-based text. Secret: use `fly secrets set` on Fly. |
| `GEMINI_MODEL` | Optional, default `gemini-3.5-flash-lite` (~0.5 s, larger free-tier quota than 3.8-flash, whose free tier has a 20-request limit; 2.5-flash is closed to new keys). |

Enable email/password sign-in in Supabase and set Authentication → URL Configuration → Site URL to your ClearChart address (locally `http://127.0.0.1:8080/login`).

## Database

SQL is never applied automatically. For a **new database**, run [schema.sql](schema.sql). For an **existing database**, apply the additive migrations in order in the Supabase SQL editor; the server refuses to start until each is applied:

1. [001_auth_identity.sql](migrations/001_auth_identity.sql): maps Supabase Auth users to profiles.
2. [002_care_invitations.sql](migrations/002_care_invitations.sql): doctor-to-patient invitations with patient consent.
3. [003_record_categories.sql](migrations/003_record_categories.sql): explicit body-area tags for timeline filters; existing records stay uncategorized.
4. [004_notifications_seen.sql](migrations/004_notifications_seen.sql): doctor notification read marker.

`seed.sql` and `seed_large.sql` are optional, repeatable fixture scripts (two doctors, 303 synthetic patients with records, scans, reports and recovery scores). The server never runs them.

### Care-team access

New accounts start without care-team links. Doctors connect to patients through invitations (patient must accept with a matching account email). Seeded profiles have no login and cannot be claimed by signing up with their email.

To give your own provider account access to the seeded cohort, run as an administrator after onboarding:

```sql
WITH testing_doctor AS (
    SELECT id FROM public.profiles
    WHERE role = 'doctor' AND auth_user_id IS NOT NULL
      AND lower(email) = lower('your-doctor-email@example.org')
), seeded_patients AS (
    SELECT DISTINCT p.id
    FROM public.profiles p
    JOIN public.care_team c ON c.patient_id = p.id
    JOIN public.profiles seed_doctor ON seed_doctor.id = c.doctor_id
    WHERE p.role = 'patient' AND p.auth_user_id IS NULL
      AND lower(p.email) LIKE '%@example.com'
      AND lower(seed_doctor.email) IN ('sarah.chen@example.com', 'james.wilson@example.com')
      AND seed_doctor.license_num LIKE 'DEMO-%'
)
INSERT INTO public.care_team (patient_id, doctor_id)
SELECT p.id, d.id FROM seeded_patients p CROSS JOIN testing_doctor d
ON CONFLICT (patient_id, doctor_id) DO NOTHING;
```

Reload the provider dashboard afterward.

## Sessions and limits

- Sessions are opaque cookies stored in server memory, capped at one hour (Supabase token lifetime). A restart or deploy signs everyone out. One signed-in account per browser; use a private window to test patient and doctor together.
- Live updates use a process-local event hub, so run exactly **one** server instance.
- Existing records reference `/static/ct-scan.svg` and `/mock-scans/...`; keep `static/ct-scan.svg` and `templates/mock_scan.html`.
- Not implemented: password recovery, file storage (uploads keep filenames only), credential verification, care-access removal UI, audit logging. Summaries are rule-based, not medical advice.

## Deploy (Fly.io)

`Dockerfile`, `.dockerignore`, `fly.toml` and `deploy.env` (non-secret settings baked in as the image's `.env`; never put secrets there) are included.

```powershell
fly launch --no-deploy --copy-config
fly secrets set DATABASE_URL='...' NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY='...'
fly deploy
fly scale count 1
```

Apply migrations before deploying, and add the `https://<app>.fly.dev` address to Supabase Auth URL settings.

## Checks

```powershell
go vet ./...
go build
```

There is no automated test suite. After changes, manually check login, onboarding, patient selection, record creation with body areas, timeline filters, uploads, and invitations.
