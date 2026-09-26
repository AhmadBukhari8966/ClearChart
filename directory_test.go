package main

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoctorCanSelectFirstMiddleAndLastPatient(t *testing.T) {
	a, handler := testApp(t)
	cookies, doctor, dashboardURL := demoSession(t, a, handler, "doctor")
	patients, err := a.store.Patients(context.Background(), doctor.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if len(patients) < 300 {
		t.Fatalf("need hundreds of selectable patients, got %d", len(patients))
	}
	for _, index := range []int{0, len(patients) / 2, len(patients) - 1} {
		patient := patients[index]
		page := serveRequest(handler, httptest.NewRequest(http.MethodGet, dashboardURL+"?patient="+patient.ID, nil), cookies)
		if page.Code != http.StatusOK {
			t.Fatalf("select patient %d: %d", index, page.Code)
		}
		body := page.Body.String()
		if !strings.Contains(body, `id="patient-directory"`) || !strings.Contains(body, "A care update for "+patient.Name) || !strings.Contains(body, `name="patient_id" value="`+patient.ID+`"`) {
			t.Fatalf("patient %d did not open the matching chart and record form", index)
		}
		records, err := a.store.Records(context.Background(), patient.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range records {
			if !strings.Contains(body, `id="record-`+record.ID+`"`) {
				t.Fatalf("selected chart missing record %s", record.ID)
			}
		}
	}
}

func TestMockScanIsPatientSpecificAndCareTeamScoped(t *testing.T) {
	a, handler := testApp(t)
	doctorCookies, _, _ := demoSession(t, a, handler, "doctor")
	patientCookies, _, _ := demoSession(t, a, handler, "patient")
	patientID := "40000000-0000-4000-8000-000000000300"
	patient, err := a.store.Profile(context.Background(), patientID)
	if err != nil {
		t.Fatal(err)
	}
	var previous string
	for _, number := range []string{"1", "2"} {
		url := "/mock-scans/" + patientID + "/" + number + ".svg"
		page := serveRequest(handler, httptest.NewRequest(http.MethodGet, url, nil), doctorCookies)
		if page.Code != http.StatusOK || !strings.HasPrefix(page.Header().Get("Content-Type"), "image/svg+xml") {
			t.Fatalf("scan response: %d %s", page.Code, page.Body.String())
		}
		body := page.Body.String()
		if !strings.Contains(body, patient.Name) || !strings.Contains(body, "NOT A CLINICAL SCAN") || strings.Contains(body, "ZgotmplZ") {
			t.Fatal("mock scan lacks its patient label or demo disclosure")
		}
		if body == previous {
			t.Fatal("scan series should have distinct illustrations")
		}
		previous = body
		decoder := xml.NewDecoder(strings.NewReader(body))
		for {
			_, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("invalid SVG XML: %v", err)
			}
		}
		for _, cookies := range [][]*http.Cookie{nil, patientCookies} {
			forbidden := serveRequest(handler, httptest.NewRequest(http.MethodGet, url, nil), cookies)
			if forbidden.Code != http.StatusForbidden {
				t.Fatal("unrelated patient or anonymous request could access scan")
			}
		}
	}
	forbidden := serveRequest(handler, httptest.NewRequest(http.MethodGet, "/mock-scans/00000000-0000-4000-8000-000000000103/1.svg", nil), doctorCookies)
	if forbidden.Code != http.StatusForbidden {
		t.Fatal("doctor could access an unlinked patient's scan")
	}
}

func TestBulkDoctorReportsRemainScoped(t *testing.T) {
	a, _ := testApp(t)
	reports, err := a.store.Reports(context.Background(), demoDoctorID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) < 300 {
		t.Fatalf("expected reports for bulk patients, got %d", len(reports))
	}
	for _, report := range reports {
		linked, err := a.store.IsCareTeam(context.Background(), report.PatientID, demoDoctorID)
		if err != nil || !linked {
			t.Fatalf("reports included an unlinked patient: %s", report.PatientID)
		}
	}
}
