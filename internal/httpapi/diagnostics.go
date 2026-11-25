package httpapi

import (
	"net/http"

	"vektor/internal/diagnostics"
)

func (a *API) diagnostics(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	runner := a.diag
	if runner == nil {
		runner = &diagnostics.Runner{
			Ctl:  a.ctl,
			Mgmt: a.mgmt,
		}
	}
	report := runner.Run(r.Context())
	writeJSON(w, http.StatusOK, report)
}
