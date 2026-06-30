package handlers

import (
	"encoding/json"
	"fmt"
)

// surveyQuestion mirrors one entry of an AWX-style survey_spec.spec array. Only
// the fields the launch path needs are modeled; the UI persists the rest.
type surveyQuestion struct {
	Variable string      `json:"variable"`
	Type     string      `json:"type"`
	Required bool        `json:"required"`
	Default  interface{} `json:"default"`
}

// applySurvey resolves submitted answers against a template's survey_spec: it
// fills in defaults for unanswered questions and rejects the launch if a
// required question has neither an answer nor a default. The returned map is the
// extra_vars to run with. Questions not in the spec are dropped, so a survey
// also constrains which variables a launcher may set.
func applySurvey(spec json.RawMessage, answers map[string]interface{}) (map[string]interface{}, error) {
	var s struct {
		Spec []surveyQuestion `json:"spec"`
	}
	if len(spec) > 0 {
		_ = json.Unmarshal(spec, &s)
	}

	out := map[string]interface{}{}
	for _, q := range s.Spec {
		if q.Variable == "" {
			continue
		}
		v, ok := answers[q.Variable]
		if !ok || v == nil || v == "" {
			if q.Default != nil && q.Default != "" {
				out[q.Variable] = q.Default
				continue
			}
			if q.Required {
				return nil, fmt.Errorf("survey question %q is required", q.Variable)
			}
			continue
		}
		out[q.Variable] = v
	}
	return out, nil
}
