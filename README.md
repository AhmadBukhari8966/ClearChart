# ClearChart

**A live, shared workspace for doctors and patients.** Patients stop stressing about carrying records between doctors, and doctors stop wondering how their patients are doing or where their reports are.

- **Live app:** [clearchart.fly.dev](https://clearchart.fly.dev) (click **Enter judge mode** on the sign-in page)
- **Source:** [github.com/SULAVSHRESTHA08/ClearChart](https://github.com/SULAVSHRESTHA08/ClearChart)
- Built at **TigerHacks 2026**

---

## The problem

A recovery usually happens in scattered pieces. Lab results live in one place, doctor notes in another, and prescriptions and scans are wherever the patient left them. At every new doctor, the patient starts over: gathering folders, chasing results, and explaining what the last doctor said.

Doctors have the reverse problem. Once a patient goes home, they know very little until the next appointment, often weeks later. They can't tell how recovery is going or whether new reports have arrived, and a referred patient often arrives with only part of their history.

Everyone is working on the same recovery, but nobody has the whole picture.

## What ClearChart does

ClearChart gives each patient **one chart** with their whole medical story. The patient controls who can see it, and every doctor they let in works from the same chart, updated live.

### For patients
- **Everything in one place.** Doctor notes, prescriptions, imaging and reports sit on one timeline that can be filtered by record type and by body area.
- **Nothing to carry.** Add a document once, and every doctor on your care team can see it.
- **Health status updates.** Log pain, mobility and energy on a 1–10 scale, and your doctor sees it right away.
- **A little more clarity.** Google Gemini explains your latest doctor note in plain, everyday language.
- **You decide who gets in.** A doctor can see your chart only after you accept their invitation.

### For doctors
- **Patient dashboard** with search and a full chart for every linked patient.
- **Needs attention.** Patients whose latest check-in shows high pain (6 or more) or low mobility or energy (4 or less) are flagged automatically, with the most severe first.
- **Live care updates.** A note, prescription or imaging record, tagged by body area, appears on the patient's screen instantly, with no refresh.
- **Review before the visit.** Reports and recent check-ins are ready before the appointment starts.

### Continuity and transparency
- **Referrals without the paper trail.** A new doctor sends an invitation. Once the patient accepts, that doctor sees the full history, including everything previous doctors wrote.
- **Nothing hidden.** Patients see everything their care team writes about them. Only doctors the patient approved can read or add to the chart.

## Try it: Judge mode

1. Open [clearchart.fly.dev](https://clearchart.fly.dev) and click **Enter judge mode**.
2. You'll see the **doctor view on the left** and the **patient view on the right**, with an 8-step guided tour.
3. Pick a patient, write a care update and watch it land on the patient's timeline live. Then log a check-in with pain of 7 or more on the patient side and watch that patient jump into the doctor's **Needs attention** list.

In judge mode, a demo doctor ("Dr. Judge Demo") is pre-linked to the demo patients so you can explore without going through the invitation step. Normal accounts always go through patient consent. The demo database contains about **1,300 fictional patients and 46 doctors**, including 1,000 generated patients with coherent recovery stories. No real patient data is used.

## Tech stack

| Layer | Technology |
| --- | --- |
| Backend | Go (`net/http`, `html/template`) with server-rendered HTML |
| Live UI | [Datastar](https://data-star.dev) + server-sent events, with no custom frontend JavaScript for the app itself |
| Styling | Custom CSS + Tailwind CSS (CDN) |
| Database | Supabase PostgreSQL (`lib/pq`) |
| Auth | Supabase Auth (email/password) with opaque server-side sessions |
| AI | Google Gemini API (default `gemini-3.5-flash-lite`), with a rule-based fallback |
| Deploy | Docker, Fly.io, GitHub Actions |

## How it works

- **One query per dashboard.** The dashboard originally took 8–10 separate queries. A single SQL statement (CTEs + `json_agg`) now loads the whole dashboard and checks, in the same query, that the doctor is on that patient's care team.
- **Live updates.** When data changes, an in-process event hub tells every open stream showing that patient. The server then sends fresh HTML fragments, and Datastar patches them into the page.
- **AI without the wait.** Gemini runs in the background. The page loads instantly with a loading state, and the explanation is pushed live once it's ready. Results are cached per note. The app retries once on 429/503 errors, and if Gemini fails it falls back to a rule-based summary. The prompt tells the model to explain only and never to add diagnoses, medications or advice.
- **Security.** Every change checks the user's role, care-team membership, same-origin and a CSRF token. An invitation only works when the patient accepts from the account with the invited email address. Row-level security is on for every table, so the browser cannot query the database directly. Database triggers enforce doctor and patient roles.

## Run locally

Requires Go 1.23+ and a Supabase project.

```bash
git clone https://github.com/SULAVSHRESTHA08/ClearChart.git
cd ClearChart
go mod download
go run .
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080), sign up, confirm your email if asked, then complete a patient or doctor profile.

Always use `go run .` (not `go run main.go`). Templates and CSS are embedded in the binary, so restart after editing them. If port 8080 is busy, set `PORT=8081`.

### Configuration

The server reads a local `.env` if present. Real environment variables take precedence. `.env` is git-ignored, so never commit it.

```dotenv
DATABASE_URL=postgresql://YOUR_DATABASE_CONNECTION?sslmode=require
SUPABASE_URL=https://YOUR_PROJECT_REF.supabase.co
SUPABASE_PUBLISHABLE_KEY=YOUR_PROJECT_PUBLISHABLE_KEY
GEMINI_API_KEY=YOUR_GOOGLE_AI_STUDIO_KEY
HOST=127.0.0.1
PORT=8080
COOKIE_SECURE=false
```

| Setting | Meaning |
| --- | --- |
| `DATABASE_URL` | Required. For hosts without IPv6, use the Supabase **session pooler** (port 5432). |
| `SUPABASE_URL` | Project API URL. Inferred from a standard Supabase database URL when omitted. |
| `SUPABASE_PUBLISHABLE_KEY` | Publishable key for Auth. Aliases: `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY`, `SUPABASE_ANON_KEY`. Never use a service-role key. |
| `GEMINI_API_KEY` | Optional. Enables AI note explanations (note text is sent to Google). Without it, the rule-based summary is used. |
| `GEMINI_MODEL` | Optional. Default `gemini-3.5-flash-lite` (fast, generous free-tier quota). |
| `JUDGE_MODE` | Optional `true`. Adds **Enter judge mode** to the sign-in page. |
| `COOKIE_SECURE` | `true` behind HTTPS, `false` for local HTTP. |
| `HOST`, `PORT` | Defaults `127.0.0.1`, `8080`. Use `HOST=0.0.0.0` in containers. |

In Supabase, enable email/password sign-in and set **Authentication → URL Configuration → Site URL** to your ClearChart address (locally `http://127.0.0.1:8080/login`).

### Database

SQL is never applied automatically.

- **New database:** run [`schema.sql`](schema.sql) in the Supabase SQL editor.
- **Existing database:** apply the additive migrations in order. The server refuses to start until all of them are applied.
  1. [`001_auth_identity.sql`](migrations/001_auth_identity.sql): links Supabase Auth users to profiles
  2. [`002_care_invitations.sql`](migrations/002_care_invitations.sql): doctor-to-patient invitations with patient consent
  3. [`003_record_categories.sql`](migrations/003_record_categories.sql): body-area tags for timeline filters
  4. [`004_notifications_seen.sql`](migrations/004_notifications_seen.sql): doctor notification read marker

New accounts start with no care-team links. Doctors connect to patients through invitations, and the patient must accept from the account whose email was invited.

## Deploy (Fly.io)

`Dockerfile`, `fly.toml` and `deploy.env` are included. `deploy.env` holds **non-secret** settings baked into the image. Never put keys or connection strings there.

```bash
fly launch --no-deploy --copy-config
fly secrets set DATABASE_URL='...' NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY='...' GEMINI_API_KEY='...'
fly deploy
fly scale count 1
```

Run exactly **one** machine, because sessions and the live-update hub are held in memory. Add `https://<app>.fly.dev` to Supabase Auth URL settings. Pushes to `main` deploy automatically through GitHub Actions if the repo has a `FLY_API_TOKEN` secret.

## Project structure

```
main.go            startup, routes, schema checks
auth.go            sessions, sign-in/sign-up
supabase_auth.go   Supabase Auth client
store.go           SQL (single-query dashboard)
handlers.go        dashboards, records, uploads, check-ins
live.go            SSE streams and page patches
ai.go              Gemini explanations (background, cached, fallback)
invitations.go     consent-based care-team invitations
notifications.go   notification panel
directory.go       doctor patient search
categories.go      body-area categories and filters
judge.go           judge mode
perf.go            gzip, static caching
templates/         server-rendered HTML
static/            CSS and images
migrations/        additive SQL migrations
schema.sql         full schema for new databases
```

## Current limitations

- Uploads record the **file name only**, and file contents are not stored yet.
- No password recovery, credential verification, audit logging or UI for removing care access.
- Sessions live in server memory: a restart signs everyone out, and the app must run as one instance.
- Imaging in the demo data uses synthetic scans.
- AI explanations are for understanding only and are **not medical advice**.

## What's next

- Direct lab uploads, so results reach the chart without the patient handling them
- Full file storage for documents
- Recovery trend charts and alerts that notify doctors when a patient is flagged
- FHIR support to connect with hospital record systems

## Checks

```bash
go vet ./...
go build
```

There is no automated test suite yet. After changes, manually check sign-in, onboarding, patient selection, record creation with body areas, timeline filters, uploads, check-ins and invitations.