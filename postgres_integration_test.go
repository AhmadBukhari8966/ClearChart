package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

// Opt-in only: creates uniquely named disposable databases on a LOCAL server,
// never uses DATABASE_URL, and drops only the databases this test created.
// CLEARCHART_TEST_DATABASE_URL must be a local admin connection URL;
// CLEARCHART_TEST_ALLOW_CREATE=1 acknowledges database creation/deletion.
func isolatedPostgres(t *testing.T) *postgresStore {
	t.Helper()
	dsn := os.Getenv("CLEARCHART_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set CLEARCHART_TEST_DATABASE_URL to run isolated PostgreSQL integration tests")
	}
	if os.Getenv("CLEARCHART_TEST_ALLOW_CREATE") != "1" {
		t.Fatal("CLEARCHART_TEST_ALLOW_CREATE=1 required")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") ||
		(u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		t.Fatal("test database URL must identify a local PostgreSQL server")
	}
	if u.Query().Get("host") != "" || u.Query().Get("hostaddr") != "" || u.Query().Get("service") != "" {
		t.Fatal("connection host overrides are not allowed in the test URL")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := "clearchart_test_" + strings.ReplaceAll(newID(), "-", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name)); err != nil {
		t.Fatal(err)
	}
	// Register cleanup as soon as CREATE succeeds, before attempting a connection.
	var result *postgresStore
	t.Cleanup(func() {
		if result != nil {
			result.Close()
		}
		cleanupCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE "+pq.QuoteIdentifier(name)); err != nil {
			t.Errorf("could not remove isolated database %s: %v", name, err)
		}
	})
	u.Path = "/" + name
	connected, err := NewPostgresStore(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	result = connected.(*postgresStore)
	return result
}

func applyTestSQL(t *testing.T, db *sql.DB, files ...string) {
	t.Helper()
	for _, file := range files {
		script, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err = db.ExecContext(ctx, string(script))
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
}

func postgresSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var result strings.Builder
	for _, table := range []string{"profiles", "care_team", "medical_records", "patient_uploads", "healing_progress", "mock_biometric_data"} {
		var digest string
		query := "SELECT md5(COALESCE(string_agg(row_to_json(x)::text, '' ORDER BY row_to_json(x)::text), '')) FROM public." + pq.QuoteIdentifier(table) + " x"
		if err := db.QueryRow(query).Scan(&digest); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&result, "%s:%s\n", table, digest)
	}
	return result.String()
}

// Fixture lookup deliberately stays in tests; production account lookup uses
// only the verified Supabase identity, never an email or a seeded UUID.
func postgresFixtureByEmail(s *postgresStore, ctx context.Context, email string) (Profile, error) {
	return scanProfile(s.db.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM profiles p WHERE lower(p.email)=lower($1)`, email))
}

func TestPostgresGeneratedIDsAndRepeatableSeeds(t *testing.T) {
	for _, existingIDs := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing_profile_ids_%t", existingIDs), func(t *testing.T) {
			s := isolatedPostgres(t)
			applyTestSQL(t, s.db, "schema.sql")
			if existingIDs {
				// Represent a database previously seeded with fixed profile UUIDs.
				_, err := s.db.Exec(`INSERT INTO profiles(id,role,name,email,license_num,specialization,dob,blood_type) VALUES
     ($1,'doctor','Dr. Sarah Chen','SARAH.CHEN@EXAMPLE.COM','DEMO-ORTHO-2048','Orthopedics',NULL,NULL),
     ($2,'patient','Alex Morgan','ALEX.MORGAN@EXAMPLE.COM',NULL,NULL,'1994-06-15','O+')`, demoDoctorID, demoPatientID)
				if err != nil {
					t.Fatal(err)
				}
			}
			applyTestSQL(t, s.db, "seed.sql", "seed_large.sql")
			for table, want := range map[string]int{"profiles": 305, "care_team": 404, "medical_records": 1511, "patient_uploads": 304, "healing_progress": 912, "mock_biometric_data": 2} {
				var got int
				if err := s.db.QueryRow("SELECT count(*) FROM public." + pq.QuoteIdentifier(table)).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("%s: got %d, want %d", table, got, want)
				}
			}
			before := postgresSnapshot(t, s.db)
			applyTestSQL(t, s.db, "seed.sql", "seed_large.sql")
			if after := postgresSnapshot(t, s.db); after != before {
				t.Fatal("seed rerun changed existing IDs, values or timestamps")
			}
			ctx := context.Background()
			doctor, err := postgresFixtureByEmail(s, ctx, demoDoctorEmail)
			if err != nil {
				t.Fatal(err)
			}
			patient, err := postgresFixtureByEmail(s, ctx, demoPatientEmail)
			if err != nil {
				t.Fatal(err)
			}
			if existingIDs {
				if doctor.ID != demoDoctorID || patient.ID != demoPatientID {
					t.Fatal("seed replaced existing profile IDs")
				}
			} else if doctor.ID == demoDoctorID || patient.ID == demoPatientID {
				t.Fatal("fresh database retained prescribed fixture UUIDs")
			}
			patients, err := s.Patients(ctx, doctor.ID)
			if err != nil || len(patients) != 302 {
				t.Fatalf("doctor patients: %d, %v", len(patients), err)
			}
			zoe, err := postgresFixtureByEmail(s, ctx, "zoe.thompson.300@example.com")
			if err != nil {
				t.Fatal(err)
			}
			records, err := s.Records(ctx, zoe.ID)
			if err != nil || len(records) != 5 {
				t.Fatalf("bulk records: %d, %v", len(records), err)
			}
			images := 0
			for _, record := range records {
				if record.Type == "imaging" {
					images++
					if !strings.HasPrefix(record.ImageURL, "/mock-scans/"+zoe.ID+"/") {
						t.Fatal("scan URL uses another ID")
					}
				}
			}
			if images != 2 {
				t.Fatal("missing scans")
			}

			// Exercise routes against SQL IDs as well as directly through Store.
			a, err := newApp(s, "SQL integration test")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(a.hub.close)
			cookies, current, location := demoSession(t, a, a.routes(), "doctor")
			if current.ProfileID != doctor.ID || !strings.HasSuffix(location, doctor.ID) {
				t.Fatal("test session used an incorrect stored profile ID")
			}
			page := serveRequest(a.routes(), httptest.NewRequest(http.MethodGet, location+"?patient="+zoe.ID, nil), cookies)
			if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), zoe.ID+"/1.svg") {
				t.Fatal("SQL dashboard did not use stored scan IDs")
			}
			scan := serveRequest(a.routes(), httptest.NewRequest(http.MethodGet, "/mock-scans/"+zoe.ID+"/1.svg", nil), cookies)
			if scan.Code != http.StatusOK {
				t.Fatalf("SQL scan status: %d", scan.Code)
			}
			stream := openTestStream(t, a.routes(), cookies, "/events/doctor/"+doctor.ID+"?view=sql-test&datastar="+url.QueryEscape(`{"selectedpatient":"`+zoe.ID+`"}`))
			if !strings.Contains(readTestSnapshot(t, stream), "record-form-"+zoe.ID) {
				t.Fatal("SQL selection stream has the wrong form ID")
			}

			callerID := "not-an-id-the-caller-must-not-assign"
			createdPatient, err := s.CreateProfile(ctx, Profile{ID: callerID, AuthUserID: newID(), Role: "patient", Name: "SQL Patient", Email: "new.patient@example.com", DOB: "1990-01-02", BloodType: "O+"})
			if err != nil {
				t.Fatal(err)
			}
			createdDoctor, err := s.CreateProfile(ctx, Profile{ID: callerID, AuthUserID: newID(), Role: "doctor", Name: "SQL Doctor", Email: "new.doctor@example.com", LicenseNum: "TEST-123", Specialization: "Primary care"})
			if err != nil {
				t.Fatal(err)
			}
			uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
			checkID := func(id string) {
				t.Helper()
				if id == callerID || !uuid.MatchString(id) {
					t.Fatalf("not a database UUID: %q", id)
				}
			}
			checkID(createdPatient.ID)
			checkID(createdDoctor.ID)
			for _, pair := range [][2]string{{createdPatient.ID, doctor.ID}, {patient.ID, createdDoctor.ID}} {
				ok, err := s.IsCareTeam(ctx, pair[0], pair[1])
				if err != nil || ok {
					t.Fatalf("new profile unexpectedly received access to a seeded care team: %v", err)
				}
			}
			if _, err := s.CreateProfile(ctx, Profile{AuthUserID: newID(), Role: "patient", Name: "Duplicate", Email: "NEW.PATIENT@EXAMPLE.COM", DOB: "1990-01-02", BloodType: "O+"}); !errors.Is(err, ErrConflict) {
				t.Fatalf("duplicate email: %v", err)
			}
			if createdPatient.ID == createdPatient.AuthUserID {
				t.Fatal("profile ID must be distinct from its Supabase auth identity")
			}
			for _, created := range []Profile{createdPatient, createdDoctor} {
				found, err := s.ProfileByAuthUserID(ctx, created.AuthUserID)
				if err != nil || found.ID != created.ID {
					t.Fatalf("auth identity mapping failed: %#v, %v", found, err)
				}
			}
			duplicateAuth := createdPatient
			duplicateAuth.Email = "another.patient@example.com"
			if _, err := s.CreateProfile(ctx, duplicateAuth); !errors.Is(err, ErrConflict) {
				t.Fatalf("duplicate auth identity: %v", err)
			}
			unverified := createdPatient
			unverified.Email = "unverified@example.com"
			unverified.AuthUserID = ""
			if _, err := s.CreateProfile(ctx, unverified); err == nil {
				t.Fatal("accepted an unverified profile")
			}
			if _, err := s.ProfileByAuthUserID(ctx, ""); !errors.Is(err, ErrNotFound) {
				t.Fatalf("empty auth ID must not match an unclaimed seeded profile: %v", err)
			}
			// Administrators create care-team relationships explicitly after signup.
			if _, err := s.db.ExecContext(ctx, `INSERT INTO care_team(patient_id,doctor_id) VALUES($1,$2)`, createdPatient.ID, doctor.ID); err != nil {
				t.Fatal(err)
			}
			record, err := s.AddRecord(ctx, Record{ID: callerID, PatientID: createdPatient.ID, DoctorID: doctor.ID, Type: "note", Content: "Database-owned ID verification"})
			if err != nil {
				t.Fatal(err)
			}
			upload, err := s.AddUpload(ctx, Upload{ID: callerID, PatientID: createdPatient.ID, FileName: "db-generated.pdf"})
			if err != nil {
				t.Fatal(err)
			}
			healing, err := s.AddHealing(ctx, Healing{ID: callerID, PatientID: createdPatient.ID, StatusType: "mobility", Value: 7})
			if err != nil {
				t.Fatal(err)
			}
			checkID(record.ID)
			checkID(upload.ID)
			checkID(healing.ID)
			if record.Timestamp.IsZero() || upload.Timestamp.IsZero() || healing.Timestamp.IsZero() {
				t.Fatal("database did not provide timestamps")
			}
			if _, err = s.AddRecord(ctx, Record{PatientID: createdPatient.ID, DoctorID: createdDoctor.ID, Type: "note", Content: "Should fail"}); !errors.Is(err, ErrForbidden) {
				t.Fatalf("unlinked doctor: %v", err)
			}
		})
	}
}

func TestPostgresAuthMigrationPreservesExistingData(t *testing.T) {
	s := isolatedPostgres(t)
	applyTestSQL(t, s.db, "schema.sql", "seed.sql", "seed_large.sql")
	// Recreate the previous schema shape. Only this disposable test database
	// loses a column, which has no values before accounts are onboarded.
	if _, err := s.db.Exec("ALTER TABLE public.profiles DROP COLUMN auth_user_id"); err != nil {
		t.Fatal(err)
	}
	var before, after string
	query := `SELECT md5(string_agg((to_jsonb(p)-'auth_user_id')::text, '' ORDER BY p.id)) FROM profiles p`
	if err := s.db.QueryRow(query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	applyTestSQL(t, s.db, "migrations/001_auth_identity.sql", "migrations/001_auth_identity.sql")
	if err := s.db.QueryRow(query).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("auth migration changed existing profile data")
	}
	var claimed int
	if err := s.db.QueryRow("SELECT count(*) FROM profiles WHERE auth_user_id IS NOT NULL").Scan(&claimed); err != nil || claimed != 0 {
		t.Fatalf("migration claimed seeded profiles: count=%d, error=%v", claimed, err)
	}
	// A deliberate administrator association survives reapplying the schema
	// and seeds; no email match in the login path performs this association.
	authID := newID()
	if _, err := s.db.Exec("UPDATE profiles SET auth_user_id=$1 WHERE lower(email)=lower($2)", authID, demoDoctorEmail); err != nil {
		t.Fatal(err)
	}
	beforeAll := postgresSnapshot(t, s.db)
	applyTestSQL(t, s.db, "schema.sql", "seed.sql", "seed_large.sql")
	if afterAll := postgresSnapshot(t, s.db); beforeAll != afterAll {
		t.Fatal("reapplying schema and seeds changed stored account mappings or records")
	}
	profile, err := s.ProfileByAuthUserID(context.Background(), authID)
	if err != nil || profile.Email != demoDoctorEmail {
		t.Fatalf("explicit seeded account association was not preserved: %#v, %v", profile, err)
	}
}
