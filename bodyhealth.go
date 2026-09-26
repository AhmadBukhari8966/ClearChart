package main

import (
	"strings"
	"time"
)

// BodyPartHealth is a conservative, record-backed status for one anatomical area.
// It is intentionally not a diagnosis. No-data areas stay explicitly marked N/A.
type BodyPartHealth struct {
	ID, Name, System, Status, Summary, PossibleIssue, FutureRisk, Guidance, Evidence string
	Level                                                                            int
	HasData, Critical                                                                bool
	LastUpdated                                                                      time.Time
}

type AnatomyHealth struct {
	Brain, Heart, Lungs, Liver, Stomach, Kidneys, Spine  BodyPartHealth
	Shoulders, Knees, Ankles, Hips, Blood, NervousSystem BodyPartHealth
	Muscles                                              BodyPartHealth
	Parts                                                []BodyPartHealth
}

func unavailablePart(id, name, system string) BodyPartHealth {
	return BodyPartHealth{
		ID: id, Name: name, System: system, Status: "N/A",
		Summary:       "No information available for this area in the current ClearChart record.",
		PossibleIssue: "N/A", FutureRisk: "N/A", Guidance: "No guidance available from current records.",
	}
}

func newAnatomyHealth() AnatomyHealth {
	return AnatomyHealth{
		Brain:         unavailablePart("brain", "Brain", "Nervous system"),
		Heart:         unavailablePart("heart", "Heart", "Cardiovascular"),
		Lungs:         unavailablePart("lungs", "Lungs", "Respiratory"),
		Liver:         unavailablePart("liver", "Liver", "Digestive"),
		Stomach:       unavailablePart("stomach", "Stomach", "Digestive"),
		Kidneys:       unavailablePart("kidneys", "Kidneys", "Urinary"),
		Spine:         unavailablePart("spine", "Spine", "Skeletal / nervous"),
		Shoulders:     unavailablePart("shoulders", "Shoulders", "Musculoskeletal"),
		Knees:         unavailablePart("knees", "Knees", "Musculoskeletal"),
		Ankles:        unavailablePart("ankles", "Ankles", "Musculoskeletal"),
		Hips:          unavailablePart("hips", "Hips", "Musculoskeletal"),
		Blood:         unavailablePart("blood", "Blood / circulation", "Cardiovascular"),
		NervousSystem: unavailablePart("nervous-system", "Nervous system", "Nervous system"),
		Muscles:       unavailablePart("muscles", "Muscles", "Musculoskeletal"),
	}
}

func recordBackedPart(base BodyPartHealth, matches []Record) BodyPartHealth {
	if len(matches) == 0 {
		return base
	}
	latest := matches[0]
	level := 3
	status := "Monitor"
	lower := strings.ToLower(latest.Content)
	if strings.Contains(lower, "improving") || strings.Contains(lower, "expected course") || strings.Contains(lower, "no new concerning") || strings.Contains(lower, "healing well") {
		level = 2
		status = "Watch"
	}
	critical := strings.Contains(lower, "urgent") || strings.Contains(lower, "emergency") || strings.Contains(lower, "critical")
	if critical {
		level = 5
		status = "Urgent flag"
	}
	base.HasData = true
	base.Level = level
	base.Status = status
	base.Summary = latest.Content
	base.PossibleIssue = "A care-team record contains information about this area. Review the source record for the clinician's wording."
	base.FutureRisk = "ClearChart does not predict future disease from this record. Ongoing or worsening findings should be reviewed by the care team."
	base.Guidance = "Continue the documented care plan and contact the care team if symptoms change or worsen."
	if critical {
		base.Guidance = "This demo record contains urgent language. Seek prompt clinical review; severe or rapidly worsening symptoms may require emergency care."
	}
	base.Critical = critical
	base.Evidence = evidenceLabel(len(matches), latest.Timestamp)
	base.LastUpdated = latest.Timestamp
	return base
}

func evidenceLabel(count int, latest time.Time) string {
	word := "records"
	if count == 1 {
		word = "record"
	}
	return strings.Join([]string{itoa(count), word, "· latest", latest.Format("Jan 2, 2006")}, " ")
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func matchesRecords(records []Record, keywords ...string) []Record {
	var out []Record
	for _, r := range records {
		text := strings.ToLower(r.Content)
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func buildAnatomyHealth(records []Record) AnatomyHealth {
	h := newAnatomyHealth()
	h.Brain = recordBackedPart(h.Brain, matchesRecords(records, "brain", "neurolog", "head", "cognitive"))
	h.Heart = recordBackedPart(h.Heart, matchesRecords(records, "heart", "cardiac", "cardiovascular"))
	h.Lungs = recordBackedPart(h.Lungs, matchesRecords(records, "lung", "pulmonary", "respiratory", "breathing"))
	h.Liver = recordBackedPart(h.Liver, matchesRecords(records, "liver", "hepatic"))
	h.Stomach = recordBackedPart(h.Stomach, matchesRecords(records, "stomach", "gastric", "abdomen", "abdominal"))
	h.Kidneys = recordBackedPart(h.Kidneys, matchesRecords(records, "kidney", "renal"))
	h.Spine = recordBackedPart(h.Spine, matchesRecords(records, "spine", "spinal", "back"))
	h.Shoulders = recordBackedPart(h.Shoulders, matchesRecords(records, "shoulder"))
	h.Knees = recordBackedPart(h.Knees, matchesRecords(records, "knee"))
	h.Ankles = recordBackedPart(h.Ankles, matchesRecords(records, "ankle"))
	h.Hips = recordBackedPart(h.Hips, matchesRecords(records, "hip", "pelvis", "pelvic"))
	h.Blood = recordBackedPart(h.Blood, matchesRecords(records, "blood", "circulation", "vascular", "hemoglobin"))
	h.NervousSystem = recordBackedPart(h.NervousSystem, matchesRecords(records, "nerve", "neurolog", "nervous"))
	h.Muscles = recordBackedPart(h.Muscles, matchesRecords(records, "muscle", "mobility", "physiotherapy", "physical therapy"))
	h.Parts = []BodyPartHealth{h.Brain, h.Heart, h.Lungs, h.Liver, h.Stomach, h.Kidneys, h.Spine, h.Shoulders, h.Knees, h.Ankles, h.Hips, h.Blood, h.NervousSystem, h.Muscles}
	return h
}
