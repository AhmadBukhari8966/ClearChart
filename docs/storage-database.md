# ClearChart: models, storage, and a persistent database

This reference describes the code in `D:\projects\ClearChart` as inspected on September 26, 2026. Start with the project's main walkthrough for request handling; this document explains everything declared in `models.go` and `store.go`, the SQL files, and how to enable persistence. No database was provisioned or modified while writing it.

## 1. The database integration already exists

You do not need to rewrite the backend to connect to a real database. `main()` selects one of two implementations of `Store`:

```mermaid
flowchart TD
    Main["main() reads DATABASE_URL"] --> Choice{"Nonempty?"}
    Choice -->|No| Memory["NewMemoryStore(): seed Go maps and slices"]
    Choice -->|Yes| Postgres["NewPostgresStore(): sql.Open + PingContext"]
    Postgres -->|Connection fails| Stop["Exit with a generic error; no silent fallback"]
    Postgres -->|Connection succeeds| Shared["newApp(store, mode)"]
    Memory --> Shared
    Shared --> Handler["HTTP handlers call Store methods"]
    Handler --> Results["Go structs -> html/template -> HTML/SSE"]
```

An **interface** is a list of operations a type promises to implement. `memoryStore` and `postgresStore` both implement the same methods, so handlers do not need separate memory and SQL versions. Go checks that the returned implementation satisfies the `Store` interface when it is assigned or returned as a `Store`.

`NewMemoryStore()` is an explicitly selected demo mode, not a fallback after a database failure. Changes live only in that Go process. `NewPostgresStore()` stores application data in PostgreSQL; restarting Go does not erase those rows. Browser sessions and the live-update hub remain in Go memory in either mode.

`Supabase connected` is the label the current `main()` uses for **any** nonempty working `DATABASE_URL`, including local PostgreSQL. It is not proof that Supabase Auth, Storage, Realtime, or Apple Watch integration is enabled. The only Supabase capability used by the app is its PostgreSQL server.

## 2. Every declaration in `models.go`

### Constants

| Constant | Value and purpose |
| --- | --- |
| `demoDoctorEmail` | `sarah.chen@example.com`. Default doctor fixture identity for demo login and automatic demo onboarding links in both stores. |
| `demoPatientEmail` | `alex.morgan@example.com`. Default patient fixture identity for demo login and automatic demo onboarding links in both stores. |
| `demoDoctorID` | `00000000-0000-4000-8000-000000000001`. Deterministic in-memory fixture ID for Dr. Sarah Chen; used by memory fixtures/tests, not as a required PostgreSQL ID. |
| `demoPatientID` | `00000000-0000-4000-8000-000000000101`. Deterministic in-memory fixture ID for Alex Morgan; used by memory fixtures/tests, not as a required PostgreSQL ID. |

These are public memory-fixture identifiers, not passwords or proof of identity. Default demo login resolves `sarah.chen@example.com` / `alex.morgan@example.com` by case-insensitive email. An explicit demo `?id=` still selects an account by its actual saved ID. PostgreSQL generates new profile IDs; an existing matching-email profile keeps its existing ID when seeds are rerun.

### `Profile`: one doctor or patient

All fields are Go `string` values. SQL stores some as `uuid` or `date`; the store translates them to strings for templates and forms.

| Field | Meaning and reason |
| --- | --- |
| `ID` | Unique profile identifier used by URLs, sessions, and foreign keys. |
| `Role` | `doctor` or `patient`; chooses permissions, onboarding requirements, and dashboard. |
| `Name` | Display name in headings, care-team lists, and authored records. |
| `Email` | Contact/registration value; uniqueness is checked case-insensitively. It is not a verified login credential. |
| `LicenseNum` | Doctor onboarding field; currently recorded as text without license verification. |
| `Specialization` | Doctor onboarding field displayed in the care team/profile. |
| `DOB` | Patient date of birth, formatted `YYYY-MM-DD`. A string avoids date parsing inside templates. |
| `BloodType` | Patient onboarding value, such as `O+` or `Unknown`. |

This single struct keeps the prototype simple. Empty doctor-only fields on patients, and empty patient-only fields on doctors, are expected. SQL nullable values become empty strings when read.

### `Record`: one timeline entry

| Field | Meaning and reason |
| --- | --- |
| `ID` | Unique entry ID; also supplies stable IDs to rendered timeline items. |
| `PatientID` | Whose chart contains the entry. |
| `DoctorID` | Which doctor authored it. |
| `Type` | `note`, `prescription`, or `imaging`; controls icon, label, filtering, and optional image display. |
| `Content` | Text of the note or simulated prescription/imaging description. |
| `ImageURL` | Optional URL/path of the illustration; no image bytes are held here. |
| `DoctorName` | Display data supplied by the store. SQL obtains it with a profile join; it is not a separate medical-record column. |
| `PatientName` | Display data supplied by the store, also not a separate medical-record column. |
| `Timestamp` | `time.Time`; used for chronological ordering and formatted dates. |

Names are copied into in-memory records when created. SQL reads current profile names with joins. If profile editing is added later, these implementations will differ unless memory reads also refresh names.

### `Upload`: metadata for one reported file

| Field | Meaning and reason |
| --- | --- |
| `ID` | Unique metadata-row ID and stable fragment identity. |
| `PatientID` | Patient who shared the report name. |
| `FileName` | Sanitized filename shown to patient and care team. There is no file object, storage key, download route, or binary data. |
| `PatientName` | Display name added by the store, not a separate SQL column. |
| `Timestamp` | `time.Time`; report ordering and display date. |

### `Healing`: one self-reported measurement

| Field | Meaning and reason |
| --- | --- |
| `ID` | Unique measurement ID. |
| `PatientID` | Patient reporting the measurement. |
| `StatusType` | `pain`, `mobility`, or `energy`. |
| `Value` | Integer from 1 to 10. Lower pain is better; higher mobility/energy is better. It is not a clinician assessment. |
| `Timestamp` | `time.Time`; lets the store choose the newest value of each type while retaining history. |

### `Biometric`: the latest synthetic doctor wearable reading

| Field | Meaning and reason |
| --- | --- |
| `DoctorID` | Doctor whose personal mock data is displayed. |
| `HeartRate` | Integer heart rate for the visual card. |
| `SleepHours` | `float64`, allowing values such as `7.6`. SQL stores `numeric(4,2)`. |
| `Timestamp` | `time.Time` of the reading. |

SQL also has an `id` for each biometric row, but this Go view struct omits it because the dashboard only needs the latest reading. There is no wearable ingestion method or endpoint.

### `Dashboard`: assembled page data, not a database table

`dashboardData()` in `handlers.go` combines multiple store calls into this view model. Templates receive one consistent object instead of issuing database queries themselves.

| Field | Type | Meaning |
| --- | --- | --- |
| `Profile` | `Profile` | The signed-in demo profile. For a doctor this differs from the selected patient. |
| `Patient` | `Profile` | Patient whose chart is currently selected. Empty when a doctor has no linked patients. |
| `Patients` | `[]Profile` | All patients linked to the current doctor, for the directory. |
| `Doctors` | `[]Profile` | Care-team doctors for the selected patient. |
| `Records` | `[]Record` | Selected patient's timeline, filtered when a record-type filter is active. |
| `Uploads` | `[]Upload` | Upload metadata for the selected patient. |
| `Reports` | `[]Upload` | Upload metadata across **all** patients linked to the current doctor. It does not narrow to the selected chart. |
| `Healing` | `[]Healing` | Latest pain, mobility, and energy entries. |
| `Biometric` | `Biometric` | Current doctor's most recent mock reading; zero value when none exists. |
| `CSRF` | `string` | Session-specific token copied into forms to authorize writes. |
| `Mode` | `string` | Human-readable storage mode label. |
| `Filter` | `string` | Selected timeline type or empty string for all types. |
| `Summary` | `string` | Preset/rule-based plain-language note summary. |
| `Role` | `string` | `doctor` or `patient`; chooses template behavior. |
| `ViewID` | `string` | Identifier for the page/view; live-update code uses it to recognize an update originating from this view. It is not authorization. |
| `RecordCount` | `int` | Count of all selected-patient records before filtering. |
| `UploadCount` | `int` | Count of uploads for the selected patient, not all doctor reports. |
| `Today` | `time.Time` | Current time for the page header. |

`[]T` means a slice, Go's growable sequence of values of type `T`. A zero-value struct contains empty strings, zero numbers, and zero times; the code/templates must decide how to display missing data.

### `Store`: every method and its contract

All data operations take `context.Context` so cancellation/deadlines can reach storage. The context is generally the HTTP request's context. A return of `(value, error)` means callers must check the error before using the value.

| Method | Input and return | Why it exists |
| --- | --- | --- |
| `Profile(ctx, id)` | Profile ID -> one `Profile` | Resolve an account/session/selected chart. Missing ID returns `ErrNotFound`. |
| `ProfileByEmail(ctx, email)` | Email -> one `Profile` | Case-insensitive lookup for default demo entry, independent of database-generated IDs. Missing email returns `ErrNotFound`. |
| `Profiles(ctx, role)` | Role -> `[]Profile` | Obtain all profiles of a role, sorted by name. This is an internal query, not a permission-filtered doctor directory. |
| `CareTeam(ctx, patient)` | Patient ID -> `[]Profile` | Show doctors linked to one patient. |
| `Patients(ctx, doctor)` | Doctor ID -> `[]Profile` | Build that doctor's permitted patient directory. |
| `Records(ctx, patient)` | Patient ID -> `[]Record` | Get the complete newest-first timeline; handler applies type filtering. |
| `Uploads(ctx, patient)` | Patient ID -> `[]Upload` | Get one patient's newest-first report metadata. |
| `Reports(ctx, doctor)` | Doctor ID -> `[]Upload` | Get reports across the doctor's care relationships in one operation, avoiding one query per patient. |
| `Healing(ctx, patient)` | Patient ID -> `[]Healing` | Get only the newest entry per status type, sorted by type. |
| `Biometrics(ctx, doctor)` | Doctor ID -> `Biometric` | Get the most recent wearable fixture; missing data returns `ErrNotFound`. |
| `CreateProfile(ctx, p)` | Unpersisted `Profile` -> saved `Profile` | Register an account and return its generated ID; memory uses Go `newID`, PostgreSQL uses its column default. Attach a seeded demo counterpart when present. |
| `AddRecord(ctx, r)` | New `Record` -> saved `Record` | Authorize care-team relationship and insert a timeline entry with generated ID/time and names. |
| `AddUpload(ctx, u)` | New `Upload` -> saved `Upload` | Insert filename metadata for an existing patient. |
| `AddHealing(ctx, h)` | New `Healing` -> saved `Healing` | Append a self-reported score, preserving past measurements. |
| `IsCareTeam(ctx, patient, doctor)` | Two IDs -> `bool` | Explicit read-access/authoring relationship check; the argument order is patient first. |
| `Close()` | No input -> `error` | Release storage resources on server shutdown. |

The interface does not contain delete, edit, file download/storage, care-team management, biometric ingestion, search pagination, account verification, or session persistence. Adding one of those requires deliberate new behavior rather than assuming it already exists.

### `newID()`

It reads 16 cryptographically random bytes with `crypto/rand`, sets the UUID version-4 and variant bits, and formats the bytes as a UUID string. New in-memory profiles/records/uploads/healing rows use this helper; sessions, CSRF tokens, and view identifiers use it in either storage mode. It panics if secure randomness is unavailable because an identifier generator must not quietly fall back to predictable values. Deterministic memory fixture IDs are constructed separately. PostgreSQL row inserts, including application writes and SQL seeds, omit primary-key IDs and use the schema's `gen_random_uuid()` defaults. The SQL store reads the assigned ID back with `RETURNING`; Go does not choose those database row IDs.

## 3. Every declaration in `store.go`

### Shared error values

| Variable | Meaning |
| --- | --- |
| `ErrNotFound` | Requested profile/row is absent. |
| `ErrForbidden` | Requested doctor is not linked to that patient's care team. |
| `ErrConflict` | An email already belongs to a profile. |

These are sentinel `error` values so handlers can use `errors.Is` instead of parsing database messages. Not every database error becomes one of these; unexpected SQL/connection/constraint errors propagate to the caller.

### `memoryStore` and its constructor

| Field | Type and purpose |
| --- | --- |
| `mu` | `sync.RWMutex`. Allows concurrent readers and exclusive writers so HTTP requests do not race while accessing Go maps/slices. |
| `profiles` | `map[string]Profile`, keyed by profile ID. |
| `care` | `map[string]map[string]bool`: patient ID -> doctor ID -> membership. Looking up a missing entry returns false. |
| `records` | `[]Record`, containing all patients' timeline entries. |
| `uploads` | `[]Upload`, containing all patients' filename metadata. |
| `healing` | `[]Healing`, retaining all historical score entries. |
| `biometrics` | `map[string]Biometric`, one latest fixture per doctor. |

`NewMemoryStore()` allocates the maps, captures a UTC reference time, inserts two doctors and three patients, their relationships, 11 records, four uploads, 12 scores, and two biometric readings. It then calls `addBulkPatients(s, now)` from `bulk_seed.go`, adding the larger directory fixture, and returns the object as `Store`.

Its local `seedRecord` struct is just a compact initializer: `patient`, `doctor`, `kind`, `content`, and `image` are strings; `hours` is how far before `now` to date the record. The anonymous upload initializer has `patient`, `name`, and `hours`. The anonymous healing initializer has `patient`, `kind`, `value`, and `hours`. They are local construction helpers, not runtime domain types or SQL tables. The `sort.Slice` anonymous functions below are comparators: name/type ascending or timestamp descending.

### Every `memoryStore` method

Each method except `Close` checks `ctx.Err()` first. That catches a context already canceled when the operation starts; the in-memory loops do not repeatedly check cancellation. Read methods use `RLock`/`RUnlock`. Writes use `Lock`/`Unlock`; `defer` ensures unlock even if an early error occurs.

| Method | What the implementation actually does |
| --- | --- |
| `Close` | Returns `nil`. There is no disk connection or file handle to close. |
| `Profile` | Looks up `profiles[id]`; returns a value copy or `ErrNotFound`. |
| `ProfileByEmail` | Scans profiles under a read lock, uses `strings.EqualFold` for case-insensitive matching, returns the matching profile or `ErrNotFound`, and respects context cancellation. |
| `Profiles` | Loops over all profiles, retains the requested role, sorts by `Name`. |
| `CareTeam` | Iterates the doctor's IDs in `care[patient]`, resolves their profiles, sorts by `Name`. |
| `Patients` | Iterates all care maps, retains patients containing this doctor's ID, sorts by `Name`. |
| `Records` | Scans all records, copies matching patient's values to a new slice, sorts newest first. |
| `Uploads` | Scans all uploads, copies matching patient's values, sorts newest first. |
| `Reports` | Scans uploads once; includes a row if `care[u.PatientID][doctor]` is true. Sorts newest first. |
| `Healing` | Scans all scores, keeps the latest timestamp for each `StatusType` in a temporary map, then returns those entries sorted by type. |
| `Biometrics` | Looks up `biometrics[doctor]`, or returns `ErrNotFound`. |
| `IsCareTeam` | Returns the nested membership lookup. A nonexistent relationship is false, not an error. |
| `CreateProfile` | Rejects an email matching any existing email under `strings.EqualFold`, rejects unknown roles, assigns `newID`, saves the profile. Finds the appropriate counterpart by case-insensitive demo email and role, then links using its actual stored ID. It initializes a missing patient care map before adding the doctor. If no counterpart exists, profile creation succeeds without that link. |
| `AddRecord` | Under the same write lock, verifies membership, checks allowed record type, assigns ID/UTC time, resolves both names, and appends the record. |
| `AddUpload` | Verifies that `PatientID` exists with patient role, assigns ID/UTC time and name, then appends the metadata. |
| `AddHealing` | Verifies patient role and value 1-10, assigns ID/UTC time, then appends the score. |

Returned slices are newly assembled; callers do not receive the store's internal slice to append to. Each element is a value struct, so ordinary field edits on returned data do not mutate storage.

Memory validation is intentionally lighter than the SQL schema: for example, `AddHealing` does not itself reject an unknown status type, and `CreateProfile` does not itself enforce all onboarding field lengths. Current HTTP handlers perform those checks first. A new direct caller should not assume complete validation happens in the memory store. SQL adds another validation layer.

### `postgresStore`, pool setup, and conversion helpers

`postgresStore` has one field, `db *sql.DB`. This is a concurrent connection-pool handle, not a single connection permanently occupied by one user. Query results must be closed so their connections can be reused. See [Go's connection-management documentation](https://go.dev/doc/database/manage-connections).

| Declaration | What it does and why |
| --- | --- |
| `NewPostgresStore(ctx, dsn)` | Calls `sql.Open("postgres", dsn)` using the registered `lib/pq` driver; sets maximum 10 open connections, five idle connections, and a 30-minute maximum connection lifetime. Calls `PingContext` to actually test connectivity; on failure closes the pool and returns an error. Returns `*postgresStore` as `Store` on success. `main()` supplies a 10-second connection context. |
| `postgresStore.Close()` | Calls `db.Close()` to release the pool at shutdown. |
| `storageError(err)` | Maps `sql.ErrNoRows` to `ErrNotFound`. Maps PostgreSQL SQLSTATE `23505` (unique violation) to `ErrConflict` only when the constraint name contains `email`. All other errors, including nil, pass through. Renaming the email index without `email` would affect that mapping. |
| `rowScanner` | An interface containing `Scan(...any) error`. Both a single `*sql.Row` and an iterated `*sql.Rows` can satisfy it, letting one decoding helper support either query style. `...any` means a variable number of destination arguments. |
| `profileColumns` | Fixed SQL projection in the exact order expected by `scanProfile`. Converts UUID to text, nullable text to empty strings with `COALESCE`, and DOB to `YYYY-MM-DD`. The table alias must be `p`. |
| `scanProfile(row)` | Scans eight projected columns into a `Profile` and passes the scan error through `storageError`. |
| `queryProfiles(ctx, query, arg)` | Runs a parameterized profile query with one argument, defers closing rows, scans each row, and checks `rows.Err()` after iteration. Reuses identical iteration/error handling for role lists and relationship lists. |
| `scanRecord(row)` | Scans the nine projected columns, including joined display names, into a `Record`, then normalizes the error. |
| `recordColumns` | Fixed projection used by `scanRecord`. Expects aliases `r` for records, `d` for doctor profile, and `p` for patient profile. Converts nullable image URL to an empty string. |
| `scanUpload(row)` | Scans ID, patient ID, filename, timestamp, and joined patient name into `Upload`. |
| `scanHealing(row)` | Scans ID, patient ID, status, value, and timestamp into `Healing`. |

`$1`, `$2`, and so on are SQL parameter positions. Values are passed separately to the driver rather than pasted into SQL. Concatenations such as `"SELECT " + profileColumns` add a fixed code constant, not user input. If you add user-controlled sorting, table names, or filters later, keep them out of SQL string concatenation.

### Every `postgresStore` data method

| Method | SQL behavior and reason |
| --- | --- |
| `Profile` | One-row lookup by profile ID using `QueryRowContext` and `scanProfile`. |
| `ProfileByEmail` | One-row lookup with `lower(p.email)=lower($1)`, using `QueryRowContext` and `scanProfile`. Matches the case-insensitive unique-email index and lets default demo profiles have database-generated IDs. |
| `Profiles` | Uses `queryProfiles` with `WHERE p.role=$1 ORDER BY p.name`. |
| `CareTeam` | Joins `profiles` to `care_team` through `doctor_id`, filters by patient ID, orders by doctor name. |
| `Patients` | Joins through `patient_id`, filters by doctor ID, orders by patient name. |
| `Records` | Joins doctor and patient profiles for names, filters by patient ID, orders by timestamp descending and then ID descending. Iterates, closes rows, and checks terminal errors. |
| `Uploads` | Joins patient profile for its name, filters by patient ID, orders by timestamp and ID descending. |
| `Reports` | Joins uploads, patient profiles, and care-team links, filters by doctor ID, orders by timestamp and ID descending. One query serves the entire reports center. |
| `Healing` | Uses PostgreSQL `DISTINCT ON (status_type)` with `ORDER BY status_type,timestamp DESC,id DESC`. The first row per type is the latest one, and the returned list is ordered by type. Historical rows remain in the table. |
| `Biometrics` | Reads one doctor's newest row with `ORDER BY timestamp DESC LIMIT 1`; there is no ID tie-breaker here. A missing reading returns `ErrNotFound`. |
| `IsCareTeam` | Uses `SELECT EXISTS(...)` for a single boolean without fetching a whole profile/list. |
| `CreateProfile` | Begins a transaction, inserts the profile without an `id` column, reads its database-generated ID with `RETURNING`, converts empty optional strings to SQL `NULL`, and casts DOB to `date`. Inserts a care link to the appropriate seeded counterpart by case-insensitive default email **only if that profile exists with the correct role**. Commits both changes together. Deferred rollback handles any earlier error; rollback after successful commit is harmless. Missing counterpart causes zero link rows, not an error. |
| `AddRecord` | One SQL statement uses a CTE (`WITH inserted AS (...)`). `INSERT ... SELECT` selects only a matching care-team pair, then `RETURNING` and joins provide the saved row and names. If no relationship exists, no row is inserted and `ErrNotFound` is translated to `ErrForbidden`. Checking membership and inserting in one statement avoids a separate application-level check/write gap; the composite foreign key also preserves relationship integrity. |
| `AddUpload` | A CTE inserts only if the referenced profile exists with patient role, then returns metadata and patient name. No selected patient means `ErrNotFound`. |
| `AddHealing` | `INSERT ... SELECT` inserts only for a patient profile and returns the new score. The database checks the status and 1-10 range. Missing patient becomes `ErrNotFound`. |

All four SQL write methods omit the primary-key `id`, use the database UUID default, and return the assigned ID. Record/upload/healing queries also use the SQL `timestamp DEFAULT current_timestamp` for new times; the in-memory version uses `time.Now().UTC()`. PostgreSQL timestamps represent instants, but formatted display can depend on the connection's time zone. No application timezone preference exists yet.

Memory sorts ties only by timestamp; PostgreSQL records/uploads/healing use an additional ID tie-breaker. Name ordering can also differ because Go compares strings while PostgreSQL uses database collation. Do not depend on identical order when names/timestamps tie.

The live SSE connection does not reserve a database connection for its whole lifetime. The handler runs queries for each snapshot and releases query rows; the HTTP stream remains open separately. Many clients still create recurring database work because snapshots periodically reload data. Indexes and a pool help, but pagination and query/refresh optimization are future scaling work.

## 4. What `schema.sql` actually creates

All six tables are in `public`. In PostgreSQL these unquoted lowercase names are different from creating quoted names such as `"Medical_Records"`; use the existing lowercase names in new queries.

```mermaid
erDiagram
    profiles ||--o{ care_team : "patient_id and doctor_id roles"
    profiles ||--o{ medical_records : "patient and author"
    care_team ||--o{ medical_records : "composite patient/doctor FK"
    profiles ||--o{ patient_uploads : "patient_id"
    profiles ||--o{ healing_progress : "patient_id"
    profiles ||--o{ mock_biometric_data : "doctor_id"
```

| Table | Columns, constraints, and consequences |
| --- | --- |
| `profiles` | UUID primary key defaulting to `gen_random_uuid()`; role restricted to doctor/patient; trimmed name length 2-120; email length 3-254. Doctor requires nonblank `license_num` and `specialization`; patient requires `dob` and `blood_type`. Blood type is one of eight ABO/Rh values or `Unknown`. No email verification, license verification, or link to `auth.users` exists. |
| `care_team` | `patient_id` and `doctor_id` both reference profiles with `ON DELETE CASCADE`; together form the primary key. They cannot be the same ID. Role triggers ensure the references mean what their names say. |
| `medical_records` | UUID ID, patient/doctor IDs, constrained type, nonblank content up to 10,000 characters, `timestamptz` default, optional image URL. Foreign keys reference both profiles and the **care-team pair**. Consequently an author must be linked, and an existing relationship with records cannot simply be deleted. Profile references here do not cascade. |
| `patient_uploads` | UUID ID, patient ID with cascade on patient deletion, nonblank filename up to 255 characters, timestamp default. A table comment explicitly says only metadata is stored. |
| `healing_progress` | UUID ID, patient ID with cascade, status restricted to pain/mobility/energy, integer value 1-10, timestamp default. A column comment explains opposite directions for pain versus other scores. |
| `mock_biometric_data` | UUID ID, doctor ID with cascade, heart rate 30-220, sleep `numeric(4,2)` from 0-24, timestamp default. A comment identifies the data as synthetic. |

The 10,000-character SQL record limit is broader than the current 5,000-character form/handler limit. Database rules are additional integrity checks, not a complete restatement of every HTTP validation rule. For example, SQL DOB is required but the schema alone does not forbid a future DOB.

Deleting a profile is not a general cleanup shortcut. Medical records can block profile and care-team deletion; cascades on other tables do not override those constraints. This design avoids silently orphaning authorship in the prototype. A real record retention/amendment design must be deliberate.

### Every explicit index

Primary keys create their own indexes. The additional indexes are:

| Index | Purpose |
| --- | --- |
| `profiles_email_unique` on `lower(email)` | Case-insensitive email uniqueness. This is also the constraint name recognized by `storageError`. |
| `profiles_role_idx` on role | Supports role lists. |
| `care_team_doctor_idx` on doctor ID | Finds a doctor's patients; the composite primary key begins with patient ID and already helps patient-to-doctor lookup. |
| `medical_records_patient_time_idx` on patient ID, timestamp descending | Supports patient timeline lookup/order. |
| `medical_records_doctor_idx` on doctor ID | Supports author lookup/foreign-key-related operations. |
| `patient_uploads_patient_time_idx` on patient ID, timestamp descending | Supports patient uploads and reports joins. |
| `healing_progress_latest_idx` on patient ID, status type, timestamp descending | Helps latest-score-per-type reads. |
| `mock_biometric_data_doctor_time_idx` on doctor ID, timestamp descending | Helps newest reading lookup. |

### Every SQL function and trigger

`clearchart_check_profile_role()` is a reusable trigger function. It reads the column named by `TG_ARGV[0]` from the incoming `NEW` row, looks for a profile with role `TG_ARGV[1]`, raises SQLSTATE `23514` if it cannot find one, and otherwise returns `NEW` so the insert/update proceeds. `to_jsonb(NEW)` permits one function to inspect different foreign-key column names. The fixed `search_path` controls object lookup.

`clearchart_keep_profile_role()` compares `NEW.role` with `OLD.role`. A change raises `23514`; unchanged role returns `NEW`. This prevents existing patient/doctor references becoming semantically invalid after a role edit.

| Trigger | When it runs and what it checks |
| --- | --- |
| `clearchart_profile_role_immutable` | Before a profile role update; calls `clearchart_keep_profile_role`. |
| `clearchart_care_patient_role` | Before a care-team insert/update; referenced patient must have patient role. |
| `clearchart_care_doctor_role` | Before a care-team insert/update; referenced doctor must have doctor role. |
| `clearchart_upload_patient_role` | Before upload insert/update; owner must be a patient. |
| `clearchart_healing_patient_role` | Before healing insert/update; owner must be a patient. |
| `clearchart_biometric_doctor_role` | Before biometric insert/update; owner must be a doctor. |

The anonymous `DO` block in `schema.sql` checks `pg_trigger` before creating these triggers, avoiding duplicates when rerun. Medical-record roles are enforced indirectly by their required care-team pair and the immutable-profile-role rule.

### Row-level security is enabled, but user identity is not mapped to it

The schema enables RLS on all six tables and creates **no policies**. Under ordinary non-owner/non-bypass database roles, that denies row access; privileged roles and normally table owners bypass it. This is why a new unprivileged connection can ping successfully yet see no rows or fail writes. See [PostgreSQL's RLS rules](https://www.postgresql.org/docs/current/ddl-rowsecurity.html).

The app currently expects a privileged server-side database connection. Every browser request uses that same backend database role; Go session/care-team checks enforce application access. There is no `auth.uid()` policy, JWT-to-database identity propagation, or Supabase Auth integration. Do not describe the current RLS configuration as patient-specific database authorization. Existing policies on an already-used database are not removed by rerunning `schema.sql`; review them separately.

## 5. Seed scripts and rerun behavior

| File | What it inserts |
| --- | --- |
| `seed.sql` | Two doctors, three patients, four care-team links, 11 medical records, four report names, 12 healing measurements, and two biometric rows. Uses database-generated IDs and times relative to when first inserted. |
| `seed_large.sql` | An additional 300 patients, 400 links (all 300 to Sarah, every third also to James), 1,500 records, 300 report names, and 900 healing scores. The 1,500 records are two notes, one prescription, and two scan references per added patient. It adds no doctors or wearable readings. |

With both scripts, expected counts in a fresh database are: 305 total profiles (303 patients and two doctors), 404 care-team links, 1,511 records, 304 uploads, 912 healing rows, and two biometrics. Sarah has 302 linked patients; James has 102.

`seed_large.sql` first verifies the two seeded doctors exist with doctor role. It creates a transaction-local temporary table, `clearchart_bulk_seed`, with names, deterministic emails, DOBs, blood types, visit codes, timestamps, and six recovery scenarios. Profile inserts omit IDs; subsequent joins resolve their actual saved IDs by case-insensitive email. `generate_series(1,300)` creates the patient numbers. `CROSS JOIN LATERAL (VALUES ...)` expands each patient into their related record/score rows. A second temporary table, `clearchart_bulk_patients`, joins fixture rows to actual patient/doctor IDs. `ON COMMIT DROP` removes both temporary SQL tables at commit; no repository file needs deletion for them.

The mock image references, such as `/mock-scans/{patientID}/1.svg`, are constructed using each actual saved profile ID; they do not assume the deterministic memory-fixture UUIDs. The Go server generates their illustrations from fixtures; seeding SQL does not upload CT images to Supabase Storage. Likewise `patient_uploads` only contains names.

All scripts use `BEGIN`/`COMMIT`. Seed transactions serialize against each other using the same transaction-scoped `pg_advisory_xact_lock(1129071442, 1)`, released at transaction completion. Profiles are inserted without primary-key IDs and reused by `lower(email)`; care-team links use the resulting IDs and ignore existing pairs. Dependent records, upload metadata, healing, and biometrics use semantic `NOT EXISTS` checks so rerunning the unchanged payload does not add copies merely because fresh random IDs would differ. Existing profile IDs and matching rows remain intact. This has limits:

- Rerunning an unchanged seed does **not** refresh existing fixture timestamps/content.
- Changing fixture content, image references, filenames, or measured values can create a new row because its semantic payload differs. Seeds are not update migrations or a general deduplication service.
- A matching email is reused regardless of its original UUID. Expected doctor/patient roles must still match; do not repurpose fixture emails for another role.
- Seed serialization coordinates the seed scripts; it does not make concurrent ordinary app writes use the same semantic deduplication contract.
- Rerunning schema uses `CREATE TABLE/INDEX IF NOT EXISTS`; it does not reconcile an old table's columns/constraints with the new SQL text. Functions are replaced; existing triggers/policies are not fully reconciled.
- Removing a row from the script does not remove it from the database.

Once data matters, preserve these files as the initial schema and introduce numbered migrations for subsequent changes. Test migrations on a disposable copy and retain backups; do not replace existing tables to make their shape match a file.

## 6. Connect to Supabase, step by step

These are instructions for you to execute against a project you control. Use synthetic data while the demo login exists.

1. Create a Supabase project or choose an empty development project. Save its **database password**; API keys are not PostgreSQL passwords.
2. In its SQL Editor, run the complete `schema.sql` from this folder. Run `seed.sql` next. Run `seed_large.sql` only if you want the full scroll-testing directory. The Go application never automatically executes any of them.
3. Open the project's **Connect** panel and copy the connection string. For this long-running Go server, use direct connection when your network supports it; use the session pooler on port 5432 when you need IPv4. Copy the exact host and username rather than guessing. Supabase documents these choices in [Connecting to Postgres](https://supabase.com/docs/guides/database/connecting-to-postgres).
4. Replace its password placeholder with the URL-encoded database password. Retain `sslmode=require` for encrypted demo connections. For deployment, use certificate/hostname verification with `sslmode=verify-full` and `sslrootcert` pointing to the downloaded server CA certificate. Those options must be configured in the driver; downloading a certificate alone is insufficient. See [Supabase's TLS guidance](https://supabase.com/docs/guides/database/connecting-to-postgres) and [the driver's connection parameters](https://pkg.go.dev/github.com/lib/pq).
5. Set `DATABASE_URL` in the **same PowerShell process** that will start Go. This prompt avoids placing your full DSN literally in a command/history entry:

```powershell
Set-Location 'D:\projects\ClearChart'
$clearChartSecret = Read-Host 'Paste the completed PostgreSQL connection string' -AsSecureString
$env:DATABASE_URL = [System.Net.NetworkCredential]::new('', $clearChartSecret).Password
Remove-Variable clearChartSecret
$env:HOST = '127.0.0.1'
$env:PORT = '8080'
go run .
```

The process environment must contain the plaintext DSN for the driver to use it; the prompt only keeps it out of ordinary typed history/display. Do not print the environment value, commit it, or put it in browser HTML. `.env.example` documents settings, but copying it to `.env` alone has **no effect**: this app does not load dotenv files.

6. Expect the startup label `Supabase connected`. Open `/demo/doctor` and `/demo/patient` to use the seeded accounts. If port 8080 is occupied, stop your previous app in its own terminal or use another `PORT`; do not terminate an unidentified process.

Connection string shapes, with placeholders rather than actual credentials:

```text
Direct:
postgresql://postgres:ENCODED_PASSWORD@db.PROJECT_REF.supabase.co:5432/postgres?sslmode=require

Session pooler:
postgresql://postgres.PROJECT_REF:ENCODED_PASSWORD@EXACT_POOLER_HOST:5432/postgres?sslmode=require

Verification options to use with the actual trusted certificate:
sslmode=verify-full&sslrootcert=D%3A%2Fsecrets%2Fsupabase-ca.crt
```

For the last example, replace the existing `sslmode` option, append `sslrootcert`, and use the real certificate's encoded absolute path. Do not leave placeholder text in the DSN. `lib/pq` v1.10.9 is the dependency pinned by this repository; this documentation does not change that version.

The pool is currently capped at 10 connections **per Go process**, not per project. Multiple app replicas multiply that budget, and other Supabase services also use connections. Review [Supabase pool limits](https://supabase.com/docs/guides/database/connecting-to-postgres/pooling-and-limits) before increasing it. Transaction-pooler behavior differs from session mode; it is not needed just to persist this prototype.

## 7. Optional local PostgreSQL instead

Supabase is not mandatory. A local PostgreSQL server can run the same SQL and Go code. Install a supported PostgreSQL release and its command-line tools, make `psql`, `createuser`, and `createdb` available in PATH, and start the PostgreSQL service. These steps create a dedicated development database; choose different names if they already exist.

```powershell
Set-Location 'D:\projects\ClearChart'
createuser -h 127.0.0.1 -U postgres --pwprompt --no-superuser --no-createdb --no-createrole clearchart_owner
if ($LASTEXITCODE -ne 0) { throw 'Creating the development role failed.' }
createdb -h 127.0.0.1 -U postgres --owner=clearchart_owner clearchart
if ($LASTEXITCODE -ne 0) { throw 'Creating the development database failed.' }
```

`createuser --pwprompt` asks for the **new role's** password; authentication as the existing administrator may also prompt for its password. The database owner is a development convenience, not the final least-privilege runtime design. Command semantics are documented by PostgreSQL for [createuser](https://www.postgresql.org/docs/current/app-createuser.html) and [createdb](https://www.postgresql.org/docs/current/app-createdb.html).

Set `DATABASE_URL` using the secure prompt above, with this local shape and your URL-encoded development password:

```text
postgresql://clearchart_owner:ENCODED_PASSWORD@127.0.0.1:5432/clearchart?sslmode=disable
```

`sslmode=disable` here assumes a loopback-only local development server without TLS. Do not copy that setting to a remote deployment.

Apply the schema **as `clearchart_owner`**, so that this development role owns the tables and can use the current policy-free RLS configuration:

```powershell
foreach ($clearChartSql in @('schema.sql', 'seed.sql')) {
    psql --dbname=$env:DATABASE_URL --set=ON_ERROR_STOP=1 --file=$clearChartSql
    if ($LASTEXITCODE -ne 0) { throw "Applying $clearChartSql failed." }
}
# Optional larger directory:
psql --dbname=$env:DATABASE_URL --set=ON_ERROR_STOP=1 --file=seed_large.sql
if ($LASTEXITCODE -ne 0) { throw 'Applying seed_large.sql failed.' }
go run .
```

`--set=ON_ERROR_STOP=1` makes a script error stop the command rather than silently continuing; see [psql](https://www.postgresql.org/docs/current/app-psql.html). The app's UI/log will still say `Supabase connected`; the actual destination is the DSN you supplied.

## 8. Prove that the database is being used

Successful `PingContext` proves the server is reachable; it does **not** prove the schema is present, the login route works, or a write persists. `/healthz` is an app liveness response, not a database-readiness test.

Perform this concrete check after applying the schema and base seed:

1. Start with `DATABASE_URL` set. Open `/demo/doctor`, select Alex Morgan, and save a note containing a unique marker such as `Persistence check 2026-09-26 A`.
2. Open `/demo/patient` in another tab. Verify the note appears through the live stream.
3. In Supabase SQL Editor (or `psql`), run:

```sql
SELECT r.id, r.patient_id, r.doctor_id, r.type, r.content, r.timestamp
FROM public.medical_records r
JOIN public.profiles p ON p.id = r.patient_id
WHERE lower(p.email) = lower('alex.morgan@example.com')
  AND r.content LIKE '%Persistence check 2026-09-26 A%'
ORDER BY r.timestamp DESC;
```

4. Stop Go with Ctrl+C and restart it in the same shell with `go run .`. Reenter via the demo role links because sessions are not persisted. Verify the same row/note still exists.
5. Submit a filename-only upload and a healing score, then confirm rows in `patient_uploads` and `healing_progress` if you want coverage of all three write flows. File contents are still not persisted.

A count check on a freshly seeded database is:

```sql
SELECT 'profiles' AS table_name, count(*) AS rows FROM public.profiles
UNION ALL SELECT 'care_team', count(*) FROM public.care_team
UNION ALL SELECT 'medical_records', count(*) FROM public.medical_records
UNION ALL SELECT 'patient_uploads', count(*) FROM public.patient_uploads
UNION ALL SELECT 'healing_progress', count(*) FROM public.healing_progress
UNION ALL SELECT 'mock_biometric_data', count(*) FROM public.mock_biometric_data;
```

Existing automated tests primarily exercise `NewMemoryStore` and HTTP/SSE behavior. Passing them is useful but is not evidence that your remote credentials, schema, PostgreSQL triggers, or persistence work. The SQL-backed smoke test above remains necessary until an integration-test database is configured.

## 9. What migrates, what does not, and how to start without fixtures

Setting `DATABASE_URL` switches the storage implementation at the **next process start**. It does not copy current Go maps into SQL. The SQL seed files recreate predefined fictional fixtures, not notes/uploads/profiles you added interactively to memory mode. Those ad hoc values disappear when the memory process stops. There is no export/import command yet; add a deliberately reviewed export/import path before relying on preservation of memory-only changes.

The temporary memory-backed demo remains available until Supabase is ready. Its deterministic fixture IDs are independent of the database-generated IDs in SQL. You can keep `NewMemoryStore`, `bulk_seed.go`, `seed.sql`, and `seed_large.sql` in the repository while using PostgreSQL. They are useful for tests and demos, and they do not run against SQL automatically. Deleting them is not a prerequisite for persistence. Be aware that simply deleting `bulk_seed.go` breaks compilation because `NewMemoryStore()` references it; fixture removal requires changing dependencies/tests, as explained in the cleanup guide.

If you run **only `schema.sql`**, the tables are empty:

- The `/` redirect and default `/demo/patient` and `/demo/doctor` entries cannot find their default fixture emails and return a missing-profile response.
- Onboarding can create a profile, but SQL auto-linking looks for the seeded counterpart by case-insensitive email and role. With no seed counterpart, `CreateProfile` succeeds with **no care-team link**. The current onboarding success text claiming a connection is then inaccurate.
- A doctor with no linked patients has no chart to author. A patient with no linked doctors has no provider receiving their data.
- Onboarding establishes a session immediately, but there is no verified login/recovery flow after restart. `/demo/{role}?id={uuid}` can regain a demo session, which is why it cannot be retained as real authentication.

For an unseeded development database, create the doctor and patient through onboarding, inspect their IDs, and link them intentionally. There is currently no HTTP endpoint for care-team management. This SQL example links existing profiles selected by their emails; replace both example emails and verify the returned pair:

```sql
INSERT INTO public.care_team (patient_id, doctor_id)
SELECT p.id, d.id
FROM public.profiles p
CROSS JOIN public.profiles d
WHERE p.role = 'patient'
  AND lower(p.email) = lower('your.patient@example.com')
  AND d.role = 'doctor'
  AND lower(d.email) = lower('your.doctor@example.com')
ON CONFLICT DO NOTHING
RETURNING patient_id, doctor_id;
```

If no row returns, either the pair already existed or an email/role did not match; inspect the profiles and care-team table rather than assuming success. Reopen/reload the doctor dashboard after adding a link so the patient directory is rebuilt. Existing live snapshots do not replace that directory.

## 10. Moving from persistent demo to an operational service

There are two separate milestones:

**A persistent prototype** uses the current SQL implementation, applied schema, server-held DSN, and a built Go binary behind an HTTPS-capable host/proxy. This is enough to preserve synthetic records between runs. The walkthrough and cleanup guide cover the binary and embedded assets; retain SSE support and do not buffer live responses at the proxy.

**A service entrusted with actual patient information** needs additional engineering beyond a database switch. Remove unrestricted demo account selection, implement verified identity and account recovery, associate profiles with that identity, and replace automatic seeded care links with an authorized invitation/consent process. Define and test per-user access at the server and database boundaries; the current owner/privileged connection and lack of policies are not that design. Preserve auditable authorship and amendment history, implement real private file storage if needed, add tested backups/restores and schema migrations, secure secrets/TLS/session storage, and plan monitoring and operational ownership. This is a technical gap list, not a claim of clinical or regulatory readiness.

For multiple Go replicas, in-memory sessions do not travel between instances and the hub only immediately notifies tabs connected to the same process. The 15-second database refresh can observe some external writes, but it does not make the hub or sessions distributed. Use shared session storage or verified stateless sessions, and an explicit cross-instance notification mechanism when you reach that stage.

To return this terminal to the disposable in-memory demo later, stop Go and run:

```powershell
Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
go run .
```

This removes the process environment setting only. It does not delete or alter the PostgreSQL database.
