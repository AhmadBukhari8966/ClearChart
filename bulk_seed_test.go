package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBulkDemoPatientsHaveIndividualCompleteCharts(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	patients, err := s.Profiles(ctx, "patient")
	if err != nil || len(patients) != 303 {
		t.Fatalf("patient list: count=%d error=%v; want 303", len(patients), err)
	}
	sarahPatients, err := s.Patients(ctx, demoDoctorID)
	if err != nil || len(sarahPatients) != 302 {
		t.Fatalf("Sarah's list: count=%d error=%v; want 302", len(sarahPatients), err)
	}
	if sarahPatients[0].ID != demoPatientID || sarahPatients[len(sarahPatients)-1].Name != "Zoe Thompson" {
		t.Fatal("patient navigation should keep Alex first and include Zoe Thompson at the end")
	}
	if linked, err := s.IsCareTeam(ctx, "00000000-0000-4000-8000-000000000103", demoDoctorID); err != nil || linked {
		t.Fatalf("bulk fixture changed Taylor's original authorization: linked=%v err=%v", linked, err)
	}
	seenIDs, seenNames, seenEmails, seenImages := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i := 1; i <= bulkPatientCount; i++ {
		patientID := fmt.Sprintf("40000000-0000-4000-8000-%012d", i)
		p, err := s.Profile(ctx, patientID)
		if err != nil || p.Role != "patient" {
			t.Fatalf("patient %d missing: %+v %v", i, p, err)
		}
		if seenIDs[p.ID] || seenNames[p.Name] || seenEmails[p.Email] {
			t.Fatalf("patient %d shares an identifier, name, or email with another fixture", i)
		}
		seenIDs[p.ID], seenNames[p.Name], seenEmails[p.Email] = true, true, true
		if _, err := time.Parse("2006-01-02", p.DOB); err != nil || !strings.HasSuffix(p.Email, "@example.com") {
			t.Fatalf("invalid fictional profile: %+v", p)
		}
		team, err := s.CareTeam(ctx, patientID)
		wantTeamSize := 1
		if i%3 == 0 {
			wantTeamSize = 2
		}
		if err != nil || len(team) != wantTeamSize {
			t.Fatalf("%s care team: count=%d err=%v", p.Name, len(team), err)
		}
		records, err := s.Records(ctx, patientID)
		if err != nil || len(records) != 5 {
			t.Fatalf("%s chart: count=%d err=%v", p.Name, len(records), err)
		}
		kinds := map[string]int{}
		for j, r := range records {
			if seenIDs[r.ID] {
				t.Fatalf("duplicate record ID %s", r.ID)
			}
			seenIDs[r.ID] = true
			kinds[r.Type]++
			if r.PatientID != p.ID || r.PatientName != p.Name || !strings.Contains(r.Content, p.Name) || !strings.Contains(r.Content, fmt.Sprintf("DEMO-%04d", i)) {
				t.Fatalf("%s record is not patient-specific: %+v", p.Name, r)
			}
			if linked, err := s.IsCareTeam(ctx, p.ID, r.DoctorID); err != nil || !linked {
				t.Fatalf("record author is outside %s's care team", p.Name)
			}
			if j > 0 && r.Timestamp.After(records[j-1].Timestamp) {
				t.Fatalf("%s timeline is out of order", p.Name)
			}
			if r.Type == "imaging" {
				if seenImages[r.ImageURL] || (r.ImageURL != "/mock-scans/"+p.ID+"/1.svg" && r.ImageURL != "/mock-scans/"+p.ID+"/2.svg") {
					t.Fatalf("%s scan is missing or shared with another patient: %s", p.Name, r.ImageURL)
				}
				seenImages[r.ImageURL] = true
			} else if r.ImageURL != "" {
				t.Fatalf("non-imaging record has an image URL: %+v", r)
			}
		}
		if kinds["note"] != 2 || kinds["prescription"] != 1 || kinds["imaging"] != 2 {
			t.Fatalf("%s record types: %v", p.Name, kinds)
		}
		uploads, err := s.Uploads(ctx, p.ID)
		if err != nil || len(uploads) != 1 || uploads[0].PatientID != p.ID || uploads[0].PatientName != p.Name {
			t.Fatalf("%s report metadata: %+v err=%v", p.Name, uploads, err)
		}
		healing, err := s.Healing(ctx, p.ID)
		if err != nil || len(healing) != 3 {
			t.Fatalf("%s recovery scores: %+v err=%v", p.Name, healing, err)
		}
		for _, h := range healing {
			if h.PatientID != p.ID || h.Value < 1 || h.Value > 10 {
				t.Fatalf("invalid recovery score: %+v", h)
			}
		}
	}
	if len(seenImages) != 600 {
		t.Fatalf("want 600 distinct scan URLs, got %d", len(seenImages))
	}
}
