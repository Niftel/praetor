package handlers

import (
	"encoding/json"
	"testing"
)

func TestValidateCredentialTypeSpec(t *testing.T) {
	cases := []struct {
		name      string
		inputs    string
		injectors string
		wantErr   bool
	}{
		{
			name:      "valid: injector refs a declared field",
			inputs:    `{"fields":[{"id":"token","label":"Token","type":"password","secret":true}]}`,
			injectors: `{"env":{"API_TOKEN":"{{ token }}"}}`,
		},
		{
			name:      "valid: empty inputs and injectors",
			inputs:    `{}`,
			injectors: `{}`,
		},
		{
			name:      "invalid: injector references an undefined field",
			inputs:    `{"fields":[{"id":"token","label":"Token","type":"password"}]}`,
			injectors: `{"env":{"X":"{{ nope }}"}}`,
			wantErr:   true,
		},
		{
			name:    "invalid: field missing id",
			inputs:  `{"fields":[{"label":"No id","type":"text"}]}`,
			wantErr: true,
		},
		{
			name:    "invalid: unsupported field type",
			inputs:  `{"fields":[{"id":"x","label":"X","type":"number"}]}`,
			wantErr: true,
		},
		{
			name:    "invalid: duplicate field id",
			inputs:  `{"fields":[{"id":"x","type":"text"},{"id":"x","type":"text"}]}`,
			wantErr: true,
		},
		{
			name:    "invalid: inputs not JSON",
			inputs:  `not json`,
			wantErr: true,
		},
		{
			name:      "valid: file injector ref",
			inputs:    `{"fields":[{"id":"key","label":"Key","type":"textarea","secret":true}]}`,
			injectors: `{"file":{"KEY_FILE":"{{ key }}"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCredentialTypeSpec(json.RawMessage(tc.inputs), json.RawMessage(tc.injectors))
			if (err != nil) != tc.wantErr {
				t.Errorf("validateCredentialTypeSpec() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
