package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	db, err := sql.Open("postgres", singleRoundTripDSN(dsn))
	if err != nil {
		return nil, err
	}
	// Keep every open connection idle-ready: a fresh TLS connection to a remote
	// database costs several round trips before the first query.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(10 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return &postgresStore{db: db}, nil
}
func (s *postgresStore) Close() error { return s.db.Close() }

// singleRoundTripDSN enables lib/pq's binary_parameters mode, which sends
// parse, bind and execute together: one network round trip per parameterized
// query instead of two. Only []byte arguments change encoding; none are used.
func singleRoundTripDSN(dsn string) string {
	if strings.Contains(dsn, "binary_parameters") {
		return dsn
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		if u, err := url.Parse(dsn); err == nil {
			q := u.Query()
			q.Set("binary_parameters", "yes")
			u.RawQuery = q.Encode()
			return u.String()
		}
		return dsn
	}
	return dsn + " binary_parameters=yes"
}
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

// profileJSON mirrors profileColumns for the aggregated dashboard query.
// encoding/json matches keys to Profile fields case-insensitively.
const profileJSON = `json_build_object('id',p.id,'authuserid',COALESCE(p.auth_user_id::text,''),'role',p.role,'name',p.name,'email',p.email,'licensenum',COALESCE(p.license_num,''),'specialization',COALESCE(p.specialization,''),'dob',COALESCE(to_char(p.dob,'YYYY-MM-DD'),''),'bloodtype',COALESCE(p.blood_type,''))`

// dashboardQuery loads a whole dashboard in one round trip. The selected
// patient is resolved in SQL: patients see themselves; doctors see the
// requested patient only when linked, otherwise their first patient by name.
// A requested but unlinked patient yields a null patient (ErrForbidden).
// $1 viewer profile ID, $2 role, $3 viewer email, $4 requested patient ID or empty.
const dashboardQuery = `WITH pts AS (
  SELECT p.*, row_number() OVER (ORDER BY p.name,p.id) AS rn
  FROM profiles p JOIN care_team c ON c.patient_id=p.id
  WHERE $2='doctor' AND c.doctor_id=$1::uuid
), flagged AS (
  -- Each linked patient's most concerning latest check-in, most severe first.
  SELECT DISTINCT ON (h.patient_id) h.patient_id,p.name,h.status_type,h.value,h.timestamp,
    CASE WHEN h.status_type='pain' THEN h.value ELSE 11-h.value END AS severity
  FROM (SELECT DISTINCT ON (hp.patient_id,hp.status_type) hp.* FROM healing_progress hp JOIN pts ON pts.id=hp.patient_id
    ORDER BY hp.patient_id,hp.status_type,hp.timestamp DESC,hp.id DESC) h
  JOIN pts p ON p.id=h.patient_id
  WHERE (h.status_type='pain' AND h.value>=6) OR (h.status_type<>'pain' AND h.value<=4)
  ORDER BY h.patient_id,severity DESC,h.timestamp DESC
), sel AS (
  SELECT CASE WHEN $2='patient' THEN $1::uuid
    WHEN $4='' THEN (SELECT id FROM pts WHERE rn=1)
    ELSE (SELECT id FROM pts WHERE id=NULLIF($4,'')::uuid) END AS id
)
SELECT json_build_object(
 'profile',(SELECT ` + profileJSON + ` FROM profiles p WHERE p.id=$1::uuid),
 'patients',COALESCE((SELECT json_agg(` + profileJSON + ` ORDER BY p.rn) FROM pts p),'[]'),
 'patient',(SELECT ` + profileJSON + ` FROM profiles p JOIN sel ON sel.id=p.id),
 'doctors',COALESCE((SELECT json_agg(` + profileJSON + ` ORDER BY p.name,p.id) FROM profiles p JOIN care_team c ON c.doctor_id=p.id JOIN sel ON sel.id=c.patient_id),'[]'),
 'records',COALESCE((SELECT json_agg(json_build_object('id',r.id,'patientid',r.patient_id,'doctorid',r.doctor_id,'type',r.type,'content',r.content,'timestamp',r.timestamp,'imageurl',COALESCE(r.image_url,''),'doctorname',d.name,'patientname',p.name,
   'categories',ARRAY(SELECT mc.category FROM medical_record_categories mc WHERE mc.record_id=r.id ORDER BY mc.category)) ORDER BY r.timestamp DESC,r.id DESC)
   FROM medical_records r JOIN sel ON sel.id=r.patient_id JOIN profiles d ON d.id=r.doctor_id JOIN profiles p ON p.id=r.patient_id),'[]'),
 'uploads',COALESCE((SELECT json_agg(json_build_object('id',u.id,'patientid',u.patient_id,'filename',u.file_name,'timestamp',u.timestamp,'patientname',p.name) ORDER BY u.timestamp DESC,u.id DESC)
   FROM patient_uploads u JOIN sel ON sel.id=u.patient_id JOIN profiles p ON p.id=u.patient_id),'[]'),
 'healing',COALESCE((SELECT json_agg(h ORDER BY h.statustype) FROM (SELECT DISTINCT ON (hp.status_type) hp.id,hp.patient_id AS patientid,hp.status_type AS statustype,hp.value,hp.timestamp
   FROM healing_progress hp JOIN sel ON sel.id=hp.patient_id ORDER BY hp.status_type,hp.timestamp DESC,hp.id DESC) h),'[]'),
 'reports',CASE WHEN $2='doctor' THEN COALESCE((SELECT json_agg(json_build_object('id',u.id,'patientid',u.patient_id,'filename',u.file_name,'timestamp',u.timestamp,'patientname',p.name) ORDER BY u.timestamp DESC,u.id DESC)
   FROM patient_uploads u JOIN pts p ON p.id=u.patient_id),'[]') ELSE '[]' END,
 'attention',CASE WHEN $2='doctor' THEN COALESCE((SELECT json_agg(json_build_object('patientid',f.patient_id,'name',f.name,'statustype',f.status_type,'value',f.value,'timestamp',f.timestamp) ORDER BY f.severity DESC,f.timestamp DESC)
   FROM (SELECT * FROM flagged ORDER BY severity DESC,timestamp DESC LIMIT 6) f),'[]') ELSE '[]' END,
 'attentioncount',CASE WHEN $2='doctor' THEN (SELECT count(*) FROM flagged) ELSE 0 END,
 'notificationcount',CASE WHEN $2='patient' THEN (SELECT count(*) FROM (SELECT 1 FROM care_invitations i JOIN profiles d ON d.id=i.doctor_id
     WHERE lower(i.email)=lower($3) AND i.status='pending' AND i.expires_at>now() LIMIT 50) x)
   ELSE (SELECT count(*) FROM (SELECT 1 FROM care_invitations i WHERE i.doctor_id=$1::uuid AND i.status IN ('accepted','declined')
     AND i.responded_at>=GREATEST(now()-interval '30 days',(SELECT COALESCE(v.notifications_seen_at,'-infinity') FROM profiles v WHERE v.id=$1::uuid)) LIMIT 50) x) END
)`

// DashboardBundle is every read a dashboard needs, loaded by one query.
type DashboardBundle struct {
	Profile, Patient  *Profile
	Patients, Doctors []Profile
	Records           []Record
	Uploads, Reports  []Upload
	Healing           []Healing
	Attention         []Attention
	AttentionCount    int
	NotificationCount int
}

func (s *postgresStore) Dashboard(ctx context.Context, viewer, role, email, patient string) (DashboardBundle, error) {
	var raw []byte
	var b DashboardBundle
	if err := s.db.QueryRowContext(ctx, dashboardQuery, viewer, role, email, patient).Scan(&raw); err != nil {
		return b, storageError(err)
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return b, err
	}
	if b.Profile == nil {
		return b, ErrNotFound
	}
	return b, nil
}

// Counts returns a patient's record and upload totals in one round trip.
func (s *postgresStore) Counts(ctx context.Context, patient string) (records, uploads int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM medical_records WHERE patient_id=$1),(SELECT count(*) FROM patient_uploads WHERE patient_id=$1)`, patient).Scan(&records, &uploads)
	return records, uploads, err
}

func scanRecord(row rowScanner) (Record, error) {
	var r Record
	err := row.Scan(&r.ID, &r.PatientID, &r.DoctorID, &r.Type, &r.Content, &r.Timestamp, &r.ImageURL, &r.DoctorName, &r.PatientName, pq.Array(&r.Categories))
	return r, storageError(err)
}

const recordColumns = `r.id::text,r.patient_id::text,r.doctor_id::text,r.type,r.content,r.timestamp,COALESCE(r.image_url,''),d.name,p.name,
 ARRAY(SELECT mc.category FROM medical_record_categories mc WHERE mc.record_id=r.id ORDER BY mc.category)`

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
// MarkNotificationsSeen resets the doctor's unread count; the panel still
// lists the last 30 days of activity.
func (s *postgresStore) MarkNotificationsSeen(ctx context.Context, profile string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE profiles SET notifications_seen_at=now() WHERE id=$1`, profile)
	return err
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
	// Authorization and insertion share one statement; explicit categories are
	// stored in the same transaction. ID and timestamp use DB defaults.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback()
	result, err := scanRecord(tx.QueryRowContext(ctx, `WITH inserted AS (
  INSERT INTO medical_records(patient_id,doctor_id,type,content,image_url)
  SELECT c.patient_id,c.doctor_id,$3,$4,NULLIF($5,'') FROM care_team c WHERE c.patient_id=$1 AND c.doctor_id=$2
  RETURNING *) SELECT `+recordColumns+` FROM inserted r JOIN profiles d ON d.id=r.doctor_id JOIN profiles p ON p.id=r.patient_id`, r.PatientID, r.DoctorID, r.Type, r.Content, r.ImageURL))
	if errors.Is(err, ErrNotFound) {
		return Record{}, ErrForbidden
	}
	if err != nil {
		return Record{}, err
	}
	if len(r.Categories) > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO medical_record_categories(record_id,category) SELECT $1::uuid,unnest($2::text[])`, result.ID, pq.Array(r.Categories)); err != nil {
			return Record{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Record{}, err
	}
	result.Categories = r.Categories
	return result, nil
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
