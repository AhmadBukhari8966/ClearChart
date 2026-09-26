package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Only the read operations used by dashboards are implemented. Unexpected
// writes or unrelated calls fail instead of touching a real database.
type bodyMapStore struct {
	Store
	linked        bool
	recordPatient string
}

func (s *bodyMapStore) Profile(_ context.Context, id string) (Profile, error) {
	if id == "doctor" {
		return Profile{ID: id, Role: "doctor", Name: "Dr. Test"}, nil
	}
	return Profile{ID: id, Role: "patient", Name: "Patient " + id}, nil
}
func (s *bodyMapStore) Patients(context.Context, string) ([]Profile, error) {
	if !s.linked {
		return nil, nil
	}
	return []Profile{{ID: "patient", Role: "patient", Name: "Patient patient"}, {ID: "second", Role: "patient", Name: "Patient second"}}, nil
}
func (s *bodyMapStore) IsCareTeam(_ context.Context, patient, doctor string) (bool, error) {
	return s.linked && doctor == "doctor" && (patient == "patient" || patient == "second"), nil
}
func (s *bodyMapStore) Records(_ context.Context, id string) ([]Record, error) {
	s.recordPatient = id
	return []Record{{ID: "record", PatientID: id, Type: "note", Content: "Knee healing well <script>alert(1)</script>", Timestamp: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}}, nil
}
func (*bodyMapStore) CareTeam(context.Context, string) ([]Profile, error)   { return nil, nil }
func (*bodyMapStore) Uploads(context.Context, string) ([]Upload, error)     { return nil, nil }
func (*bodyMapStore) Reports(context.Context, string) ([]Upload, error)     { return nil, nil }
func (*bodyMapStore) Healing(context.Context, string) ([]Healing, error)    { return nil, nil }
func (*bodyMapStore) Biometrics(context.Context, string) (Biometric, error) { return Biometric{}, nil }
func (*bodyMapStore) PendingInvitationsForEmail(context.Context, string) ([]Invitation, error) {
	return []Invitation{{ID: "invite"}}, nil
}
func (*bodyMapStore) RecentInvitationActivity(context.Context, string) ([]Invitation, error) {
	return []Invitation{{ID: "activity"}}, nil
}

func TestBodyMapIntegration(t *testing.T) {
	tests := []struct {
		name, role, path, patient string
		linked                    bool
		status                    int
		contains                  []string
	}{
		{name: "patient", role: "patient", path: "/dashboard/patient/patient/body-map", patient: "patient", status: 200,
			contains: []string{`id="view-2d" checked`, `id="view-3d"`, `id="layer-skeleton"`, `id="layer-circulation"`, `id="layer-nervous"`, `id="part-knees"`, `Watch`, `N/A`, `width: 40%`, `&lt;script&gt;`, `/static/body-map.css`, `data-signals="{_notifOpen: false}"`, `Notifications (1 new)`}},
		{name: "patient cannot select another chart", role: "patient", path: "/dashboard/patient/patient/body-map?patient=second", patient: "patient", status: 200},
		{name: "doctor selected patient", role: "doctor", path: "/dashboard/doctor/doctor/body-map?patient=second", patient: "second", linked: true, status: 200,
			contains: []string{`Viewing Patient second`, `Invite patients`, `Notifications (1 new)`}},
		{name: "doctor default patient", role: "doctor", path: "/dashboard/doctor/doctor/body-map", patient: "patient", linked: true, status: 200},
		{name: "doctor empty state", role: "doctor", path: "/dashboard/doctor/doctor/body-map", status: 200, contains: []string{`No patient selected`}},
		{name: "unlinked patient denied", role: "doctor", path: "/dashboard/doctor/doctor/body-map?patient=stranger", linked: true, status: 403},
		{name: "foreign profile denied", role: "patient", path: "/dashboard/patient/second/body-map", status: 403},
		{name: "anonymous", path: "/dashboard/patient/patient/body-map", status: 303},
		{name: "wrong role", role: "patient", path: "/dashboard/doctor/doctor/body-map", status: 303},
		{name: "invalid role", path: "/dashboard/admin/patient/body-map", status: 404},
		{name: "patient overview preserved", role: "patient", path: "/dashboard/patient/patient", patient: "patient", status: 200,
			contains: []string{`<strong>Overview</strong>`, `id="health-timeline"`, `Notifications (1 new)`, `/dashboard/patient/patient/body-map`}},
		{name: "doctor overview preserved", role: "doctor", path: "/dashboard/doctor/doctor?patient=second", patient: "second", linked: true, status: 200,
			contains: []string{`<strong>Overview</strong>`, `Invite patients`, `id="care-team-update"`, `Notifications (1 new)`, `/body-map?patient=second`, `data-attr:href=`, `$selectedpatient`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &bodyMapStore{linked: tc.linked}
			a, err := newApp(st, "Test")
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", tc.path, nil)
			if tc.role != "" {
				w := httptest.NewRecorder()
				a.issueSession(w, r, session{ProfileID: tc.role, Role: tc.role, Email: "test@example.org", Expires: time.Now().Add(time.Hour)})
				for _, cookie := range w.Result().Cookies() {
					r.AddCookie(cookie)
				}
			}
			w := httptest.NewRecorder()
			a.routes().ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if st.recordPatient != tc.patient {
				t.Fatalf("read chart %q, want %q", st.recordPatient, tc.patient)
			}
			body := w.Body.String()
			for _, want := range tc.contains {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q", want)
				}
			}
			for _, bad := range []string{"ZgotmplZ", "<script>alert(1)</script>"} {
				if strings.Contains(body, bad) {
					t.Errorf("unexpected unsafe output %q", bad)
				}
			}
			if !strings.Contains(tc.path, "/body-map") && strings.Contains(body, `/static/body-map.css`) {
				t.Error("body map styles leaked onto the overview")
			}
			if tc.status == http.StatusSeeOther && w.Header().Get("Location") != "/login" {
				t.Error("expected login redirect")
			}
		})
	}
}

func TestBodyMapStylesServed(t *testing.T) {
	a, err := newApp(nil, "Test")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, httptest.NewRequest("GET", "/static/body-map.css", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "rotateY(-18deg)") || !strings.Contains(w.Body.String(), "#layer-organs:checked") {
		t.Fatal("body view and anatomy layer styles were not served")
	}
}
