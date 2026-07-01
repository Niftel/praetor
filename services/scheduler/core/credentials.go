package core

import (
	"context"
	"encoding/json"
	"log"
	"regexp"

	"github.com/praetordev/praetor/pkg/crypto"
)

// loadAutomationKey returns Praetor's automation SSH private key (decrypted) from
// the database-managed identity, or "" if none is configured. Used as the
// default connection key for jobs that carry no machine credential.
func loadAutomationKey(ctx context.Context, q ctxGetter) string {
	var enc string
	if err := q.GetContext(ctx, &enc, `SELECT private_key FROM automation_identity WHERE id = 1`); err != nil {
		return ""
	}
	dec, err := crypto.DecryptSecret(enc)
	if err != nil {
		log.Printf("automation identity: decrypt failed: %v", err)
		return ""
	}
	return dec
}

// ctxGetter is satisfied by both *sqlx.DB and *sqlx.Tx, so the injector resolver
// can run inside the scheduler's tick transaction or standalone.
type ctxGetter interface {
	GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
}

// injectorVar matches an AWX-style "{{ field }}" reference inside an injector
// template (e.g. "{{ access_key }}").
var injectorVar = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

// resolveCredentialInjectors loads credential credID and its type, decrypts the
// secret input fields, and renders the type's injectors into:
//
//   - env:   environment variable name -> value
//   - files: environment variable name -> file content (the executor writes the
//     content to a temp file and points the env var at the path)
//
// It returns (nil, nil, nil) when credID <= 0. Injector entries that render to an
// empty string (an unset optional field) are dropped so we never export an empty
// AWS_SECURITY_TOKEN and the like.
func resolveCredentialInjectors(ctx context.Context, q ctxGetter, credID int64) (map[string]string, map[string]string, error) {
	if credID <= 0 {
		return nil, nil, nil
	}

	var row struct {
		Inputs     []byte `db:"inputs"`
		TypeInputs []byte `db:"type_inputs"`
		Injectors  []byte `db:"injectors"`
	}
	if err := q.GetContext(ctx, &row, `
		SELECT c.inputs AS inputs, ct.inputs AS type_inputs, ct.injectors AS injectors
		FROM credentials c
		JOIN credential_types ct ON ct.id = c.credential_type_id
		WHERE c.id = $1`, credID); err != nil {
		return nil, nil, err
	}

	// Which input fields are stored encrypted (secret).
	var schema struct {
		Fields []struct {
			ID     string `json:"id"`
			Secret bool   `json:"secret"`
		} `json:"fields"`
	}
	_ = json.Unmarshal(row.TypeInputs, &schema)
	secret := make(map[string]bool)
	for _, f := range schema.Fields {
		if f.Secret {
			secret[f.ID] = true
		}
	}

	// Decrypt secret fields into a flat value map used to render the injectors.
	var rawInputs map[string]interface{}
	_ = json.Unmarshal(row.Inputs, &rawInputs)
	vals := make(map[string]string, len(rawInputs))
	for k, v := range rawInputs {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if secret[k] {
			if dec, err := crypto.DecryptSecret(s); err == nil {
				s = dec // tolerate plaintext (e.g. legacy/unencrypted) by keeping s
			}
		}
		vals[k] = s
	}

	var inj struct {
		Env  map[string]string `json:"env"`
		File map[string]string `json:"file"`
	}
	_ = json.Unmarshal(row.Injectors, &inj)

	var env, files map[string]string
	for k, t := range inj.Env {
		if r := renderInjector(t, vals); r != "" {
			if env == nil {
				env = make(map[string]string)
			}
			env[k] = r
		}
	}
	for k, t := range inj.File {
		if r := renderInjector(t, vals); r != "" {
			if files == nil {
				files = make(map[string]string)
			}
			files[k] = r
		}
	}
	return env, files, nil
}

// renderInjector substitutes every "{{ field }}" reference in an injector
// template with its value from vals (unknown fields render to empty).
func renderInjector(template string, vals map[string]string) string {
	return injectorVar.ReplaceAllStringFunc(template, func(m string) string {
		return vals[injectorVar.FindStringSubmatch(m)[1]]
	})
}
