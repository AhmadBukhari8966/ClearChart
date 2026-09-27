package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"
)

type Profile struct {
	ID, AuthUserID, Role, Name, Email, LicenseNum, Specialization, DOB, BloodType string
}

type Record struct {
	ID, PatientID, DoctorID, Type, Content, ImageURL, DoctorName, PatientName string
	Timestamp                                                                 time.Time
	Categories                                                                []string // explicit body-area tags; empty when uncategorized
}

type Upload struct {
	ID, PatientID, FileName, PatientName string
	Timestamp                            time.Time
}

type Healing struct {
	ID, PatientID, StatusType string
	Value                     int
	Timestamp                 time.Time
}

type Biometric struct {
	DoctorID   string
	HeartRate  int
	SleepHours float64
	Timestamp  time.Time
}

type Dashboard struct {
	Profile, Patient                            Profile
	Patients, Doctors                           []Profile
	Records                                     []Record
	Uploads, Reports                            []Upload
	Healing                                     []Healing
	Biometric                                   Biometric
	CSRF, Mode, Summary, Role, ViewID           string
	Filter                                      timelineFilter
	RecordCount, UploadCount, NotificationCount int
	Today                                       time.Time
}

type Store interface {
	Profile(context.Context, string) (Profile, error)
	ProfileByAuthUserID(context.Context, string) (Profile, error)
	ProfileByEmail(context.Context, string) (Profile, error)
	Dashboard(ctx context.Context, viewer, role, email, patient string) (DashboardBundle, error)
	Counts(context.Context, string) (int, int, error)
	Records(context.Context, string) ([]Record, error)
	Healing(context.Context, string) ([]Healing, error)
	CreateProfile(context.Context, Profile) (Profile, error)
	AddRecord(context.Context, Record) (Record, error)
	AddUpload(context.Context, Upload) (Upload, error)
	AddHealing(context.Context, Healing) (Healing, error)
	IsCareTeam(context.Context, string, string) (bool, error)
	CreateInvitation(context.Context, string, string, string) (Invitation, error)
	Invitations(context.Context, string) ([]Invitation, error)
	Invitation(context.Context, string) (Invitation, error)
	RespondInvitation(context.Context, string, string, string, bool) (string, error)
	RevokeInvitation(context.Context, string, string) error
	PendingInvitationsForEmail(context.Context, string) ([]Invitation, error)
	RecentInvitationActivity(context.Context, string) ([]Invitation, error)
	Close() error
}

// validUUID reports whether s is a canonical textual UUID, so malformed IDs
// are rejected before they reach PostgreSQL casts.
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure randomness unavailable")
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
