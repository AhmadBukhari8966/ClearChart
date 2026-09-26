package main

import "net/http"

func (a *app) bodyMap(w http.ResponseWriter, r *http.Request) {
	role := r.PathValue("role")
	if !validRole(role) {
		http.NotFound(w, r)
		return
	}
	s, ok := a.sessionFor(r, role)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if s.ProfileID != r.PathValue("id") {
		http.Error(w, "This profile does not belong to your account.", http.StatusForbidden)
		return
	}
	d, err := a.dashboardData(r.Context(), s, r.URL.Query().Get("patient"), "")
	if err != nil {
		a.readError(w, err)
		return
	}
	d.BodyHealth = buildAnatomyHealth(d.Records)
	d.Page = "body-map"
	a.page(w, "body_map.html", d)
}
