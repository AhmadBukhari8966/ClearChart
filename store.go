package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("not part of this patient's care team")
	ErrConflict  = errors.New("a profile for this email or account already exists")
)

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
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && (strings.Contains(pgErr.Constraint, "email") || strings.Contains(pgErr.Constraint, "auth_user_id")) {
		return ErrConflict
	}
	return err
}

type rowScanner interface{ Scan(...any) error }

const profileColumns = `p.id::text,COALESCE(p.auth_user_id::text,''),p.role,p.name,p.email,COALESCE(p.license_num,''),COALESCE(p.specialization,''),COALESCE(to_char(p.dob,'YYYY-MM-DD'),''),COALESCE(p.blood_type,'')`

func scanProfile(row rowScanner) (Profile, error) {
	var p Profile
	err := row.Scan(&p.ID, &p.AuthUserID, &p.Role, &p.Name, &p.Email, &p.LicenseNum, &p.Specialization, &p.DOB, &p.BloodType)
	return p, storageError(err)
}
func (s *postgresStore) Profile(ctx context.Context, id string) (Profile, error) {
	return scanProfile(s.db.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM profiles p WHERE p.id=$1`, id))
}
func (s *postgresStore) ProfileByAuthUserID(ctx context.Context, authUserID string) (Profile, error) {
	if authUserID == "" {
		return Profile{}, ErrNotFound
	}
	return scanProfile(s.db.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM profiles p WHERE p.auth_user_id=$1`, authUserID))
}
func (s *postgresStore) ProfileByEmail(ctx context.Context, email string) (Profile, error) {
	return scanProfile(s.db.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM profiles p WHERE lower(p.email)=lower($1)`, email))
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
	if p.AuthUserID == "" {
		return Profile{}, errors.New("authenticated user ID is required")
	}
	// PostgreSQL generates the profile ID. AuthUserID comes from the verified
	// Supabase identity; onboarding cannot claim profiles by email alone.
	err := s.db.QueryRowContext(ctx, `INSERT INTO profiles(auth_user_id,role,name,email,license_num,specialization,dob,blood_type)
 VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,'')::date,NULLIF($8,''))
 RETURNING id::text`, p.AuthUserID, p.Role, p.Name, p.Email, p.LicenseNum, p.Specialization, p.DOB, p.BloodType).Scan(&p.ID)
	if err != nil {
		return Profile{}, storageError(err)
	}
	return p, nil
}
func (s *postgresStore) AddRecord(ctx context.Context, r Record) (Record, error) {
	// Authorization and insertion share one statement. ID and timestamp use DB defaults.
	result, err := scanRecord(s.db.QueryRowContext(ctx, `WITH inserted AS (
  INSERT INTO medical_records(patient_id,doctor_id,type,content,image_url)
  SELECT c.patient_id,c.doctor_id,$3,$4,NULLIF($5,'') FROM care_team c WHERE c.patient_id=$1 AND c.doctor_id=$2
  RETURNING *) SELECT `+recordColumns+` FROM inserted r JOIN profiles d ON d.id=r.doctor_id JOIN profiles p ON p.id=r.patient_id`, r.PatientID, r.DoctorID, r.Type, r.Content, r.ImageURL))
	if errors.Is(err, ErrNotFound) {
		return Record{}, ErrForbidden
	}
	return result, err
}
func (s *postgresStore) AddUpload(ctx context.Context, u Upload) (Upload, error) {
	return scanUpload(s.db.QueryRowContext(ctx, `WITH inserted AS (
  INSERT INTO patient_uploads(patient_id,file_name) SELECT id,$2 FROM profiles WHERE id=$1 AND role='patient' RETURNING *)
  SELECT u.id::text,u.patient_id::text,u.file_name,u.timestamp,p.name FROM inserted u JOIN profiles p ON p.id=u.patient_id`, u.PatientID, u.FileName))
}
func (s *postgresStore) AddHealing(ctx context.Context, h Healing) (Healing, error) {
	return scanHealing(s.db.QueryRowContext(ctx, `INSERT INTO healing_progress(patient_id,status_type,value)
 SELECT id,$2,$3 FROM profiles WHERE id=$1 AND role='patient' RETURNING id::text,patient_id::text,status_type,value,timestamp`, h.PatientID, h.StatusType, h.Value))
}
