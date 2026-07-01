package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/services/api/render"
)

// AutomationKeyHandler returns Praetor's automation SSH public key from the
// database-managed identity. A user adds this to a host's authorized_keys (via
// their own provisioning) so Praetor can manage that host with no per-host
// credential. Only the public half is exposed; the private key stays encrypted
// in the database and is used solely by the scheduler/executor.
func AutomationKeyHandler(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var pub string
		err := db.GetContext(r.Context(), &pub, `SELECT public_key FROM automation_identity WHERE id = 1`)
		if err == sql.ErrNoRows || err != nil {
			render.JSON(w, r, map[string]interface{}{"public_key": "", "configured": false})
			return
		}
		render.JSON(w, r, map[string]interface{}{"public_key": strings.TrimSpace(pub), "configured": true})
	}
}
