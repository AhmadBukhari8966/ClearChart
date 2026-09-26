package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) (*app, http.Handler) {
	t.Helper()
	a, err := newApp(NewMemoryStore(), "Test database")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.hub.close(); a.store.Close() })
	return a, a.routes()
}

// fixtureProfile is test-only: runtime authentication never looks up identities by email.
func fixtureProfile(t *testing.T, store Store, role string) Profile {
	t.Helper()
	email := demoPatientEmail
	if role == "doctor" {
		email = demoDoctorEmail
	}
	var p Profile
	var err error
	switch s := store.(type) {
	case *memoryStore:
		p, err = s.ProfileByEmail(context.Background(), email)
	case *postgresStore:
		p, err = scanProfile(s.db.QueryRowContext(context.Background(), "SELECT "+profileColumns+" FROM profiles p WHERE lower(p.email)=lower($1)", email))
	default:
		t.Fatal("unsupported test store")
	}
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func demoSession(t *testing.T, a *app, handler http.Handler, role string) ([]*http.Cookie, session, string) {
	t.Helper()
	p := fixtureProfile(t, a.store, role)
	w := httptest.NewRecorder()
	s := a.issueSession(w, httptest.NewRequest(http.MethodGet, "/", nil), session{AuthUserID: p.AuthUserID, Email: p.Email, ProfileID: p.ID, Role: p.Role, Expires: time.Now().Add(time.Hour)})
	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Fatal("session cookie flags missing")
		}
	}
	return cookies, s, sessionDestination(s)
}

func serveRequest(handler http.Handler, r *http.Request, cookies []*http.Cookie) *httptest.ResponseRecorder {
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func postForm(handler http.Handler, path string, values url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return serveRequest(handler, r, cookies)
}

func assertSSE(t *testing.T, w *httptest.ResponseRecorder, selector, mode string) {
	t.Helper()
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected successful SSE response; status=%d, content-type=%q, body=%s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	for _, want := range []string{"event: datastar-patch-elements\n", "data: selector " + selector + "\n", "data: mode " + mode + "\n", "data: elements "} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("SSE response missing %q", want)
		}
	}
}

func TestDashboardsRenderForSeparateAccounts(t *testing.T) {
	a, handler := testApp(t)
	cookies := make(map[string][]*http.Cookie)
	paths := make(map[string]string)
	for _, role := range []string{"patient", "doctor"} {
		roleCookies, _, path := demoSession(t, a, handler, role)
		cookies[role] = roleCookies
		paths[role] = path
	}
	scripts := regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	inlineHandlers := regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	for _, role := range []string{"patient", "doctor"} {
		t.Run(role, func(t *testing.T) {
			w := serveRequest(handler, httptest.NewRequest(http.MethodGet, paths[role], nil), cookies[role])
			if w.Code != http.StatusOK {
				t.Fatalf("dashboard: status %d: %s", w.Code, w.Body.String())
			}
			body := w.Body.String()
			if strings.Contains(body, "ZgotmplZ") || strings.Contains(body, "<no value>") {
				t.Fatal("dashboard contains a failed template interpolation")
			}
			for _, tag := range scripts.FindAllStringSubmatch(body, -1) {
				if !strings.Contains(tag[1], "src=") || strings.TrimSpace(tag[2]) != "" {
					t.Errorf("dashboard includes custom inline JavaScript: %s", tag[0])
				}
			}
			if inlineHandlers.MatchString(body) {
				t.Error("dashboard includes native inline JavaScript event handlers")
			}
			liveAttribute := "data-init="
			if role == "doctor" {
				liveAttribute = "data-effect="
			}
			for _, want := range []string{`id="timeline"`, liveAttribute, "/events/" + role + "/"} {
				if !strings.Contains(body, want) {
					t.Errorf("dashboard missing %q", want)
				}
			}
		})
	}
	foreign := "/dashboard/patient/00000000-0000-4000-8000-000000000102"
	w := serveRequest(handler, httptest.NewRequest(http.MethodGet, foreign, nil), cookies["patient"])
	if w.Code != http.StatusForbidden {
		t.Errorf("foreign patient dashboard returned %d, want 403", w.Code)
	}
	w = serveRequest(handler, httptest.NewRequest(http.MethodGet, paths["doctor"]+"?patient=00000000-0000-4000-8000-000000000103", nil), cookies["doctor"])
	if w.Code != http.StatusForbidden {
		t.Errorf("unlinked patient dashboard returned %d, want 403", w.Code)
	}
}

func TestDoctorRecordEscapedAndVisibleToPatient(t *testing.T) {
	a, handler := testApp(t)
	doctorCookies, doctor, _ := demoSession(t, a, handler, "doctor")
	patientCookies, _, patientPath := demoSession(t, a, handler, "patient")
	before, err := a.store.Records(context.Background(), demoPatientID)
	if err != nil {
		t.Fatal(err)
	}
	content := `Progress <script>alert("demo")</script> & gentle movement.`
	w := postForm(handler, "/add-record", url.Values{
		"csrf": {doctor.CSRF}, "doctor_id": {doctor.ProfileID}, "patient_id": {demoPatientID},
		"type": {"note"}, "content": {content}, "view_id": {"doctor-tab"},
	}, doctorCookies)
	assertSSE(t, w, "#timeline", "prepend")
	if strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatal("record content was not escaped in the SSE fragment")
	}
	after, err := a.store.Records(context.Background(), demoPatientID)
	if err != nil || len(after) != len(before)+1 {
		t.Fatalf("record was not persisted once: before=%d after=%d err=%v", len(before), len(after), err)
	}
	if after[0].Content != content || after[0].DoctorID != doctor.ProfileID {
		t.Fatal("saved record lost its content or doctor attribution")
	}
	patientPage := serveRequest(handler, httptest.NewRequest(http.MethodGet, patientPath, nil), patientCookies)
	if patientPage.Code != http.StatusOK || !strings.Contains(patientPage.Body.String(), "&lt;script&gt;") {
		t.Fatal("new doctor record was not visible in the patient's escaped timeline")
	}
}

func TestRejectedRecordsDoNotWrite(t *testing.T) {
	tests := []struct {
		name, role, csrf, patientID, doctorID, origin string
	}{
		{name: "invalid CSRF", role: "doctor", csrf: "wrong"},
		{name: "patient cannot write doctor notes", role: "patient"},
		{name: "unlinked patient", role: "doctor", patientID: "00000000-0000-4000-8000-000000000103"},
		{name: "impersonated doctor", role: "doctor", doctorID: "00000000-0000-4000-8000-000000000002"},
		{name: "cross origin", role: "doctor", origin: "https://foreign.example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a, handler := testApp(t)
			cookies, s, _ := demoSession(t, a, handler, test.role)
			patientID, doctorID, csrf := test.patientID, test.doctorID, test.csrf
			if patientID == "" {
				patientID = demoPatientID
			}
			if doctorID == "" {
				doctorID = demoDoctorID
			}
			if csrf == "" {
				csrf = s.CSRF
			}
			before, _ := a.store.Records(context.Background(), patientID)
			values := url.Values{"csrf": {csrf}, "patient_id": {patientID}, "doctor_id": {doctorID}, "type": {"note"}, "content": {"Must not be saved"}}
			r := httptest.NewRequest(http.MethodPost, "/add-record", strings.NewReader(values.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			w := serveRequest(handler, r, cookies)
			assertSSE(t, w, "#form-feedback", "outer")
			if !strings.Contains(w.Body.String(), "feedback error") || strings.Contains(w.Body.String(), "data: mode prepend") {
				t.Fatal("invalid mutation was not rejected visibly")
			}
			after, _ := a.store.Records(context.Background(), patientID)
			if len(after) != len(before) {
				t.Fatal("rejected request changed the records")
			}
		})
	}
}

func uploadRequest(t *testing.T, fields url.Values, filename string, contents []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, values := range fields {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	file, err := writer.CreateFormFile("report", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/upload-report", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func TestUploadStoresFilenameAndSharesWithCareTeam(t *testing.T) {
	a, handler := testApp(t)
	patientCookies, patient, _ := demoSession(t, a, handler, "patient")
	doctorCookies, _, doctorPath := demoSession(t, a, handler, "doctor")
	contents := []byte("private-file-payload-should-never-appear-in-the-page")
	r := uploadRequest(t, url.Values{"csrf": {patient.CSRF}, "patient_id": {patient.ProfileID}}, `C:\fakepath\recovery-report.pdf`, contents)
	w := serveRequest(handler, r, patientCookies)
	assertSSE(t, w, "#uploads", "prepend")
	uploads, err := a.store.Uploads(context.Background(), demoPatientID)
	if err != nil || len(uploads) == 0 || uploads[0].FileName != "recovery-report.pdf" {
		t.Fatalf("filename was not sanitized and stored: %+v, %v", uploads, err)
	}
	page := serveRequest(handler, httptest.NewRequest(http.MethodGet, doctorPath, nil), doctorCookies)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "recovery-report.pdf") {
		t.Fatal("linked doctor cannot see the uploaded filename")
	}
	if strings.Contains(w.Body.String(), string(contents)) || strings.Contains(page.Body.String(), string(contents)) {
		t.Fatal("file bytes leaked into an HTML response")
	}
	if strings.Contains(page.Body.String(), "ankle-recovery-notes.pdf") {
		t.Fatal("reports center leaked an unlinked patient's filename")
	}
}

func TestRejectedUploadsDoNotWrite(t *testing.T) {
	for _, test := range []struct {
		name, patientID, filename string
		size                      int
		badCSRF                   bool
	}{
		{name: "foreign profile", patientID: "00000000-0000-4000-8000-000000000102", filename: "report.pdf", size: 10},
		{name: "invalid CSRF", filename: "report.pdf", size: 10, badCSRF: true},
		{name: "oversize", filename: "report.pdf", size: (5 << 20) + 1},
		{name: "unsupported extension", filename: "report.exe", size: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, handler := testApp(t)
			cookies, s, _ := demoSession(t, a, handler, "patient")
			patientID := test.patientID
			if patientID == "" {
				patientID = s.ProfileID
			}
			csrf := s.CSRF
			if test.badCSRF {
				csrf = "wrong"
			}
			before, _ := a.store.Uploads(context.Background(), patientID)
			r := uploadRequest(t, url.Values{"csrf": {csrf}, "patient_id": {patientID}}, test.filename, bytes.Repeat([]byte("x"), test.size))
			w := serveRequest(handler, r, cookies)
			assertSSE(t, w, "#form-feedback", "outer")
			if !strings.Contains(w.Body.String(), "feedback error") {
				t.Fatal("upload was not rejected visibly")
			}
			after, _ := a.store.Uploads(context.Background(), patientID)
			if len(after) != len(before) {
				t.Fatal("rejected request changed the uploads")
			}
		})
	}
}

func TestOnboardingCreatesRoleSpecificProfiles(t *testing.T) {
	for _, role := range []string{"patient", "doctor"} {
		t.Run(role, func(t *testing.T) {
			a, handler := testApp(t)
			pending := httptest.NewRecorder()
			identity := newID()
			loggedIn := a.issueSession(pending, httptest.NewRequest(http.MethodGet, "/", nil), session{AuthUserID: identity, Email: role + ".test@example.com", Expires: time.Now().Add(time.Hour)})
			cookies := pending.Result().Cookies()
			get := serveRequest(handler, httptest.NewRequest(http.MethodGet, "/onboard/"+role, nil), cookies)
			if get.Code != http.StatusOK {
				t.Fatalf("onboard form: %d: %s", get.Code, get.Body.String())
			}
			csrf := loggedIn.CSRF
			form := url.Values{
				"csrf": {csrf}, "name": {"Demo <Tester>"}, "email": {"forged@example.com"}, "auth_user_id": {"forged"},
				"dob": {"1990-01-02"}, "blood_type": {"O+"}, "license_num": {"DEMO-123"}, "specialization": {"Primary care"},
			}
			w := postForm(handler, "/onboard/"+role, form, cookies)
			assertSSE(t, w, "#onboarding-result", "outer")
			if strings.Contains(w.Body.String(), "<Tester>") {
				t.Fatal("profile name was not escaped")
			}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, c := range w.Result().Cookies() {
				r.AddCookie(c)
			}
			s, ok := a.sessionFor(r, role)
			if !ok {
				t.Fatal("onboarding did not sign into the new role")
			}
			profile, err := a.store.Profile(context.Background(), s.ProfileID)
			if err != nil || profile.Role != role || profile.Email != role+".test@example.com" || profile.AuthUserID != identity {
				t.Fatalf("wrong new profile: %+v, %v", profile, err)
			}
			if role == "doctor" {
				if profile.LicenseNum != "DEMO-123" || profile.Specialization != "Primary care" || profile.DOB != "" || profile.BloodType != "" {
					t.Fatal("doctor profile fields were not scoped to the role")
				}
				linked, err := a.store.IsCareTeam(context.Background(), demoPatientID, profile.ID)
				if err != nil || linked {
					t.Fatal("new doctor must not gain access to a seeded patient automatically")
				}
			} else {
				if profile.DOB != "1990-01-02" || profile.BloodType != "O+" || profile.LicenseNum != "" || profile.Specialization != "" {
					t.Fatal("patient profile fields were not scoped to the role")
				}
				linked, err := a.store.IsCareTeam(context.Background(), profile.ID, demoDoctorID)
				if err != nil || linked {
					t.Fatal("new patient must not be shared with a seeded doctor automatically")
				}
			}
			page := serveRequest(handler, httptest.NewRequest(http.MethodGet, "/dashboard/"+role+"/"+profile.ID, nil), w.Result().Cookies())
			if page.Code != http.StatusOK {
				t.Fatalf("new account dashboard: %d: %s", page.Code, page.Body.String())
			}
		})
	}
}

func TestLivePatientStreamReceivesDoctorRecord(t *testing.T) {
	a, handler := testApp(t)
	patientCookies, patient, _ := demoSession(t, a, handler, "patient")
	doctorCookies, doctor, _ := demoSession(t, a, handler, "doctor")
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events/patient/"+patient.ProfileID+"?view=patient-tab", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range patientCookies {
		r.AddCookie(c)
	}
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("live stream: %d: %s", response.StatusCode, body)
	}
	marker := "Cross-tab integration check 98124"
	w := postForm(handler, "/add-record", url.Values{"csrf": {doctor.CSRF}, "doctor_id": {doctor.ProfileID}, "patient_id": {patient.ProfileID}, "type": {"note"}, "content": {marker}, "view_id": {"doctor-tab"}}, doctorCookies)
	assertSSE(t, w, "#timeline", "prepend")
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), marker) {
			return
		}
	}
	t.Fatalf("patient event stream did not receive the doctor's new record: %v", scanner.Err())
}
