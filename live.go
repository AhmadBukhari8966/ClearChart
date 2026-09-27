package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type change struct{ PatientID, Origin string }
type eventHub struct {
	mu      sync.Mutex
	clients map[chan change]struct{}
	done    chan struct{}
}

func newEventHub() *eventHub {
	return &eventHub{clients: make(map[chan change]struct{}), done: make(chan struct{})}
}
func (h *eventHub) subscribe() (chan change, func()) {
	ch := make(chan change, 1)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() { h.mu.Lock(); delete(h.clients, ch); h.mu.Unlock() }
}
func (h *eventHub) publish(patientID, origin string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- change{patientID, origin}:
		default:
		}
	}
}
func (h *eventHub) close() { close(h.done) }

func startSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
}

// Datastar's wire protocol: every line of multiline HTML needs its own elements
// data field, and two trailing newlines terminate each event.
func patch(w http.ResponseWriter, selector, mode, fragment string) {
	fmt.Fprintf(w, "event: datastar-patch-elements\ndata: selector %s\ndata: mode %s\n", selector, mode)
	for _, line := range strings.Split(strings.ReplaceAll(fragment, "\r\n", "\n"), "\n") {
		fmt.Fprintf(w, "data: elements %s\n", line)
	}
	fmt.Fprint(w, "\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// Bound each write without leaving a deadline armed during the idle interval.
// An expired HTTP/2 stream deadline cannot be extended for the next heartbeat.
func writeStream(w http.ResponseWriter, write func()) error {
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	defer controller.SetWriteDeadline(time.Time{})
	write()
	if err := controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

func (a *app) events(w http.ResponseWriter, r *http.Request) {
	role := r.PathValue("role")
	s, ok := a.sessionFor(r, role)
	if !validRole(role) || !ok || s.ProfileID != r.PathValue("id") {
		http.Error(w, "Session required.", 403)
		return
	}
	filter := timelineFilter{Type: r.URL.Query().Get("type"), Category: r.URL.Query().Get("category")}
	patientID := r.URL.Query().Get("patient")
	chartPatient := ""
	// Each page uses one fixed Datastar action URL. Only the signals change,
	// allowing Datastar to cancel the previous stream on every patient or
	// filter click. The stream keeps the filter for later live updates.
	// Keep the query parameters for bookmarked pages and non-Datastar clients.
	if raw := r.URL.Query().Get("datastar"); raw != "" {
		var signals struct {
			PatientID    string `json:"selectedpatient"`
			ChartPatient string `json:"chartpatient"`
			Type         string `json:"timelinetype"`
			Category     string `json:"timelinecat"`
		}
		if len(raw) > 4096 || json.Unmarshal([]byte(raw), &signals) != nil {
			http.Error(w, "Invalid selection.", http.StatusBadRequest)
			return
		}
		filter = timelineFilter{Type: signals.Type, Category: signals.Category}
		if role == "doctor" {
			if signals.PatientID != "" {
				patientID = signals.PatientID
			}
			chartPatient = signals.ChartPatient
		}
	}
	if !filter.valid() {
		http.Error(w, "Unknown filter.", 400)
		return
	}
	viewID := r.URL.Query().Get("view")
	ch, unsubscribe := a.hub.subscribe()
	defer unsubscribe()
	d, err := a.dashboardData(r.Context(), s, patientID, filter)
	if err != nil {
		a.readError(w, err)
		return
	}
	patientID = d.Patient.ID
	allowed := map[string]bool{patientID: true, s.ProfileID: true}
	for _, p := range d.Patients {
		allowed[p.ID] = true
	}
	startSSE(w)
	// Initial reconciliation also catches changes made while a tab was disconnected.
	d.ViewID = viewID
	if err = writeStream(w, func() {
		fmt.Fprint(w, ": connected\n\n")
		if role == "doctor" {
			a.doctorContext(w, d, chartPatient != patientID)
		}
		a.snapshot(w, d)
	}); err != nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.hub.done:
			return
		case <-ticker.C:
			if time.Now().After(s.Expires) {
				return
			}
		case ev := <-ch:
			if !allowed[ev.PatientID] || (viewID != "" && ev.Origin == viewID) {
				continue
			}
		}
		if current, ok := a.sessionFor(r, role); !ok || current.ProfileID != s.ProfileID {
			return
		}
		previousPatients := d.Patients
		next, loadErr := a.dashboardData(r.Context(), s, patientID, filter)
		err = loadErr
		if err == nil && role == "doctor" && !samePatientDirectory(previousPatients, next.Patients) {
			if err = writeStream(w, func() {
				patch(w, "#care-team-update", "outer", `<div id="care-team-update" class="success-box" role="status">Your patient list has changed. <a href="/">Refresh your workspace to see new connections.</a></div>`)
			}); err != nil {
				return
			}
			continue
		}
		d = next
		if err != nil {
			if err = writeStream(w, func() { fmt.Fprint(w, ": refresh unavailable\n\n") }); err != nil {
				return
			}
			continue
		}
		d.ViewID = viewID
		if err = writeStream(w, func() { a.snapshot(w, d) }); err != nil {
			return
		}
	}
}

// Update only the selected chart. The patient directory, page shell and their
// scroll positions stay untouched. Form field IDs include the patient ID so a
// draft cannot follow the doctor into a different patient's chart. Regular
// snapshots never replace the form, preserving drafts while live updates arrive.
// Filter changes reopen the stream for the same chart (chartpatient signal), so
// the form is replaced only when the doctor switches to another patient.
func (a *app) doctorContext(w http.ResponseWriter, d Dashboard, replaceForm bool) {
	fragments := []struct{ name, selector string }{
		{"doctor-selected-patient", "#selected-patient-summary"},
		{"doctor-timeline", "#health-timeline"},
	}
	if replaceForm {
		fragments = append(fragments, struct{ name, selector string }{"doctor-record-form", "#new-record"})
	}
	for _, fragment := range fragments {
		if h, err := a.render(fragment.name, d); err == nil {
			patch(w, fragment.selector, "outer", h)
		}
	}
	if replaceForm {
		patch(w, "#form-feedback", "outer", `<div id="form-feedback" role="status" aria-live="polite"></div>`)
		chart, _ := json.Marshal(map[string]string{"chartpatient": d.Patient.ID})
		patchSignals(w, string(chart))
	}
}

func patchSignals(w http.ResponseWriter, signals string) {
	fmt.Fprintf(w, "event: datastar-patch-signals\ndata: signals %s\n\n", signals)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// writeTimeline patches the filtered records and the filter bar that reports
// the active selection. Both roles share these fragments.
func (a *app) writeTimeline(w http.ResponseWriter, d Dashboard) {
	if h, e := a.render("timeline-items", d); e == nil {
		patch(w, "#timeline", "inner", h)
	}
	if h, e := a.render("timeline-filters", d); e == nil {
		patch(w, "#timeline-filters", "outer", h)
	}
}

func (a *app) snapshot(w http.ResponseWriter, d Dashboard) {
	var uploads, reports strings.Builder
	for _, u := range d.Uploads {
		if h, e := a.render("upload", u); e == nil {
			uploads.WriteString(h)
			uploads.WriteByte('\n')
		}
	}
	for _, u := range d.Reports {
		if h, e := a.render("report", u); e == nil {
			reports.WriteString(h)
			reports.WriteByte('\n')
		}
	}
	// The persistent empty-state CSS uses :empty, so the containers remain patchable.
	a.writeTimeline(w, d)
	if d.Role == "patient" {
		patch(w, "#uploads", "inner", uploads.String())
	} else {
		patch(w, "#reports", "inner", reports.String())
	}
	if d.Patient.ID != "" {
		if h, e := a.render("ai-summary", d); e == nil {
			patch(w, "#ai-summary", "outer", h)
		}
	}
	if h, e := a.render("healing-panel", d); e == nil {
		patch(w, "#healing-panel", "outer", h)
	}
	patch(w, "#record-count", "outer", fmt.Sprintf(`<strong id="record-count">%d</strong>`, d.RecordCount))
	patch(w, "#upload-count", "outer", fmt.Sprintf(`<strong id="upload-count">%d</strong>`, d.UploadCount))
	if h, e := a.render("notification-badge", notificationPage{Count: d.NotificationCount}); e == nil {
		patch(w, "#notification-badge", "outer", h)
	}
}

// Keep selection and unsaved drafts intact when a new care relationship arrives.
func samePatientDirectory(a, b []Profile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}
