package main

import (
	"fmt"
	"strings"
	"time"
)

const bulkPatientCount = 300

// Keep this fixture aligned with seed_large.sql, which expands the same data in
// PostgreSQL. IDs are deterministic so saved patient links survive demo restarts.
func addBulkPatients(s *memoryStore, now time.Time) {
	firstNames := []string{"Avery", "Bailey", "Cameron", "Casey", "Dakota", "Drew", "Elliot", "Emery", "Finley", "Harper", "Jamie", "Jesse", "Kai", "Logan", "Morgan", "Parker", "Quinn", "Riley", "Rowan", "Zoe"}
	lastNames := []string{"Bennett", "Carter", "Diaz", "Ellis", "Foster", "Garcia", "Hayes", "Ibrahim", "Jensen", "Kim", "Lopez", "Nguyen", "Patel", "Rivera", "Thompson"}
	bloodTypes := []string{"O+", "A+", "B+", "AB+", "O-", "A-", "B-", "AB-"}
	type recoveryScenario struct{ area, review, baseline, medication string }
	scenarios := []recoveryScenario{
		{"knee", "Walking confidence is improving and reported swelling has eased. Physiotherapy progress was reviewed.", "Initial knee recovery discussion documented stiffness and a need for support on longer walks.", "Fictional acetaminophen review recorded; the existing discharge plan is unchanged."},
		{"shoulder", "Comfort during daily activities has improved. Range-of-motion progress was reviewed with the therapy team.", "Initial shoulder review documented difficulty reaching overhead and interrupted sleep.", "Fictional anti-inflammatory medication review recorded; questions are reserved for the care team."},
		{"ankle", "The patient reports less swelling and more confidence moving around the home. Rehabilitation milestones were discussed.", "Initial ankle recovery discussion documented swelling after activity and limited walking tolerance.", "Fictional pain-relief prescription review recorded; no new medication instructions were added."},
		{"wrist", "Hand comfort and light daily tasks are improving. The therapist's progress update was reviewed.", "Initial wrist review documented stiffness and difficulty with grip-dependent activities.", "Fictional discharge medication reconciliation completed; the recorded plan remains unchanged."},
		{"spine", "Sitting tolerance and sleep have improved. Progress against the agreed rehabilitation goals was reviewed.", "Initial spine recovery discussion documented reduced sitting tolerance and stiffness during daily activities.", "Fictional medication reconciliation completed; a review with the care team is documented."},
		{"hip", "The patient reports steadier movement and improved confidence with everyday activities. Therapy milestones were reviewed.", "Initial hip review documented reduced mobility and a need for support with longer walks.", "Fictional postoperative prescription review recorded; the existing clinician-approved plan is unchanged."},
	}
	for i := 1; i <= bulkPatientCount; i++ {
		first, last := firstNames[(i-1)/len(lastNames)], lastNames[(i-1)%len(lastNames)]
		patientID := fmt.Sprintf("40000000-0000-4000-8000-%012d", i)
		name := first + " " + last
		dob := time.Date(2006-(i-1)%61, time.Month(1+(i-1)%8), 1+(i-1)%28, 0, 0, 0, 0, time.UTC)
		s.profiles[patientID] = Profile{ID: patientID, Role: "patient", Name: name, Email: fmt.Sprintf("%s.%s.%03d@example.com", strings.ToLower(first), strings.ToLower(last), i), DOB: dob.Format("2006-01-02"), BloodType: bloodTypes[(i-1)%len(bloodTypes)]}
		s.care[patientID] = map[string]bool{demoDoctorID: true}
		if i%3 == 0 {
			s.care[patientID]["00000000-0000-4000-8000-000000000002"] = true
		}
		scenario := scenarios[(i-1)%len(scenarios)]
		recentHours := 12 + (i*7)%180
		visit := fmt.Sprintf("DEMO-%04d", i)
		prefix := fmt.Sprintf("%s | %s | %s recovery. ", name, visit, scenario.area)
		for j, r := range []struct {
			kind, content, image string
			hours                int
		}{
			{"note", prefix + "Follow-up: " + scenario.review + " This is a fictional training record.", "", recentHours},
			{"imaging", prefix + "Follow-up illustrative synthetic scan. Comparison image for this mock visit; no diagnostic interpretation.", "/mock-scans/" + patientID + "/2.svg", recentHours + 24},
			{"prescription", prefix + scenario.medication + " Demo documentation only; no dosage or treatment instructions.", "", recentHours + 48},
			{"note", prefix + "Baseline: " + scenario.baseline + " Goals were recorded for the next fictional review.", "", recentHours + 168},
			{"imaging", prefix + "Baseline illustrative synthetic scan. Patient-specific demo reference image; no diagnostic interpretation.", "/mock-scans/" + patientID + "/1.svg", recentHours + 192},
		} {
			s.records = append(s.records, Record{ID: fmt.Sprintf("50000000-0000-4000-8000-%012d", (i-1)*5+j+1), PatientID: patientID, DoctorID: demoDoctorID, Type: r.kind, Content: r.content, ImageURL: r.image, DoctorName: s.profiles[demoDoctorID].Name, PatientName: name, Timestamp: now.Add(-time.Duration(r.hours) * time.Hour)})
		}
		s.uploads = append(s.uploads, Upload{ID: fmt.Sprintf("60000000-0000-4000-8000-%012d", i), PatientID: patientID, FileName: fmt.Sprintf("%s-%s-%03d-%s-progress.pdf", strings.ToLower(first), strings.ToLower(last), i, scenario.area), PatientName: name, Timestamp: now.Add(-time.Duration(recentHours+6) * time.Hour)})
		for j, h := range []struct {
			kind  string
			value int
		}{{"pain", 1 + i%6}, {"mobility", 4 + i%7}, {"energy", 5 + i%6}} {
			s.healing = append(s.healing, Healing{ID: fmt.Sprintf("70000000-0000-4000-8000-%012d", (i-1)*3+j+1), PatientID: patientID, StatusType: h.kind, Value: h.value, Timestamp: now.Add(-time.Duration(recentHours+1) * time.Hour)})
		}
	}
}
