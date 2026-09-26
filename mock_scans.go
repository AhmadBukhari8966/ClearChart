package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Images are generated locally from an SVG illustration and tagged to their
// fictional patient's chart. There are no remote images or real scan files.
func (a *app) mockScan(w http.ResponseWriter, r *http.Request) {
	patientID := r.PathValue("id")
	number, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("scan"), ".svg"))
	if err != nil || number < 1 || number > 2 {
		http.NotFound(w, r)
		return
	}
	allowed := false
	if s, ok := a.sessionFor(r, "patient"); ok && s.ProfileID == patientID {
		allowed = true
	}
	if !allowed {
		if s, ok := a.sessionFor(r, "doctor"); ok {
			allowed, err = a.store.IsCareTeam(r.Context(), patientID, s.ProfileID)
			if err != nil {
				a.readError(w, err)
				return
			}
		}
	}
	if !allowed {
		http.Error(w, "This scan is not part of your current care team.", http.StatusForbidden)
		return
	}
	p, err := a.store.Profile(r.Context(), patientID)
	if err != nil {
		a.readError(w, err)
		return
	}
	if p.Role != "patient" {
		http.NotFound(w, r)
		return
	}
	// The deterministic variation gives each series a distinct appearance while
	// keeping every image unmistakably an illustration for UI testing.
	variant := number * 11
	for _, ch := range patientID {
		variant += int(ch)
	}
	data := struct {
		Patient       Profile
		Number, Angle int
		Reference     string
	}{p, number, variant%31 - 15, fmt.Sprintf("CC-%s-%02d", patientID[len(patientID)-6:], number)}
	fragment, err := a.render("mock_scan.svg", data)
	if err != nil {
		http.Error(w, "Could not render the mock scan.", 500)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	fmt.Fprint(w, fragment)
}
