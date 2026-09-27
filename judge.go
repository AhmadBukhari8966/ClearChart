package main

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"
)

// Judge mode is an opt-in (JUDGE_MODE=true) shortcut for hackathon judges.
// One click signs in as a shared demo doctor linked to every patient, and
// /judge shows that doctor beside the selected patient's own dashboard.
// Regular Supabase sign-in is unchanged.

const judgeEmail = "judge@clearchart.demo"

type judgePage struct{ DoctorID, CSRF string }

// JudgeWorkspace creates the demo doctor if needed, links it to every patient
// and returns its ID with the linked patients' emails keyed by profile ID.
// Rerunning it links patients who signed up since the last run.
func (s *postgresStore) JudgeWorkspace(ctx context.Context) (string, map[string]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO profiles(role,name,email,license_num,specialization)
 VALUES('doctor','Dr. Judge Demo',$1,'JUDGE-DEMO','Hackathon judging') ON CONFLICT DO NOTHING`, judgeEmail); err != nil {
		return "", nil, err
	}
	var doctor string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM profiles WHERE lower(email)=lower($1) AND role='doctor'`, judgeEmail).Scan(&doctor); err != nil {
		return "", nil, storageError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO care_team(patient_id,doctor_id) SELECT id,$1::uuid FROM profiles WHERE role='patient' ON CONFLICT DO NOTHING`, doctor); err != nil {
		return "", nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mock_biometric_data(doctor_id,heart_rate,sleep_hours)
 SELECT $1::uuid,64,7.4 WHERE NOT EXISTS (SELECT 1 FROM mock_biometric_data WHERE doctor_id=$1::uuid)`, doctor); err != nil {
		return "", nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.id::text,p.email FROM profiles p JOIN care_team c ON c.patient_id=p.id WHERE c.doctor_id=$1::uuid`, doctor)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	patients := make(map[string]string)
	for rows.Next() {
		var id, email string
		if err = rows.Scan(&id, &email); err != nil {
			return "", nil, err
		}
		patients[id] = email
	}
	if err = rows.Err(); err != nil {
		return "", nil, err
	}
	return doctor, patients, tx.Commit()
}

func (a *app) judgeEnabled() bool { return a.judgeDoctorID != "" }

// judgePatientSession lets a judge session act as the patient named by the
// request: the {id} path value (dashboard, events, scans) or an already
// parsed patient_id form field (uploads, check-ins). Only patients linked to
// the judge doctor qualify; anything else fails closed.
func (a *app) judgePatientSession(r *http.Request, s session) (session, bool) {
	id := r.PathValue("id")
	if id == "" && r.Form != nil {
		id = r.Form.Get("patient_id")
	}
	a.mu.Lock()
	email, ok := a.judgePatients[id]
	a.mu.Unlock()
	if !ok {
		return session{}, false
	}
	s.ProfileID, s.Role, s.Email = id, "patient", email
	return s, true
}

func (a *app) enterJudge(w http.ResponseWriter, r *http.Request) {
	if !a.judgeEnabled() {
		http.NotFound(w, r)
		return
	}
	data := AuthPage{Mode: "login"}
	if err := parseForm(w, r); err != nil {
		data.Error = "Please try again."
		a.authPage(w, r, data, 400)
		return
	}
	c, err := r.Cookie("clearchart_auth_csrf")
	if err != nil || c.Value == "" || !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.PostForm.Get("csrf"))) != 1 {
		data.Error = "This form expired. Please try again."
		a.authPage(w, r, data, 403)
		return
	}
	// Refresh links so patients who signed up since startup are included.
	doctor, patients, err := a.store.JudgeWorkspace(r.Context())
	if err != nil {
		data.Error = "Judge mode could not be prepared. Please try again."
		a.authPage(w, r, data, 503)
		return
	}
	a.mu.Lock()
	a.judgePatients = patients
	a.mu.Unlock()
	a.issueSession(w, r, session{Judge: true, ProfileID: doctor, Role: "doctor", Email: judgeEmail, Expires: time.Now().Add(4 * time.Hour)})
	a.cookie(w, r, "clearchart_auth_csrf", "", -1)
	http.Redirect(w, r, "/judge", http.StatusSeeOther)
}

func (a *app) judgeView(w http.ResponseWriter, r *http.Request) {
	s, ok := a.currentSession(r)
	if !ok || !s.Judge {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	a.page(w, "judge.html", judgePage{DoctorID: s.ProfileID, CSRF: s.CSRF})
}
