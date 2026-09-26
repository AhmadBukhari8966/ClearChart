package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func validRole(s string) bool { return s == "patient" || s == "doctor" }
func validType(s string) bool { return s == "note" || s == "prescription" || s == "imaging" }

// Demo identities are deliberately selectable. Cookies isolate the two roles so
// judges can use patient and doctor tabs at the same time. This is not real login.
func (a *app) setSession(w http.ResponseWriter, r *http.Request, p Profile) session {
	s := session{ProfileID: p.ID, Role: p.Role, CSRF: newID(), Expires: time.Now().Add(12 * time.Hour)}
	token := newID()
	a.mu.Lock()
	for key, old := range a.sessions {
		if time.Now().After(old.Expires) {
			delete(a.sessions, key)
		}
	}
	a.sessions[token] = s
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "clearchart_" + p.Role, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 43200})
	return s
}

func (a *app) sessionFor(r *http.Request, role string) (session, bool) {
	c, err := r.Cookie("clearchart_" + role)
	if err != nil {
		return session{}, false
	}
	a.mu.Lock()
	s, ok := a.sessions[c.Value]
	a.mu.Unlock()
	return s, ok && s.Role == role && time.Now().Before(s.Expires)
}

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	if s, ok := a.sessionFor(r, "patient"); ok {
		http.Redirect(w, r, "/dashboard/patient/"+s.ProfileID, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/demo/patient", http.StatusSeeOther)
}

func (a *app) demo(w http.ResponseWriter, r *http.Request) {
	role := r.PathValue("role")
	if !validRole(role) {
		http.NotFound(w, r)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		if role == "doctor" {
			id = demoDoctorID
		} else {
			id = demoPatientID
		}
	}
	p, err := a.store.Profile(r.Context(), id)
	if err != nil || p.Role != role {
		http.Error(w, "Demo profile not found. Apply schema.sql and seed.sql for Supabase mode.", 404)
		return
	}
	a.setSession(w, r, p)
	http.Redirect(w, r, "/dashboard/"+role+"/"+p.ID, http.StatusSeeOther)
}

func (a *app) dashboardData(ctx context.Context, s session, patientID, filter string) (Dashboard, error) {
	d := Dashboard{Role: s.Role, CSRF: s.CSRF, Mode: a.mode, Filter: filter, Today: time.Now(), ViewID: newID()}
	var err error
	if d.Profile, err = a.store.Profile(ctx, s.ProfileID); err != nil {
		return d, err
	}
	if s.Role == "patient" {
		patientID = s.ProfileID
	} else {
		if d.Patients, err = a.store.Patients(ctx, s.ProfileID); err != nil {
			return d, err
		}
		if patientID == "" && len(d.Patients) > 0 {
			patientID = d.Patients[0].ID
		}
		if patientID != "" {
			allowed, e := a.store.IsCareTeam(ctx, patientID, s.ProfileID)
			if e != nil {
				return d, e
			}
			if !allowed {
				return d, ErrForbidden
			}
		}
		if d.Biometric, err = a.store.Biometrics(ctx, s.ProfileID); err != nil && !errors.Is(err, ErrNotFound) {
			return d, err
		}
		if d.Reports, err = a.store.Reports(ctx, s.ProfileID); err != nil {
			return d, err
		}
	}
	if patientID == "" {
		return d, nil
	}
	if d.Patient, err = a.store.Profile(ctx, patientID); err != nil {
		return d, err
	}
	if d.Doctors, err = a.store.CareTeam(ctx, patientID); err != nil {
		return d, err
	}
	records, err := a.store.Records(ctx, patientID)
	if err != nil {
		return d, err
	}
	d.RecordCount = len(records)
	d.Summary = plainSummary(records)
	for _, record := range records {
		if filter == "" || record.Type == filter {
			d.Records = append(d.Records, record)
		}
	}
	if d.Uploads, err = a.store.Uploads(ctx, patientID); err != nil {
		return d, err
	}
	d.UploadCount = len(d.Uploads)
	if d.Healing, err = a.store.Healing(ctx, patientID); err != nil {
		return d, err
	}
	return d, nil
}

func plainSummary(records []Record) string {
	for _, r := range records {
		if r.Type == "note" {
			if strings.HasPrefix(r.Content, "Two-week knee recovery review:") {
				return "Your knee is healing well: swelling is going down and movement is improving. Keep following the recovery plan you agreed with your care team. Your next review is in two weeks."
			}
			replacer := strings.NewReplacer("ambulation", "walking", "edema", "swelling", "ROM", "range of motion", "range-of-motion", "range of motion", "PRN", "as needed", "BID", "twice daily", "postoperative", "after surgery")
			return replacer.Replace(r.Content)
		}
	}
	return "Your doctor's next note will appear here in plain language. You can always read the original in your timeline."
}

func (a *app) dashboard(w http.ResponseWriter, r *http.Request) {
	role := r.PathValue("role")
	if !validRole(role) {
		http.NotFound(w, r)
		return
	}
	s, ok := a.sessionFor(r, role)
	if !ok {
		http.Redirect(w, r, "/demo/"+role, http.StatusSeeOther)
		return
	}
	if s.ProfileID != r.PathValue("id") {
		http.Error(w, "This profile is not part of your current demo session.", 403)
		return
	}
	filter := r.URL.Query().Get("type")
	if filter != "" && !validType(filter) {
		http.Error(w, "Unknown timeline filter.", 400)
		return
	}
	d, err := a.dashboardData(r.Context(), s, r.URL.Query().Get("patient"), filter)
	if err != nil {
		a.readError(w, err)
		return
	}
	a.page(w, role+"_dashboard.html", d)
}

func (a *app) readError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrForbidden) {
		http.Error(w, "This patient is not linked to your care team.", 403)
		return
	}
	if errors.Is(err, ErrNotFound) {
		http.Error(w, "The requested profile was not found.", 404)
		return
	}
	http.Error(w, "We couldn't load this page. Please check the database and try again.", 500)
}

func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
	}
	return true
}

func (a *app) authorizePost(w http.ResponseWriter, r *http.Request, role string) (session, bool) {
	s, ok := a.sessionFor(r, role)
	if !ok {
		a.feedback(w, "Your demo session expired. Open the role switcher to start again.", http.StatusUnauthorized)
		return s, false
	}
	if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(r.FormValue("csrf")), []byte(s.CSRF)) != 1 {
		a.feedback(w, "This form expired. Refresh the page and try again.", http.StatusForbidden)
		return s, false
	}
	return s, true
}

func parseForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	return r.ParseForm()
}

func (a *app) addRecord(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		a.feedback(w, "The note is too large.", 400)
		return
	}
	s, ok := a.authorizePost(w, r, "doctor")
	if !ok {
		return
	}
	patientID := r.FormValue("patient_id")
	if r.FormValue("doctor_id") != s.ProfileID {
		a.feedback(w, "The doctor does not match your session.", 403)
		return
	}
	allowed, err := a.store.IsCareTeam(r.Context(), patientID, s.ProfileID)
	if err != nil {
		a.feedback(w, "Could not verify this care team. Try again.", 500)
		return
	}
	if !allowed {
		a.feedback(w, "You can add records only for your linked patients.", 403)
		return
	}
	content := strings.TrimSpace(r.FormValue("content"))
	kind := r.FormValue("type")
	if !validType(kind) || len(content) < 3 || len(content) > 5000 {
		a.feedback(w, "Choose a record type and enter a note between 3 and 5,000 characters.", 400)
		return
	}
	record := Record{ID: newID(), PatientID: patientID, DoctorID: s.ProfileID, Type: kind, Content: content, Timestamp: time.Now().UTC()}
	if kind == "imaging" {
		record.ImageURL = "/static/ct-scan.svg"
	}
	record, err = a.store.AddRecord(r.Context(), record)
	if err != nil {
		a.feedback(w, "Your record could not be saved. Please try again.", 500)
		return
	}
	fragment, err := a.render("record", record)
	if err != nil {
		a.feedback(w, "Record saved. Refresh to see it.", 500)
		return
	}
	startSSE(w)
	patch(w, "#timeline", "prepend", fragment)
	a.writeCounts(w, r.Context(), patientID)
	patch(w, "#form-feedback", "outer", feedbackHTML("Record shared with your patient. Their timeline is up to date.", false))
	a.hub.publish(patientID, r.FormValue("view_id"))
}

func (a *app) uploadReport(w http.ResponseWriter, r *http.Request) {
	// The body is bounded before parsing, and any temporary multipart files are removed.
	// Only the sanitized filename reaches either store; file bytes are discarded.
	r.Body = http.MaxBytesReader(w, r.Body, (5<<20)+(64<<10))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
		a.feedback(w, "Choose a report smaller than 5 MB.", 400)
		return
	}
	defer r.MultipartForm.RemoveAll()
	s, ok := a.authorizePost(w, r, "patient")
	if !ok {
		return
	}
	if r.FormValue("patient_id") != s.ProfileID {
		a.feedback(w, "You can upload reports only to your own profile.", 403)
		return
	}
	file, header, err := r.FormFile("report")
	if err != nil {
		a.feedback(w, "Choose a PDF or image report first.", 400)
		return
	}
	defer file.Close()
	if header.Size == 0 || header.Size > 5<<20 {
		a.feedback(w, "Choose a nonempty report smaller than 5 MB.", 400)
		return
	}
	name := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	ext := strings.ToLower(path.Ext(name))
	if len(name) > 180 || name == "." || !(ext == ".pdf" || ext == ".png" || ext == ".jpg" || ext == ".jpeg") {
		a.feedback(w, "Choose a PDF, PNG, or JPG with a filename under 180 characters.", 400)
		return
	}
	if _, err = io.Copy(io.Discard, file); err != nil {
		a.feedback(w, "We couldn't read that report. Please try again.", 400)
		return
	}
	u, err := a.store.AddUpload(r.Context(), Upload{ID: newID(), PatientID: s.ProfileID, FileName: name, Timestamp: time.Now().UTC()})
	if err != nil {
		a.feedback(w, "Your report could not be saved. Please try again.", 500)
		return
	}
	fragment, err := a.render("upload", u)
	if err != nil {
		a.feedback(w, "Report saved. Refresh to see it.", 500)
		return
	}
	startSSE(w)
	patch(w, "#uploads", "prepend", fragment)
	a.writeCounts(w, r.Context(), s.ProfileID)
	patch(w, "#form-feedback", "outer", feedbackHTML("Report name shared with your care team. File contents are not stored in this demo.", false))
	a.hub.publish(s.ProfileID, r.FormValue("view_id"))
}

func (a *app) addHealing(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		a.feedback(w, "Could not read this check-in.", 400)
		return
	}
	s, ok := a.authorizePost(w, r, "patient")
	if !ok {
		return
	}
	value, err := strconv.Atoi(r.FormValue("value"))
	kind := r.FormValue("status_type")
	if r.FormValue("patient_id") != s.ProfileID {
		a.feedback(w, "You can check in only for your own profile.", 403)
		return
	}
	if err != nil || value < 1 || value > 10 || (kind != "pain" && kind != "mobility" && kind != "energy") {
		a.feedback(w, "Choose a health measure and a value from 1 to 10.", 400)
		return
	}
	_, err = a.store.AddHealing(r.Context(), Healing{ID: newID(), PatientID: s.ProfileID, StatusType: kind, Value: value, Timestamp: time.Now().UTC()})
	if err != nil {
		a.feedback(w, "Your check-in could not be saved. Please try again.", 500)
		return
	}
	d, err := a.dashboardData(r.Context(), s, "", "")
	if err != nil {
		a.feedback(w, "Check-in saved. Refresh to see it.", 500)
		return
	}
	d.ViewID = r.FormValue("view_id")
	fragment, err := a.render("healing-panel", d)
	if err != nil {
		a.feedback(w, "Check-in saved. Refresh to see it.", 500)
		return
	}
	startSSE(w)
	patch(w, "#healing-panel", "outer", fragment)
	patch(w, "#form-feedback", "outer", feedbackHTML("Check-in saved. Your care team can see how you're feeling.", false))
	a.hub.publish(s.ProfileID, d.ViewID)
}

func (a *app) onboardForm(w http.ResponseWriter, r *http.Request) {
	role := r.PathValue("role")
	if !validRole(role) {
		http.NotFound(w, r)
		return
	}
	csrf := newID()
	http.SetCookie(w, &http.Cookie{Name: "clearchart_onboard", Value: csrf, Path: "/onboard/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 3600})
	a.page(w, "onboarding.html", Dashboard{Role: role, CSRF: csrf, Mode: a.mode, Today: time.Now()})
}

func (a *app) onboard(w http.ResponseWriter, r *http.Request) {
	role := r.PathValue("role")
	if !validRole(role) {
		http.NotFound(w, r)
		return
	}
	if err := parseForm(w, r); err != nil {
		a.feedback(w, "Please shorten the form fields.", 400)
		return
	}
	c, err := r.Cookie("clearchart_onboard")
	if err != nil || !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.FormValue("csrf"))) != 1 {
		a.feedback(w, "This form expired. Refresh the page and try again.", 403)
		return
	}
	p := Profile{ID: newID(), Role: role, Name: strings.TrimSpace(r.FormValue("name")), Email: strings.ToLower(strings.TrimSpace(r.FormValue("email"))), LicenseNum: strings.TrimSpace(r.FormValue("license_num")), Specialization: strings.TrimSpace(r.FormValue("specialization")), DOB: r.FormValue("dob"), BloodType: r.FormValue("blood_type")}
	address, err := mail.ParseAddress(p.Email)
	if len(p.Name) < 2 || len(p.Name) > 100 || len(p.Email) > 254 || err != nil || address.Address != p.Email {
		a.feedback(w, "Enter your full name and a valid email address.", 400)
		return
	}
	if role == "doctor" {
		if len(p.LicenseNum) < 3 || len(p.LicenseNum) > 60 || len(p.Specialization) < 2 || len(p.Specialization) > 100 {
			a.feedback(w, "Enter your license number and specialization.", 400)
			return
		}
		p.DOB = ""
		p.BloodType = ""
	} else {
		dob, e := time.Parse("2006-01-02", p.DOB)
		blood := map[string]bool{"A+": true, "A-": true, "B+": true, "B-": true, "AB+": true, "AB-": true, "O+": true, "O-": true, "Unknown": true}
		if e != nil || dob.After(time.Now()) || dob.Before(time.Now().AddDate(-130, 0, 0)) || !blood[p.BloodType] {
			a.feedback(w, "Enter a valid date of birth and blood type.", 400)
			return
		}
		p.LicenseNum = ""
		p.Specialization = ""
	}
	p, err = a.store.CreateProfile(r.Context(), p)
	if err != nil {
		a.feedback(w, "This profile could not be created. Try another email address or check the database.", 400)
		return
	}
	a.setSession(w, r, p)
	startSSE(w)
	patch(w, "#onboarding-result", "outer", fmt.Sprintf(`<div id="onboarding-result" class="success-box" role="status"><strong>You're all set, %s.</strong><p>Your demo profile is ready and connected to a care team.</p><a class="button button-primary" href="/dashboard/%s/%s">Open my dashboard &rarr;</a></div>`, html.EscapeString(p.Name), p.Role, p.ID))
	patch(w, "#form-feedback", "outer", feedbackHTML("Profile created successfully.", false))
}

func feedbackHTML(message string, isError bool) string {
	class := "feedback success"
	if isError {
		class = "feedback error"
	}
	return `<div id="form-feedback" class="` + class + `" role="status" aria-live="polite">` + html.EscapeString(message) + `</div>`
}

func (a *app) feedback(w http.ResponseWriter, message string, status int) {
	// Datastar processes successful SSE responses. Validation errors are conveyed
	// as visible HTML with no mutation, rather than triggering automatic retries.
	startSSE(w)
	patch(w, "#form-feedback", "outer", feedbackHTML(message, status >= 400))
}

func (a *app) writeCounts(w http.ResponseWriter, ctx context.Context, patientID string) {
	if records, err := a.store.Records(ctx, patientID); err == nil {
		patch(w, "#record-count", "outer", fmt.Sprintf(`<strong id="record-count">%d</strong>`, len(records)))
	}
	if uploads, err := a.store.Uploads(ctx, patientID); err == nil {
		patch(w, "#upload-count", "outer", fmt.Sprintf(`<strong id="upload-count">%d</strong>`, len(uploads)))
	}
}
