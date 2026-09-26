# Authentication and database-backed onboarding

This is the current setup for ClearChart in `D:\projects\ClearChart`. Database records remain in Supabase. Supabase Auth now provides email/password identity, and the Go server keeps an opaque application session cookie.

## Upgrade your existing database

Run [migrations/001_auth_identity.sql](../migrations/001_auth_identity.sql) in your Supabase SQL editor:

```sql
BEGIN;
ALTER TABLE public.profiles ADD COLUMN IF NOT EXISTS auth_user_id uuid;
CREATE UNIQUE INDEX IF NOT EXISTS profiles_auth_user_id_unique
    ON public.profiles (auth_user_id);
COMMIT;
```

The migration is additive and safe to rerun. It preserves profile IDs, medical records, scan URLs, and care-team links. Existing synthetic profiles have `auth_user_id = NULL`; a unique index permits multiple unclaimed profiles but only one profile for a particular Auth user. New installations get the same column/index from `schema.sql`.

`profiles.id` remains the application's database-generated profile ID. `profiles.auth_user_id` is a different UUID supplied by verified Supabase Auth. The server uses this mapping to find your profile after login. It never treats a submitted email address as proof that you own an existing profile.

The application does not run migrations or seeds on startup. You do not need to rerun either seed script to upgrade an already populated database.

## Configure the Go server

Keep your working database connection in `.env`; add the Auth configuration from the same Supabase project. The server loads `.env` when it exists without overriding variables already set in its process.

| Setting | Meaning |
| --- | --- |
| `DATABASE_URL` | Required lib/pq PostgreSQL connection URL; retain your working Supabase connection and SSL settings. |
| `SUPABASE_URL` | Project API URL, e.g. `https://PROJECT_REF.supabase.co`. It can be inferred from standard direct host `db.PROJECT_REF.supabase.co` or pooler username `postgres.PROJECT_REF`; specify it for other formats. |
| `SUPABASE_PUBLISHABLE_KEY` | Project publishable key for Auth calls. Aliases: `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY`, then `SUPABASE_ANON_KEY`. |
| `COOKIE_SECURE` | Set `true` behind an HTTPS reverse proxy so the application cookie is sent only over HTTPS. Use `false` for plain HTTP local development. |
| `HOST`, `PORT` | Defaults: `127.0.0.1`, `8080`. Set according to your hosting/network setup. |

These values configure the Go process. Browser templates receive no database credentials, Auth API key, or Supabase access/refresh tokens. Do not substitute a service-role/secret key for the publishable/anon key. Keep `.env` out of source control; the repository's ignore rules already exclude it.

Enable email/password authentication in your Supabase project. Configure its email confirmation redirect/site URL for your ClearChart address. When confirmation is enabled, the user confirms their email and then signs in with their password; ClearChart does not consume browser-fragment tokens or depend on custom JavaScript to finish login.

## Request and session flow

1. `GET /` sends signed-out visitors to `/login`. `/signup` creates an account through Supabase Auth.
2. Login/signup submit ordinary POST forms with CSRF protection. Go sends credentials to Supabase Auth over HTTPS and does not store the password in the profile table.
3. A verified sign-in produces a new opaque application cookie. The server stores the Auth user identity and expiry in its in-memory session map.
4. The server finds `ProfileByAuthUserID`. An existing profile enters its own dashboard; an account without a profile continues to `/onboard/patient` and can choose patient/provider.
5. Onboarding validates role-specific fields. It takes the email and Auth ID from the session, saves a profile through PostgreSQL defaults, and returns Datastar feedback with a workspace link.
6. Reads and writes still check the session, requested profile, CSRF/origin where applicable, and care-team relationship. Changing an ID in a URL or form does not grant access.
7. Logout is POST-only, validates CSRF, invalidates the local application session, and clears its cookie.

There is one signed-in account per browser cookie store. For patient/provider interaction tests, use two browser profiles or a normal and private window. Login no longer creates separate patient and doctor cookies.

Application sessions last no longer than the Supabase token lifetime, capped at one hour. The server currently has no refresh-token flow or shared session store. Expiration or a server restart requires signing in again. A single running Go process owns its own sessions and notification hub; multiple server instances need shared session infrastructure and a cross-process change mechanism.

## Connect your account to the existing synthetic patients

New providers correctly start with an empty list. Existing seed doctors and patients are application data, not Supabase login accounts. No new account automatically gains access to those charts.

For the existing 303-patient scroll test:

1. Create and verify **your own** account, then finish provider onboarding.
2. In the SQL below, replace `your-doctor-email@example.org` with that account's email.
3. Run it as an administrator in the Supabase SQL editor, then reload the provider dashboard.

```sql
-- Explicitly grant one signed-in provider access to the seeded test cohort.
-- Existing care teams and record authors remain unchanged.
WITH testing_doctor AS (
    SELECT id FROM public.profiles
    WHERE role = 'doctor'
      AND auth_user_id IS NOT NULL
      AND lower(email) = lower('your-doctor-email@example.org')
), seeded_patients AS (
    SELECT DISTINCT p.id
    FROM public.profiles p
    JOIN public.care_team c ON c.patient_id = p.id
    JOIN public.profiles seed_doctor ON seed_doctor.id = c.doctor_id
    WHERE p.role = 'patient'
      AND p.auth_user_id IS NULL
      AND lower(p.email) LIKE '%@example.com'
      AND lower(seed_doctor.email) IN (
          'sarah.chen@example.com', 'james.wilson@example.com'
      )
      AND seed_doctor.license_num LIKE 'DEMO-%'
)
INSERT INTO public.care_team (patient_id, doctor_id)
SELECT p.id, d.id FROM seeded_patients p CROSS JOIN testing_doctor d
ON CONFLICT (patient_id, doctor_id) DO NOTHING;
```

This is a deliberate administrator action for your synthetic cohort. It does not add a general permission rule or grant future signups access. If it inserts zero rows, verify the doctor's email, completed onboarding, and that the seed rows are present. The directory is rendered at page load; reload after assigning links.

For a new patient and doctor account that you control, assign their relationship explicitly:

```sql
INSERT INTO public.care_team (patient_id, doctor_id)
SELECT p.id, d.id
FROM public.profiles p CROSS JOIN public.profiles d
WHERE p.role = 'patient' AND d.role = 'doctor'
  AND p.auth_user_id IS NOT NULL AND d.auth_user_id IS NOT NULL
  AND lower(p.email) = lower('your-patient-email@example.org')
  AND lower(d.email) = lower('your-doctor-email@example.org')
ON CONFLICT (patient_id, doctor_id) DO NOTHING;
```

Then sign in separately as each account, post a care update, and observe the patient's timeline update. Upload a report from the patient to verify its filename appears in the provider reports center. File contents are still discarded; this migration adds authentication and persistence, not object storage.

Do not sign up using a seeded patient's email to claim their chart. Mapping an existing seeded profile to an Auth user is an explicit administrative operation and is not exposed by the normal signup flow. A conflicting profile email is reported as a conflict rather than silently transferring records.

## What was removed and what remains

| Item | Status |
| --- | --- |
| Runtime in-memory store and Go bulk seeding | Removed from the server; fixtures live only in `_test.go` files. |
| `/demo/patient`, `/demo/doctor`, demo profile switcher | Removed. Signed-out access goes through login. |
| Automatic seed-doctor/patient care links at onboarding | Removed. Relationships are explicit. |
| Existing Supabase synthetic rows | Preserved, including IDs and image references. |
| `seed.sql`, `seed_large.sql` | Optional database fixtures, retained for reproducible testing. |
| `mock_scans.go`, scan template and static scan image | Retained because existing medical records use those URLs. |
| Datastar forms, patient selection, SSE hub | Retained with authenticated access. |
| Rule-based summary, sample watch chart, filename-only uploads | Retained and labeled according to what they actually do. |

## Checks and operational limits

Run `go test ./...` and `go vet ./...`. Authentication tests use an isolated fake provider; they do not send confirmation emails or register users in your live project. PostgreSQL integration tests require an explicit local test connection and create/drop an isolated database; the normal test suite does not mutate your configured Supabase database.

The clinical tables still use a server database connection and RLS without browser access policies. The Go authorization checks protect application requests; this change does not switch the application to direct browser SQL/REST access. A provider's self-entered license number is not independently verified. Password recovery, care-team administration UI, audit logging, durable sessions, and actual file storage remain separate features to implement.

## Authentication code reference

- **auth.go**: `AuthPage` supplies the login/signup template. `authConfiguration` reads configuration or derives the project URL from a recognized Supabase DSN. `cookie` applies HTTP-only, SameSite and HTTPS flags. `issueSession` rotates opaque tokens and CSRF values; `currentSession` rejects expired tokens; `sessionFor` requires a completed profile and matching role. `sessionDestination` chooses onboarding or the user's dashboard. `home`, `authForm`, and `authPage` route and render entry pages. `login` and `signup` call `authenticate`, which validates form/CSRF, calls the provider, resolves the verified Auth ID to a profile, and redirects. `logout` invalidates the local token.
- **supabase_auth.go**: `AuthProvider` is the replaceable authentication boundary used by tests. `AuthResult` holds verified identity and expiry without retaining provider tokens. `supabaseAuth` contains the private HTTPS client, project URL, and key; `NewSupabaseAuth` validates configuration and bounds requests. `SignIn`/`SignUp` select API endpoints; `authenticate` validates responses, confirmation, identity, and expiry. `supabaseAuthUser` and `supabaseAuthResponse` decode provider JSON. `authResponseError` translates safe provider error codes into sentinel errors; `validAuthEmail`/`validAuthUserID` reject malformed identity data.
- **models.go / store.go**: `Profile.AuthUserID` maps a verified Supabase user to a distinct database-generated profile ID. `ProfileByAuthUserID` resolves that mapping. `CreateProfile` requires the mapping and no longer creates care-team relationships.
- **handlers.go / live.go**: onboarding requires an authenticated pending profile, trusts the session's email/identity, and rotates the session after profile creation. Existing dashboard/write authorization remains role/care-team scoped. Live streams recheck the current session before subsequent snapshots, so logout prevents further updates.

Protocol reference: [Supabase Auth API](https://github.com/supabase/auth/blob/master/openapi.yaml).

## Verification on September 26, 2026

The additive migration was applied to the configured Supabase database, preserving its five profiles, 13 records, and four care links. The previously requested large seed was then imported: the database contained **303 patients, two doctors, 1,513 records and 603 image references** afterward. Existing rows were preserved; no Auth accounts or automatic access links were created.

The project's Auth settings were reachable with the configured publishable key: email/password and signup were enabled, with email confirmation required. Complete real email confirmation through your own account; automated checks did not register users or send emails. In Supabase Authentication URL Configuration, set the Site URL to your actual app address (for local development, `http://127.0.0.1:8080/login`) before trying the confirmation link.

Unit tests, Go vet/build, and isolated PostgreSQL tests passed. The database tests cover generated IDs, repeatable seeds, identity uniqueness, no automatic care links, and migration preservation. Browser checks against the connected server verified login redirects, removed demo routes, and desktop/mobile auth layouts.
