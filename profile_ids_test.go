package main

import (
	"context"
	"errors"
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

func TestAuthIdentityIsIndependentOfProfileID(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	original, err := s.Profile(ctx, demoPatientID)
	if err != nil {
		t.Fatal(err)
	}
	storedID := newID()
	remapMemoryProfile(s, original.ID, storedID)
	profile, err := s.ProfileByAuthUserID(ctx, original.AuthUserID)
	if err != nil || profile.ID != storedID || profile.AuthUserID != original.AuthUserID {
		t.Fatalf("auth lookup did not return the actual stored profile ID: %#v, %v", profile, err)
	}
	for _, absent := range []string{"", newID()} {
		if _, err = s.ProfileByAuthUserID(ctx, absent); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown auth identity %q: %v", absent, err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.ProfileByAuthUserID(canceled, original.AuthUserID); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup: %v", err)
	}
}

func TestProfileCreationRequiresAuthenticatedIdentity(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	profile := Profile{ID: "caller-must-not-pick-an-id", Role: "patient", Name: "New Patient", Email: "new.patient@example.com", DOB: "1990-01-02", BloodType: "O+"}
	if _, err := s.CreateProfile(ctx, profile); err == nil {
		t.Fatal("profile creation accepted an unauthenticated identity")
	}
	profile.AuthUserID = newID()
	created, err := s.CreateProfile(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == profile.ID || created.ID == created.AuthUserID || created.ID == "" {
		t.Fatal("profile ID must be independently assigned by storage")
	}
	team, err := s.CareTeam(ctx, created.ID)
	if err != nil || len(team) != 0 {
		t.Fatalf("new account unexpectedly received seeded care-team access: %v, %v", team, err)
	}
	byAuth, err := s.ProfileByAuthUserID(ctx, profile.AuthUserID)
	if err != nil || byAuth.ID != created.ID {
		t.Fatalf("created account lookup: %#v, %v", byAuth, err)
	}
	duplicate := profile
	duplicate.Email = "different.email@example.com"
	if _, err := s.CreateProfile(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("same auth identity created two profiles: %v", err)
	}
	duplicate = profile
	duplicate.AuthUserID = newID()
	duplicate.Email = "NEW.PATIENT@EXAMPLE.COM"
	if _, err := s.CreateProfile(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("case-insensitive duplicate email: %v", err)
	}
}
