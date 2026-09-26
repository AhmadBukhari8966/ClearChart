package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Change fixture IDs without changing their identity or relationships. This
// simulates the arbitrary IDs returned by PostgreSQL on a fresh installation.
func remapMemoryProfile(s *memoryStore, oldID, newID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.profiles[oldID]
	delete(s.profiles, oldID)
	p.ID = newID
	s.profiles[newID] = p
	if doctors, ok := s.care[oldID]; ok {
		delete(s.care, oldID)
		s.care[newID] = doctors
	}
	for _, doctors := range s.care {
		if doctors[oldID] {
			delete(doctors, oldID)
			doctors[newID] = true
		}
	}
	for i := range s.records {
		if s.records[i].PatientID == oldID {
			s.records[i].PatientID = newID
		}
		if s.records[i].DoctorID == oldID {
			s.records[i].DoctorID = newID
		}
	}
	for i := range s.uploads {
		if s.uploads[i].PatientID == oldID {
			s.uploads[i].PatientID = newID
		}
	}
	for i := range s.healing {
		if s.healing[i].PatientID == oldID {
			s.healing[i].PatientID = newID
		}
	}
	if b, ok := s.biometrics[oldID]; ok {
		delete(s.biometrics, oldID)
		b.DoctorID = newID
		s.biometrics[newID] = b
	}
}

func TestDemoAndOnboardingUseStoredProfileIDs(t *testing.T) {
	a, handler := testApp(t)
	doctorID, patientID := newID(), newID()
	s := a.store.(*memoryStore)
	remapMemoryProfile(s, demoDoctorID, doctorID)
	remapMemoryProfile(s, demoPatientID, patientID)

	for _, role := range []string{"doctor", "patient"} {
		cookies, current, location := demoSession(t, a, handler, role)
		want := patientID
		if role == "doctor" {
			want = doctorID
		}
		if current.ProfileID != want || !strings.HasSuffix(location, want) {
			t.Fatalf("%s selected old fixture ID: %s, %s", role, current.ProfileID, location)
		}
		page := serveRequest(handler, httptest.NewRequest(http.MethodGet, location, nil), cookies)
		if page.Code != http.StatusOK {
			t.Fatalf("%s dashboard: %d", role, page.Code)
		}

		get := serveRequest(handler, httptest.NewRequest(http.MethodGet, "/onboard/"+role, nil), nil)
		var csrf string
		for _, c := range get.Result().Cookies() {
			if c.Name == "clearchart_onboard" {
				csrf = c.Value
			}
		}
		result := postForm(handler, "/onboard/"+role, url.Values{
			"csrf": {csrf}, "name": {"Database ID Tester"}, "email": {role + ".generated-id@example.com"},
			"license_num": {"TEST-123"}, "specialization": {"Primary care"},
			"dob": {"1990-01-02"}, "blood_type": {"O+"},
		}, get.Result().Cookies())
		assertSSE(t, result, "#onboarding-result", "outer")
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, c := range result.Result().Cookies() {
			request.AddCookie(c)
		}
		created, ok := a.sessionFor(request, role)
		if !ok {
			t.Fatal("onboarding did not create a session")
		}
		var linked bool
		var err error
		if role == "doctor" {
			linked, err = s.IsCareTeam(context.Background(), patientID, created.ProfileID)
		} else {
			linked, err = s.IsCareTeam(context.Background(), created.ProfileID, doctorID)
		}
		if err != nil || !linked {
			t.Fatalf("%s onboarding did not link to the stored counterpart ID: %v", role, err)
		}
	}

	p, err := s.ProfileByEmail(context.Background(), strings.ToUpper(demoPatientEmail))
	if err != nil || p.ID != patientID {
		t.Fatal("email lookup must be case-insensitive and return the stored ID")
	}
	if _, err = s.ProfileByEmail(context.Background(), "absent@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown email: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.ProfileByEmail(ctx, demoPatientEmail); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup: %v", err)
	}
}
