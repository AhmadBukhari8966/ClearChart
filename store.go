package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("not part of this patient's care team")
	ErrConflict  = errors.New("a profile with this email already exists")
)

type memoryStore struct {
	mu         sync.RWMutex
	profiles   map[string]Profile
	care       map[string]map[string]bool
	records    []Record
	uploads    []Upload
	healing    []Healing
	biometrics map[string]Biometric
}

// NewMemoryStore is an explicitly ephemeral, seeded demo. Database connection
// failures never fall back to it silently.
func NewMemoryStore() Store {
	s := &memoryStore{profiles: map[string]Profile{}, care: map[string]map[string]bool{}, biometrics: map[string]Biometric{}}
	now := time.Now().UTC()
	doctor2 := "00000000-0000-4000-8000-000000000002"
	patient2 := "00000000-0000-4000-8000-000000000102"
	patient3 := "00000000-0000-4000-8000-000000000103"
	for _, p := range []Profile{
		{ID: demoDoctorID, Role: "doctor", Name: "Dr. Sarah Chen", Email: "sarah.chen@example.com", LicenseNum: "DEMO-ORTHO-2048", Specialization: "Orthopedics"},
		{ID: doctor2, Role: "doctor", Name: "Dr. James Wilson", Email: "james.wilson@example.com", LicenseNum: "DEMO-PCP-1024", Specialization: "Primary care"},
		{ID: demoPatientID, Role: "patient", Name: "Alex Morgan", Email: "alex.morgan@example.com", DOB: "1994-06-15", BloodType: "O+"},
		{ID: patient2, Role: "patient", Name: "Jordan Lee", Email: "jordan.lee@example.com", DOB: "1987-03-22", BloodType: "A+"},
		{ID: patient3, Role: "patient", Name: "Taylor Brooks", Email: "taylor.brooks@example.com", DOB: "2000-11-08", BloodType: "B+"},
	} {
		s.profiles[p.ID] = p
	}
	s.care[demoPatientID] = map[string]bool{demoDoctorID: true, doctor2: true}
	s.care[patient2] = map[string]bool{demoDoctorID: true}
	s.care[patient3] = map[string]bool{doctor2: true}
	type seedRecord struct {
		patient, doctor, kind, content, image string
		hours                                 int
	}
	for i, r := range []seedRecord{
		{demoPatientID, demoDoctorID, "note", "Two-week knee recovery review: incision is healing well. Swelling has reduced and range of motion is improving. Continue the rehabilitation plan and follow up in two weeks.", "", 2},
		{demoPatientID, demoDoctorID, "prescription", "Demo medication plan: acetaminophen as directed on the discharge instructions, only when needed. Review all medications with your care team.", "", 26},
		{demoPatientID, demoDoctorID, "imaging", "Follow-up knee imaging: postoperative alignment is maintained. No new concerning findings in this simulated study.", "/static/ct-scan.svg", 50},
		{demoPatientID, doctor2, "note", "Recovery check-in: sleep is improving and Alex is walking more comfortably with support. Keep sharing any changes with the care team.", "", 74},
		{demoPatientID, demoDoctorID, "note", "Initial postoperative review: begin the agreed gentle movement plan with your physiotherapist. Expected swelling is present; the wound looks clean.", "", 170},
		{patient2, demoDoctorID, "note", "Shoulder follow-up: mobility is improving with physiotherapy. Continue the agreed exercise plan and review next month.", "", 5},
		{patient2, demoDoctorID, "prescription", "Demo prescription review: continue current care plan. Medication questions will be reviewed at the next appointment.", "", 52},
		{patient2, demoDoctorID, "imaging", "Simulated shoulder imaging reviewed. Findings are consistent with the established recovery plan.", "/static/ct-scan.svg", 100},
		{patient3, doctor2, "note", "Ankle recovery check: less swelling reported and daily activity is gradually increasing. Follow up as scheduled.", "", 8},
		{patient3, doctor2, "prescription", "Demo medication reconciliation completed. No changes to the discharge medication plan.", "", 76},
		{patient3, doctor2, "imaging", "Simulated ankle imaging reviewed with the patient. Recovery remains on the expected course.", "/static/ct-scan.svg", 124},
	} {
		s.records = append(s.records, Record{ID: fmt.Sprintf("10000000-0000-4000-8000-%012d", i+1), PatientID: r.patient, DoctorID: r.doctor, Type: r.kind, Content: r.content, ImageURL: r.image, DoctorName: s.profiles[r.doctor].Name, PatientName: s.profiles[r.patient].Name, Timestamp: now.Add(-time.Duration(r.hours) * time.Hour)})
	}
	for i, u := range []struct {
		patient, name string
		hours         int
	}{
		{demoPatientID, "physiotherapy-progress.pdf", 4}, {demoPatientID, "discharge-summary.pdf", 168}, {patient2, "shoulder-exercises.pdf", 12}, {patient3, "ankle-recovery-notes.pdf", 20},
	} {
		s.uploads = append(s.uploads, Upload{ID: fmt.Sprintf("20000000-0000-4000-8000-%012d", i+1), PatientID: u.patient, FileName: u.name, PatientName: s.profiles[u.patient].Name, Timestamp: now.Add(-time.Duration(u.hours) * time.Hour)})
	}
	for i, h := range []struct {
		patient, kind string
		value, hours  int
	}{
		{demoPatientID, "pain", 6, 168}, {demoPatientID, "mobility", 3, 168}, {demoPatientID, "energy", 4, 168},
		{demoPatientID, "pain", 3, 3}, {demoPatientID, "mobility", 7, 3}, {demoPatientID, "energy", 8, 3},
		{patient2, "pain", 2, 6}, {patient2, "mobility", 8, 6}, {patient2, "energy", 7, 6},
		{patient3, "pain", 4, 9}, {patient3, "mobility", 6, 9}, {patient3, "energy", 7, 9},
	} {
		s.healing = append(s.healing, Healing{ID: fmt.Sprintf("30000000-0000-4000-8000-%012d", i+1), PatientID: h.patient, StatusType: h.kind, Value: h.value, Timestamp: now.Add(-time.Duration(h.hours) * time.Hour)})
	}
	s.biometrics[demoDoctorID] = Biometric{DoctorID: demoDoctorID, HeartRate: 64, SleepHours: 7.6, Timestamp: now.Add(-30 * time.Minute)}
	s.biometrics[doctor2] = Biometric{DoctorID: doctor2, HeartRate: 68, SleepHours: 7.2, Timestamp: now.Add(-45 * time.Minute)}
	addBulkPatients(s, now)
	return s
}

func (s *memoryStore) Close() error { return nil }
func (s *memoryStore) Profile(ctx context.Context, id string) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.profiles[id]
	if !ok {
		return Profile{}, ErrNotFound
	}
	return p, nil
}
func (s *memoryStore) Profiles(ctx context.Context, role string) ([]Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Profile
	for _, p := range s.profiles {
		if p.Role == role {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *memoryStore) CareTeam(ctx context.Context, patient string) ([]Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Profile
	for doctor := range s.care[patient] {
		out = append(out, s.profiles[doctor])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *memoryStore) Patients(ctx context.Context, doctor string) ([]Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Profile
	for patient, doctors := range s.care {
		if doctors[doctor] {
			out = append(out, s.profiles[patient])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *memoryStore) Records(ctx context.Context, patient string) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Record
	for _, r := range s.records {
		if r.PatientID == patient {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	return out, nil
}
func (s *memoryStore) Uploads(ctx context.Context, patient string) ([]Upload, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Upload
	for _, u := range s.uploads {
		if u.PatientID == patient {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	return out, nil
}

// Reports collects a doctor's reports in one pass, rather than one lookup per
// patient. The same operation uses a single SQL join in the PostgreSQL store.
func (s *memoryStore) Reports(ctx context.Context, doctor string) ([]Upload, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Upload
	for _, u := range s.uploads {
		if s.care[u.PatientID][doctor] {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	return out, nil
}

func (s *memoryStore) Healing(ctx context.Context, patient string) ([]Healing, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	latest := map[string]Healing{}
	for _, h := range s.healing {
		if h.PatientID == patient {
			prev, ok := latest[h.StatusType]
			if !ok || h.Timestamp.After(prev.Timestamp) {
				latest[h.StatusType] = h
			}
		}
	}
	var out []Healing
	for _, h := range latest {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StatusType < out[j].StatusType })
	return out, nil
}
func (s *memoryStore) Biometrics(ctx context.Context, doctor string) (Biometric, error) {
	if err := ctx.Err(); err != nil {
		return Biometric{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.biometrics[doctor]
	if !ok {
		return Biometric{}, ErrNotFound
	}
	return b, nil
}
func (s *memoryStore) IsCareTeam(ctx context.Context, patient, doctor string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.care[patient][doctor], nil
}
func (s *memoryStore) CreateProfile(ctx context.Context, p Profile) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.profiles {
		if strings.EqualFold(existing.Email, p.Email) {
			return Profile{}, ErrConflict
		}
	}
	if p.Role != "doctor" && p.Role != "patient" {
		return Profile{}, errors.New("invalid role")
	}
	p.ID = newID()
	s.profiles[p.ID] = p
	if p.Role == "patient" {
		s.care[p.ID] = map[string]bool{demoDoctorID: true}
	} else {
		s.care[demoPatientID][p.ID] = true
	}
	return p, nil
}
func (s *memoryStore) AddRecord(ctx context.Context, r Record) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.care[r.PatientID][r.DoctorID] {
		return Record{}, ErrForbidden
	}
	if r.Type != "note" && r.Type != "prescription" && r.Type != "imaging" {
		return Record{}, errors.New("invalid record type")
	}
	r.ID = newID()
	r.Timestamp = time.Now().UTC()
	r.DoctorName = s.profiles[r.DoctorID].Name
	r.PatientName = s.profiles[r.PatientID].Name
	s.records = append(s.records, r)
	return r, nil
}
func (s *memoryStore) AddUpload(ctx context.Context, u Upload) (Upload, error) {
	if err := ctx.Err(); err != nil {
		return Upload{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[u.PatientID]
	if !ok || p.Role != "patient" {
		return Upload{}, ErrNotFound
	}
	u.ID = newID()
	u.Timestamp = time.Now().UTC()
	u.PatientName = p.Name
	s.uploads = append(s.uploads, u)
	return u, nil
}
func (s *memoryStore) AddHealing(ctx context.Context, h Healing) (Healing, error) {
	if err := ctx.Err(); err != nil {
		return Healing{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[h.PatientID]
	if !ok || p.Role != "patient" {
		return Healing{}, ErrNotFound
	}
	if h.Value < 1 || h.Value > 10 {
		return Healing{}, errors.New("healing value must be 1–10")
	}
	h.ID = newID()
	h.Timestamp = time.Now().UTC()
	s.healing = append(s.healing, h)
	return h, nil
}

type postgresStore struct{ db *sql.DB }

func NewPostgresStore(ctx context.Context, dsn string) (Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return &postgresStore{db: db}, nil
}
func (s *postgresStore) Close() error { return s.db.Close() }
func storageError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.Constraint, "email") {
		return ErrConflict
	}
	return err
}

type rowScanner interface{ Scan(...any) error }

const profileColumns = `p.id::text,p.role,p.name,p.email,COALESCE(p.license_num,''),COALESCE(p.specialization,''),COALESCE(to_char(p.dob,'YYYY-MM-DD'),''),COALESCE(p.blood_type,'')`

func scanProfile(row rowScanner) (Profile, error) {
	var p Profile
	err := row.Scan(&p.ID, &p.Role, &p.Name, &p.Email, &p.LicenseNum, &p.Specialization, &p.DOB, &p.BloodType)
	return p, storageError(err)
}
func (s *postgresStore) Profile(ctx context.Context, id string) (Profile, error) {
	return scanProfile(s.db.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM profiles p WHERE p.id=$1`, id))
}
func (s *postgresStore) queryProfiles(ctx context.Context, query string, arg string) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *postgresStore) Profiles(ctx context.Context, role string) ([]Profile, error) {
	return s.queryProfiles(ctx, `SELECT `+profileColumns+` FROM profiles p WHERE p.role=$1 ORDER BY p.name`, role)
}
func (s *postgresStore) CareTeam(ctx context.Context, patient string) ([]Profile, error) {
	return s.queryProfiles(ctx, `SELECT `+profileColumns+` FROM profiles p JOIN care_team c ON c.doctor_id=p.id WHERE c.patient_id=$1 ORDER BY p.name`, patient)
}
func (s *postgresStore) Patients(ctx context.Context, doctor string) ([]Profile, error) {
	return s.queryProfiles(ctx, `SELECT `+profileColumns+` FROM profiles p JOIN care_team c ON c.patient_id=p.id WHERE c.doctor_id=$1 ORDER BY p.name`, doctor)
}
func scanRecord(row rowScanner) (Record, error) {
	var r Record
	err := row.Scan(&r.ID, &r.PatientID, &r.DoctorID, &r.Type, &r.Content, &r.Timestamp, &r.ImageURL, &r.DoctorName, &r.PatientName)
	return r, storageError(err)
}

const recordColumns = `r.id::text,r.patient_id::text,r.doctor_id::text,r.type,r.content,r.timestamp,COALESCE(r.image_url,''),d.name,p.name`

func (s *postgresStore) Records(ctx context.Context, patient string) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recordColumns+` FROM medical_records r JOIN profiles d ON d.id=r.doctor_id JOIN profiles p ON p.id=r.patient_id WHERE r.patient_id=$1 ORDER BY r.timestamp DESC,r.id DESC`, patient)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func scanUpload(row rowScanner) (Upload, error) {
	var u Upload
	err := row.Scan(&u.ID, &u.PatientID, &u.FileName, &u.Timestamp, &u.PatientName)
	return u, storageError(err)
}
func (s *postgresStore) Uploads(ctx context.Context, patient string) ([]Upload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id::text,u.patient_id::text,u.file_name,u.timestamp,p.name FROM patient_uploads u JOIN profiles p ON p.id=u.patient_id WHERE u.patient_id=$1 ORDER BY u.timestamp DESC,u.id DESC`, patient)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *postgresStore) Reports(ctx context.Context, doctor string) ([]Upload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id::text,u.patient_id::text,u.file_name,u.timestamp,p.name
	 FROM patient_uploads u JOIN profiles p ON p.id=u.patient_id
	 JOIN care_team c ON c.patient_id=u.patient_id WHERE c.doctor_id=$1
	 ORDER BY u.timestamp DESC,u.id DESC`, doctor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func scanHealing(row rowScanner) (Healing, error) {
	var h Healing
	err := row.Scan(&h.ID, &h.PatientID, &h.StatusType, &h.Value, &h.Timestamp)
	return h, storageError(err)
}
func (s *postgresStore) Healing(ctx context.Context, patient string) ([]Healing, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT ON (status_type) id::text,patient_id::text,status_type,value,timestamp FROM healing_progress WHERE patient_id=$1 ORDER BY status_type,timestamp DESC,id DESC`, patient)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Healing
	for rows.Next() {
		h, err := scanHealing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
func (s *postgresStore) Biometrics(ctx context.Context, doctor string) (Biometric, error) {
	var b Biometric
	err := s.db.QueryRowContext(ctx, `SELECT doctor_id::text,heart_rate,sleep_hours,timestamp FROM mock_biometric_data WHERE doctor_id=$1 ORDER BY timestamp DESC LIMIT 1`, doctor).Scan(&b.DoctorID, &b.HeartRate, &b.SleepHours, &b.Timestamp)
	return b, storageError(err)
}
func (s *postgresStore) IsCareTeam(ctx context.Context, patient, doctor string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM care_team WHERE patient_id=$1 AND doctor_id=$2)`, patient, doctor).Scan(&ok)
	return ok, err
}
func (s *postgresStore) CreateProfile(ctx context.Context, p Profile) (Profile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Profile{}, err
	}
	defer tx.Rollback()
	p.ID = newID()
	_, err = tx.ExecContext(ctx, `INSERT INTO profiles(id,role,name,email,license_num,specialization,dob,blood_type) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,'')::date,NULLIF($8,''))`, p.ID, p.Role, p.Name, p.Email, p.LicenseNum, p.Specialization, p.DOB, p.BloodType)
	if err != nil {
		return Profile{}, storageError(err)
	}
	// Onboarding connects each demo account to one seeded counterpart so both
	// sides of the product can be explored immediately.
	if p.Role == "patient" {
		_, err = tx.ExecContext(ctx, `INSERT INTO care_team(patient_id,doctor_id) SELECT $1,id FROM profiles WHERE id=$2 AND role='doctor' ON CONFLICT DO NOTHING`, p.ID, demoDoctorID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO care_team(patient_id,doctor_id) SELECT id,$1 FROM profiles WHERE id=$2 AND role='patient' ON CONFLICT DO NOTHING`, p.ID, demoPatientID)
	}
	if err != nil {
		return Profile{}, storageError(err)
	}
	if err = tx.Commit(); err != nil {
		return Profile{}, err
	}
	return p, nil
}
func (s *postgresStore) AddRecord(ctx context.Context, r Record) (Record, error) {
	// Authorization and insertion share one statement, avoiding a gap between
	// checking a care-team relationship and writing the record.
	result, err := scanRecord(s.db.QueryRowContext(ctx, `WITH inserted AS (
	 INSERT INTO medical_records(id,patient_id,doctor_id,type,content,image_url)
	 SELECT $1,c.patient_id,c.doctor_id,$4,$5,NULLIF($6,'') FROM care_team c WHERE c.patient_id=$2 AND c.doctor_id=$3
	 RETURNING *) SELECT `+recordColumns+` FROM inserted r JOIN profiles d ON d.id=r.doctor_id JOIN profiles p ON p.id=r.patient_id`, newID(), r.PatientID, r.DoctorID, r.Type, r.Content, r.ImageURL))
	if errors.Is(err, ErrNotFound) {
		return Record{}, ErrForbidden
	}
	return result, err
}
func (s *postgresStore) AddUpload(ctx context.Context, u Upload) (Upload, error) {
	return scanUpload(s.db.QueryRowContext(ctx, `WITH inserted AS (
	 INSERT INTO patient_uploads(id,patient_id,file_name) SELECT $1,id,$3 FROM profiles WHERE id=$2 AND role='patient' RETURNING *)
	 SELECT u.id::text,u.patient_id::text,u.file_name,u.timestamp,p.name FROM inserted u JOIN profiles p ON p.id=u.patient_id`, newID(), u.PatientID, u.FileName))
}
func (s *postgresStore) AddHealing(ctx context.Context, h Healing) (Healing, error) {
	return scanHealing(s.db.QueryRowContext(ctx, `INSERT INTO healing_progress(id,patient_id,status_type,value) SELECT $1,id,$3,$4 FROM profiles WHERE id=$2 AND role='patient' RETURNING id::text,patient_id::text,status_type,value,timestamp`, newID(), h.PatientID, h.StatusType, h.Value))
}
