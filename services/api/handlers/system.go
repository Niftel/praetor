package handlers

import (
	"net/http"
	"os"
	"strings"

	"github.com/praetordev/praetor/services/api/render"
)

// AutomationKey returns Praetor's automation SSH public key. A user adds this to
// a host's authorized_keys (via their own provisioning — cloud-init, an image, a
// config-management run, or by hand) so Praetor can manage that host with no
// per-host credential. Only the public half is ever exposed; the private key
// stays with the executor and never leaves Praetor.
func AutomationKey(w http.ResponseWriter, r *http.Request) {
	path := os.Getenv("AUTOMATION_PUBKEY_PATH")
	if path == "" {
		path = "/etc/praetor/automation_key.pub"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		render.JSON(w, r, map[string]interface{}{"public_key": "", "configured": false})
		return
	}
	render.JSON(w, r, map[string]interface{}{"public_key": strings.TrimSpace(string(data)), "configured": true})
}
