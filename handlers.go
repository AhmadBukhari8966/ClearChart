package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func validRole(s string) bool { return s == "patient" || s == "doctor" }
func validType(s string) bool { return s == "note" || s == "prescription" || s == "imaging" }

// dashboardData loads the whole dashboard with a single database round trip.
func (a *app) dashboardData(ctx context.Context, s session, patientID string, filter timelineFilter) (Dashboard, error) {
	d := Dashboard{Role: s.Role, CSRF: s.CSRF, Mode: a.mode, Filter: filter, Today: time.Now(), ViewID: newID()}
	if s.Role == "patient" {
		patientID = ""
	} else if patientID != "" && !validUUID(patientID) {
		return d, ErrForbidden
	}
	b, err := a.store.Dashboard(ctx, s.ProfileID, s.Role, s.Email, patientID)
	if err != nil {
		return d, err
	}
	if b.Patient == nil && patientID != "" {
		return d, ErrForbidden
	}
	d.Profile = *b.Profile
	d.Patients, d.Reports, d.NotificationCount = b.Patients, b.Reports, b.NotificationCount
	d.Attention, d.AttentionCount = b.Attention, b.AttentionCount
	if b.Patient == nil {
		return d, nil
	}
	d.Patient, d.Doctors, d.Uploads, d.Healing = *b.Patient, b.Doctors, b.Uploads, b.Healing
	d.RecordCount, d.UploadCount = len(b.Records), len(b.Uploads)
	if s.Role == "patient" {
		d.Summary = plainSummary(b.Records)
	}
	for _, record := range b.Records {
		if filter.matches(record) {
			d.Records = append(d.Records, record)
		}
	}
	return d, nil
}

var plainLanguage = strings.NewReplacer("ambulation", "walking", "edema", "swelling", "ROM", "range of motion", "range-of-motion", "range of motion", "PRN", "as needed", "BID", "twice daily", "postoperative", "after surgery")

func plainSummary(records []Record) string {
	for _, r := range records {
		if r.Type == "note" {
			return plainLanguage.Replace(r.Content)
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
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if s.ProfileID != r.PathValue("id") {
		http.Error(w, "This profile does not belong to your account.", 403)
		return
	}
	filter := timelineFilter{Type: r.URL.Query().Get("type"), Category: r.URL.Query().Get("category")}
	if !filter.valid() {
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
		a.feedback(w, "Your session expired. Sign in again at /login.", http.StatusUnauthorized)
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
	if !validUUID(patientID) {
		a.feedback(w, "You can add records only for your linked patients.", 403)
		return
	}
	content := strings.TrimSpace(r.FormValue("content"))
	kind := r.FormValue("type")
	if !validType(kind) || len(content) < 3 || len(content) > 5000 {
		a.feedback(w, "Choose a record type and enter a note between 3 and 5,000 characters.", 400)
		return
	}
	categories, ok := normalizeCategories(r.Form["category"])
	if !ok {
		a.feedback(w, "Choose body areas from the list shown.", 400)
		return
	}
	record := Record{PatientID: patientID, DoctorID: s.ProfileID, Type: kind, Content: content, Categories: categories}
	if kind == "imaging" {
		record.ImageURL = "/static/ct-scan.svg"
	}
	// AddRecord authorizes the care-team link in the same statement as the insert.
	if _, err := a.store.AddRecord(r.Context(), record); errors.Is(err, ErrForbidden) {
		a.feedback(w, "You can add records only for your linked patients.", 403)
		return
	} else if err != nil {
		a.feedback(w, "Your record could not be saved. Please try again.", 500)
		return
	}
	// Re-render with the doctor's active filters so a new record that does not
	// match them is not shown under the wrong filter.
	filter := timelineFilter{Type: r.FormValue("filter_type"), Category: r.FormValue("filter_category")}
	if !filter.valid() {
		filter = timelineFilter{}
	}
	d, err := a.dashboardData(r.Context(), s, patientID, filter)
	if err != nil {
		a.feedback(w, "Record saved. Refresh to see it.", 500)
		return
	}
	startSSE(w)
	a.writeTimeline(w, d)
	writeCounts(w, d.RecordCount, d.UploadCount)
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
	u, err := a.store.AddUpload(r.Context(), Upload{PatientID: s.ProfileID, FileName: name})
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
	if records, uploads, err := a.store.Counts(r.Context(), s.ProfileID); err == nil {
		writeCounts(w, records, uploads)
	}
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
	_, err = a.store.AddHealing(r.Context(), Healing{PatientID: s.ProfileID, StatusType: kind, Value: value})
	if err != nil {
		a.feedback(w, "Your check-in could not be saved. Please try again.", 500)
		return
	}
	// The healing panel needs only the latest check-ins, not a full dashboard.
	healing, err := a.store.Healing(r.Context(), s.ProfileID)
	if err != nil {
		a.feedback(w, "Check-in saved. Refresh to see it.", 500)
		return
	}
	me := Profile{ID: s.ProfileID, Role: s.Role}
	d := Dashboard{Profile: me, Patient: me, Role: s.Role, CSRF: s.CSRF, Healing: healing, ViewID: r.FormValue("view_id")}
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
	s, ok := a.currentSession(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if s.ProfileID != "" {
		http.Redirect(w, r, sessionDestination(s), http.StatusSeeOther)
		return
	}
	a.page(w, "onboarding.html", Dashboard{Role: role, CSRF: s.CSRF, Profile: Profile{Email: s.Email}, Mode: a.mode, Today: time.Now()})
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
	s, ok := a.currentSession(r)
	if !ok || s.AuthUserID == "" {
		a.feedback(w, "Sign in before creating your profile.", 401)
		return
	}
	if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(s.CSRF), []byte(r.PostForm.Get("csrf"))) != 1 {
		a.feedback(w, "This form expired. Refresh the page and try again.", 403)
		return
	}
	if s.ProfileID != "" {
		a.feedback(w, "You already have a profile. Open your dashboard.", 409)
		return
	}
	if _, err := a.store.ProfileByAuthUserID(r.Context(), s.AuthUserID); !errors.Is(err, ErrNotFound) {
		a.feedback(w, "Your account already has a profile or could not be checked. Sign in again.", 409)
		return
	}
	p := Profile{AuthUserID: s.AuthUserID, Role: role, Name: strings.TrimSpace(r.PostForm.Get("name")), Email: s.Email, LicenseNum: strings.TrimSpace(r.PostForm.Get("license_num")), Specialization: strings.TrimSpace(r.PostForm.Get("specialization")), DOB: r.PostForm.Get("dob"), BloodType: r.PostForm.Get("blood_type")}
	if len(p.Name) < 2 || len(p.Name) > 100 {
		a.feedback(w, "Enter your full name (2?100 characters).", 400)
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
	p, err := a.store.CreateProfile(r.Context(), p)
	if err != nil {
		a.feedback(w, "This profile could not be created. If this email already has a profile, ask the workspace administrator to link your account.", 400)
		return
	}
	s.ProfileID, s.Role = p.ID, p.Role
	a.issueSession(w, r, s)
	startSSE(w)
	patch(w, "#onboarding-result", "outer", fmt.Sprintf(`<div id="onboarding-result" class="success-box" role="status"><strong>You're all set, %s.</strong><p>Your profile is ready. Care-team connections are assigned separately.</p><a class="button button-primary" href="/">Continue to my workspace &rarr;</a></div>`, html.EscapeString(p.Name)))
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

func writeCounts(w http.ResponseWriter, records, uploads int) {
	patch(w, "#record-count", "outer", fmt.Sprintf(`<strong id="record-count">%d</strong>`, records))
	patch(w, "#upload-count", "outer", fmt.Sprintf(`<strong id="upload-count">%d</strong>`, uploads))
}

// Images are generated locally from an SVG illustration and tagged to their
// fictional patient's chart. There are no remote images or real scan files.
func (a *app) mockScan(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")
	number, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("scan"), ".svg"))
	if err != nil || number < 1 || number > 2 || !validUUID(patientID) {
		http.NotFound(w, r)
		return
	}
	allowed := false
	if s, ok := a.sessionFor(r, "patient"); ok && s.ProfileID == patientID {
		allowed = true
	}
	var p Profile
	if allowed {
		p, err = a.store.Profile(r.Context(), patientID)
	} else if s, ok := a.sessionFor(r, "doctor"); ok {
		err = parallel(
			func() (e error) { allowed, e = a.store.IsCareTeam(r.Context(), patientID, s.ProfileID); return },
			func() (e error) { p, e = a.store.Profile(r.Context(), patientID); return },
		)
		if errors.Is(err, ErrNotFound) {
			err = nil // an unlinked doctor gets 403, not a profile-existence signal
		}
	}
	if err != nil {
		a.readError(w, err)
		return
	}
	if !allowed {
		http.Error(w, "This scan is not part of your current care team.", http.StatusForbidden)
		return
	}
	if p.Role != "patient" {
		http.NotFound(w, r)
		return
	}
	// The deterministic variation gives each series a distinct appearance while
	// keeping every image unmistakably an illustration for UI testing.
	variant := number * 11
	for _, ch := range patientID {
		variant += int(ch)
	}
	data := struct {
		Patient       Profile
		Number, Angle int
		Reference     string
	}{p, number, variant%31 - 15, fmt.Sprintf("CC-%s-%02d", patientID[len(patientID)-6:], number)}
	fragment, err := a.render("mock_scan.svg", data)
	if err != nil {
		http.Error(w, "Could not render the mock scan.", 500)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	// Deterministic output: let the browser reuse it instead of refetching per render.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	fmt.Fprint(w, fragment)
}
