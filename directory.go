package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

// The doctor's patient directory renders at most directoryLimit rows. Anyone
// else is reached through the active search, which queries PostgreSQL and
// patches only #patient-directory-results, leaving the chart and drafts alone.
const directoryLimit = 50

type patientDirectory struct {
	DoctorID, SelectedID, Query string
	Patients                    []Profile
	Matches                     int // linked patients matching Query (all of them when Query is empty)
}

// Directory is the first page of the full directory for the initial render.
// A selected patient beyond the first page is pinned on top so it stays visible.
func (d Dashboard) Directory() patientDirectory {
	dir := patientDirectory{DoctorID: d.Profile.ID, SelectedID: d.Patient.ID, Matches: len(d.Patients)}
	if len(d.Patients) <= directoryLimit {
		dir.Patients = d.Patients
		return dir
	}
	dir.Patients = append([]Profile(nil), d.Patients[:directoryLimit]...)
	for _, p := range d.Patients[directoryLimit:] {
		if p.ID == d.Patient.ID {
			dir.Patients = append([]Profile{p}, dir.Patients[:directoryLimit-1]...)
			break
		}
	}
	return dir
}

// SearchPatients returns the doctor's linked patients whose name or email
// contains query, in directory order, with the total number of matches.
func (s *postgresStore) SearchPatients(ctx context.Context, doctorID, query string, limit int) ([]Profile, int, error) {
	pattern := ""
	if query != "" {
		pattern = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(query) + "%"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+profileColumns+`,count(*) OVER()
 FROM profiles p JOIN care_team c ON c.patient_id=p.id
 WHERE c.doctor_id=$1::uuid AND ($2='' OR p.name ILIKE $2 OR p.email ILIKE $2)
 ORDER BY p.name,p.id LIMIT $3`, doctorID, pattern, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var patients []Profile
	matches := 0
	for rows.Next() {
		var p Profile
		if err = rows.Scan(&p.ID, &p.AuthUserID, &p.Role, &p.Name, &p.Email, &p.LicenseNum, &p.Specialization, &p.DOB, &p.BloodType, &matches); err != nil {
			return nil, 0, err
		}
		patients = append(patients, p)
	}
	return patients, matches, rows.Err()
}

// searchPatients serves the directory's active search (Datastar @get).
func (a *app) searchPatients(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sessionFor(r, "doctor")
	if !ok {
		http.Error(w, "Session required.", 403)
		return
	}
	var signals struct {
		Query    string `json:"patientsearch"`
		Selected string `json:"selectedpatient"`
	}
	if raw := r.URL.Query().Get("datastar"); len(raw) > 4096 || (raw != "" && json.Unmarshal([]byte(raw), &signals) != nil) {
		http.Error(w, "Invalid search.", http.StatusBadRequest)
		return
	}
	query := strings.TrimSpace(signals.Query)
	if utf8.RuneCountInString(query) > 100 {
		query = string([]rune(query)[:100])
	}
	patients, matches, err := a.store.SearchPatients(r.Context(), s.ProfileID, query, directoryLimit)
	if err != nil {
		http.Error(w, "Search is unavailable right now.", 500)
		return
	}
	h, err := a.render("patient-directory", patientDirectory{DoctorID: s.ProfileID, SelectedID: signals.Selected, Query: query, Patients: patients, Matches: matches})
	if err != nil {
		http.Error(w, "Search is unavailable right now.", 500)
		return
	}
	startSSE(w)
	patch(w, "#patient-directory-results", "outer", h)
}
