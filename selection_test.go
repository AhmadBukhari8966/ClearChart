package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDoctorSignalSelectionPatchesOnlyTheSelectedChart(t *testing.T) {
	a, handler := testApp(t)
	cookies, doctor, _ := demoSession(t, a, handler, "doctor")
	patientID := "40000000-0000-4000-8000-000000000300"
	path := "/events/doctor/" + doctor.ProfileID + "?view=stable-doctor-tab&datastar=" + url.QueryEscape(`{"selectedpatient":"`+patientID+`"}`)
	stream := openTestStream(t, handler, cookies, path)
	snapshot := readTestSnapshot(t, stream)
	for _, expected := range []string{
		`data: selector #selected-patient-summary`,
		`data: selector #new-record`,
		`data: selector #health-timeline`,
		`A care update for Zoe Thompson`,
		`id="record-form-` + patientID + `"`,
		`id="record-content-` + patientID + `"`,
		`name="patient_id" value="` + patientID + `"`,
		`name="view_id" value="stable-doctor-tab"`,
	} {
		if !strings.Contains(snapshot, expected) {
			t.Errorf("selected chart missing %q", expected)
		}
	}
	for _, forbidden := range []string{`data: selector #patient-directory`, `data: selector #my-patients`, `class="app-shell"`, `id="patient-directory"`} {
		if strings.Contains(snapshot, forbidden) {
			t.Errorf("selection must preserve the directory: %q", forbidden)
		}
	}
	timeline := timelineEvent(snapshot)
	if !strings.Contains(timeline, patientID+"/1.svg") || strings.Contains(timeline, "Two-week knee recovery review") {
		t.Fatal("selected chart has another patient's timeline or missing scans")
	}

	marker := "Updated selected chart from another doctor tab"
	w := postForm(handler, "/add-record", url.Values{"csrf": {doctor.CSRF}, "doctor_id": {doctor.ProfileID}, "patient_id": {patientID}, "type": {"note"}, "content": {marker}, "view_id": {"other-doctor-tab"}}, cookies)
	assertSSE(t, w, "#timeline", "prepend")
	liveUpdate := readTestSnapshot(t, stream)
	if !strings.Contains(timelineEvent(liveUpdate), marker) {
		t.Fatal("selected patient's stream did not receive a new note")
	}
	if strings.Contains(liveUpdate, "data: selector #new-record") {
		t.Fatal("live refresh should preserve the current note draft")
	}
}

func TestDoctorSignalSelectionValidatesCareTeamAndJSON(t *testing.T) {
	a, handler := testApp(t)
	cookies, doctor, _ := demoSession(t, a, handler, "doctor")
	for _, tc := range []struct {
		signals string
		status  int
	}{
		{`{"selectedpatient":"00000000-0000-4000-8000-000000000103"}`, http.StatusForbidden},
		{`{"selectedpatient":42}`, http.StatusBadRequest},
		{`{broken`, http.StatusBadRequest},
		{strings.Repeat("x", 4097), http.StatusBadRequest},
	} {
		// A valid legacy query parameter must not override the actual selection.
		path := "/events/doctor/" + doctor.ProfileID + "?patient=" + demoPatientID + "&datastar=" + url.QueryEscape(tc.signals)
		w := serveRequest(handler, httptest.NewRequest(http.MethodGet, path, nil), cookies)
		if w.Code != tc.status {
			t.Errorf("selection %q: status=%d, want %d", tc.signals[:min(len(tc.signals), 90)], w.Code, tc.status)
		}
	}
}
