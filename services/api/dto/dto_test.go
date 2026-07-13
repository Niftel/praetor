package dto

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/praetordev/models"
)

// TestWireShapeMatchesModel is the byte-compatibility contract for the DTO layer.
// For every DTO/model pair it asserts, field by field, that the DTO has the same
// json tag, the same field type and the same field order as the model it mirrors.
// json.Marshal output depends only on those three things, so matching them proves
// the wire bytes are identical — without needing a database or an HTTP round-trip.
//
// Add a row here for every DTO in this package. A mismatch (renamed json tag,
// changed type, reordered field, or a field present on one side only) fails loudly
// at test time instead of silently changing the API contract the frontend depends
// on.
func TestWireShapeMatchesModel(t *testing.T) {
	cases := []struct {
		name  string
		dto   any
		model any
	}{
		{"Project", Project{}, models.Project{}},
		{"Organization", Organization{}, models.Organization{}},
		{"User", User{}, models.User{}},
		{"Team", Team{}, models.Team{}},
		{"Inventory", Inventory{}, models.Inventory{}},
		{"Host", Host{}, models.Host{}},
		{"Group", Group{}, models.Group{}},
		{"CredentialType", CredentialType{}, models.CredentialType{}},
		{"Credential", Credential{}, models.Credential{}},
		{"JobTemplate", JobTemplate{}, models.JobTemplate{}},
		{"Schedule", Schedule{}, models.Schedule{}},
		{"UnifiedJob", UnifiedJob{}, models.UnifiedJob{}},
		{"ExecutionRun", ExecutionRun{}, models.ExecutionRun{}},
		{"JobEvent", JobEvent{}, models.JobEvent{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertWireShape(t, reflect.TypeOf(c.dto), reflect.TypeOf(c.model))
		})
	}
}

// assertWireShape checks that dtoT is a JSON-identical mirror of modelT: the
// sequence of wire-visible fields (json tag, type, order) must match. Model fields
// tagged json:"-" (e.g. User.PasswordHash) never appear on the wire, so the DTO
// legitimately omits them and they are skipped on the model side. Extra struct tags
// (db:"…") don't affect JSON and are ignored.
func assertWireShape(t *testing.T, dtoT, modelT reflect.Type) {
	t.Helper()
	type field struct {
		name string
		tag  string
		typ  reflect.Type
	}
	wireFields := func(rt reflect.Type) []field {
		var fs []field
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			tag := f.Tag.Get("json")
			if tag == "-" { // never serialized
				continue
			}
			fs = append(fs, field{f.Name, tag, f.Type})
		}
		return fs
	}
	got, want := wireFields(dtoT), wireFields(modelT)
	if len(got) != len(want) {
		t.Fatalf("%s has %d wire fields, %s has %d", dtoT, len(got), modelT, len(want))
	}
	for i := range want {
		if got[i].tag != want[i].tag {
			t.Errorf("wire field %d (%s): json tag %q on DTO vs %q on model", i, want[i].name, got[i].tag, want[i].tag)
		}
		if got[i].typ != want[i].typ {
			t.Errorf("wire field %d (%s): type %s on DTO vs %s on model", i, want[i].name, got[i].typ, want[i].typ)
		}
	}
}

// TestProjectWireBytesIdentical is a concrete belt-and-suspenders check that a
// fully-populated project (and one with its omitempty pointers nil) serializes to
// exactly the same bytes through the DTO as through the model.
func TestProjectWireBytesIdentical(t *testing.T) {
	desc, branch := "a description", "main"
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	full := models.Project{
		ID: 7, OrganizationID: 3, Name: "infra", Description: &desc,
		SCMType: "git", SCMURL: "https://example.com/repo.git", SCMBranch: &branch,
		CreatedAt: now, ModifiedAt: now,
	}
	empty := models.Project{ID: 1, Name: "x", CreatedAt: now, ModifiedAt: now} // nil omitempty ptrs

	for _, m := range []models.Project{full, empty} {
		wantBytes, _ := json.Marshal(m)
		gotBytes, _ := json.Marshal(FromProject(m))
		if string(wantBytes) != string(gotBytes) {
			t.Errorf("wire bytes diverged\n model: %s\n   dto: %s", wantBytes, gotBytes)
		}
	}
}
