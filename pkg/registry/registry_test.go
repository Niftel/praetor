package registry

import (
	"reflect"
	"testing"
)

func TestRegisterGetNames(t *testing.T) {
	r := New[int]("thing")
	r.Register("b", 2)
	r.Register("a", 1)

	if v, ok := r.Get("a"); !ok || v != 1 {
		t.Errorf("Get(a) = %d,%v want 1,true", v, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Errorf("Get(missing) should be false")
	}
	if got := r.Names(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Names() = %v want [a b] (sorted)", got)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("expected panic on duplicate registration")
		}
	}()
	r := New[int]("thing")
	r.Register("x", 1)
	r.Register("x", 2) // must panic
}
