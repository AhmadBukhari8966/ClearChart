package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"
)

const (
	demoDoctorID  = "00000000-0000-4000-8000-000000000001"
	demoPatientID = "00000000-0000-4000-8000-000000000101"
)

type Profile struct {
	ID, Role, Name, Email, LicenseNum, Specialization, DOB, BloodType string
}

type Record struct {
	ID, PatientID, DoctorID, Type, Content, ImageURL, DoctorName, PatientName string
	Timestamp                                                                 time.Time
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
	Profile, Patient                          Profile
	Patients, Doctors                         []Profile
	Records                                   []Record
	Uploads, Reports                          []Upload
	Healing                                   []Healing
	Biometric                                 Biometric
	CSRF, Mode, Filter, Summary, Role, ViewID string
	RecordCount, UploadCount                  int
	Today                                     time.Time
}

type Store interface {
	Profile(context.Context, string) (Profile, error)
	Profiles(context.Context, string) ([]Profile, error)
	CareTeam(context.Context, string) ([]Profile, error)
	Patients(context.Context, string) ([]Profile, error)
	Records(context.Context, string) ([]Record, error)
	Uploads(context.Context, string) ([]Upload, error)
	Reports(context.Context, string) ([]Upload, error)
	Healing(context.Context, string) ([]Healing, error)
	Biometrics(context.Context, string) (Biometric, error)
	CreateProfile(context.Context, Profile) (Profile, error)
	AddRecord(context.Context, Record) (Record, error)
	AddUpload(context.Context, Upload) (Upload, error)
	AddHealing(context.Context, Healing) (Healing, error)
	IsCareTeam(context.Context, string, string) (bool, error)
	Close() error
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
