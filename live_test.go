package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func openTestStream(t *testing.T, handler http.Handler, cookies []*http.Cookie, path string) *bufio.Scanner {
	t.Helper()
	server := httptest.NewServer(handler)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
	if err != nil {
		cancel()
		server.Close()
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	response, err := server.Client().Do(r)
	if err != nil {
		cancel()
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); response.Body.Close(); server.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("event stream status = %d", response.StatusCode)
	}
	return bufio.NewScanner(response.Body)
}

func readTestSnapshot(t *testing.T, scanner *bufio.Scanner) string {
	t.Helper()
	var result strings.Builder
	lastEvent := false
	for scanner.Scan() {
		line := scanner.Text()
		result.WriteString(line)
		result.WriteByte('\n')
		if line == "data: selector #upload-count" {
			lastEvent = true
		}
		if lastEvent && line == "" {
			return result.String()
		}
	}
	t.Fatalf("stream ended before a complete snapshot: %v", scanner.Err())
	return ""
}

func timelineEvent(snapshot string) string {
	for _, event := range strings.Split(snapshot, "\n\n") {
		if strings.Contains(event, "data: selector #timeline\n") {
			return event
		}
	}
	return ""
}

func TestLiveDoctorStreamReceivesPatientUpload(t *testing.T) {
	a, handler := testApp(t)
	patientCookies, patient, _ := demoSession(t, a, handler, "patient")
	doctorCookies, doctor, _ := demoSession(t, a, handler, "doctor")
	stream := openTestStream(t, handler, doctorCookies, "/events/doctor/"+doctor.ProfileID+"?patient="+patient.ProfileID+"&view=doctor-reports-tab")
	readTestSnapshot(t, stream)

	filename := "live-upload-from-patient.pdf"
	r := uploadRequest(t, url.Values{"csrf": {patient.CSRF}, "patient_id": {patient.ProfileID}, "view_id": {"patient-upload-tab"}}, filename, []byte("synthetic report contents"))
	w := serveRequest(handler, r, patientCookies)
	assertSSE(t, w, "#uploads", "prepend")
	snapshot := readTestSnapshot(t, stream)
	if strings.Count(snapshot, filename) != 1 || !strings.Contains(snapshot, "data: selector #reports\n") {
		t.Fatal("doctor's live reports center did not receive exactly one copy of the new upload")
	}
	if strings.Contains(snapshot, "ankle-recovery-notes.pdf") || strings.Contains(snapshot, "synthetic report contents") {
		t.Fatal("live stream exposed an unlinked patient's report or uploaded file bytes")
	}
}

func TestLivePatientStreamPreservesFilterAndPatientScope(t *testing.T) {
	a, handler := testApp(t)
	patientCookies, patient, _ := demoSession(t, a, handler, "patient")
	doctorCookies, doctor, _ := demoSession(t, a, handler, "doctor")
	stream := openTestStream(t, handler, patientCookies, "/events/patient/"+patient.ProfileID+"?type=imaging&view=filtered-patient-tab")
	checkTimeline := func(snapshot string) {
		t.Helper()
		timeline := timelineEvent(snapshot)
		if timeline == "" || !strings.Contains(timeline, `timeline-marker imaging`) {
			t.Fatal("filtered stream did not contain the patient's imaging timeline")
		}
		if strings.Contains(timeline, `timeline-marker note`) || strings.Contains(timeline, `timeline-marker prescription`) || strings.Contains(timeline, "Simulated shoulder") || strings.Contains(timeline, "Simulated ankle") {
			t.Fatal("filtered stream exposed the wrong record type or another patient's records")
		}
	}
	checkTimeline(readTestSnapshot(t, stream))
	for _, kind := range []string{"note", "imaging"} {
		marker := "Live filtered " + kind + " verification"
		w := postForm(handler, "/add-record", url.Values{"csrf": {doctor.CSRF}, "doctor_id": {doctor.ProfileID}, "patient_id": {patient.ProfileID}, "type": {kind}, "content": {marker}, "view_id": {"doctor-author-tab"}}, doctorCookies)
		assertSSE(t, w, "#timeline", "prepend")
		snapshot := readTestSnapshot(t, stream)
		checkTimeline(snapshot)
		if got := strings.Contains(timelineEvent(snapshot), marker); got != (kind == "imaging") {
			t.Fatalf("live %s record visibility in imaging filter = %t", kind, got)
		}
	}
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

func TestStreamWritesClearDeadlineForIdleInterval(t *testing.T) {
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	if err := writeStream(w, func() {
		if w.deadline.IsZero() || !w.deadline.After(time.Now()) {
			t.Error("stream write was not bounded by a future deadline")
		}
		fmt.Fprint(w, ": heartbeat\n\n")
	}); err != nil {
		t.Fatal(err)
	}
	if !w.deadline.IsZero() {
		t.Fatal("stream left a deadline armed during its idle interval")
	}
	// ResponseController wraps ErrNotSupported for ordinary recorders and some
	// middleware. Such writers must still receive a snapshot.
	plain := httptest.NewRecorder()
	if err := writeStream(plain, func() { fmt.Fprint(plain, ": ready\n\n") }); err != nil || !strings.Contains(plain.Body.String(), "ready") {
		t.Fatalf("writer without deadline support was rejected: %v", err)
	}
}
