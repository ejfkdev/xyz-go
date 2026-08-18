package registry

import (
	"context"
	"testing"

	"github.com/ejfkdev/xyz-go/spec"
)

type args struct {
	Name string `json:"name"`
}

func mkEntry(t *testing.T, name string) *spec.Entry {
	t.Helper()
	e, err := spec.Define(name, func(_ context.Context, in *args) (*args, error) {
		return in, nil
	}).Entry()
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	return e
}

func TestAddGet(t *testing.T) {
	r := New()
	e := mkEntry(t, "demo.one")
	if err := r.Add(e); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got, ok := r.Get("demo.one"); !ok || got != e {
		t.Fatal("Get returned wrong entry")
	}
	if _, ok := r.Get("demo.two"); ok {
		t.Fatal("Get should miss")
	}
}

func TestAddDuplicate(t *testing.T) {
	r := New()
	if err := r.Add(mkEntry(t, "demo.dup")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Add(mkEntry(t, "demo.dup")); err == nil {
		t.Fatal("want duplicate error")
	}
}

func TestDefaultRegistrarWired(t *testing.T) {
	// registry 包的 init 必须把默认注册表接到 spec.RegisterDefault 入口上。
	if spec.DefaultRegistrar == nil {
		t.Fatal("spec.DefaultRegistrar not wired to the default registry")
	}
	if err := Add(mkEntry(t, "wired.cmd")); err != nil {
		t.Fatalf("Add into Default: %v", err)
	}
	if _, ok := Default.Get("wired.cmd"); !ok {
		t.Fatal("Default registry should contain the entry")
	}
}

func TestRegisterDefaultFlowsIntoDefault(t *testing.T) {
	_, err := spec.Define("wired.two", func(_ context.Context, in *args) (*args, error) {
		return in, nil
	}).RegisterDefault()
	if err != nil {
		t.Fatalf("RegisterDefault: %v", err)
	}
	if _, ok := Default.Get("wired.two"); !ok {
		t.Fatal("RegisterDefault should land in Default")
	}
}

func TestAddNil(t *testing.T) {
	r := New()
	if err := r.Add(nil); err == nil {
		t.Fatal("want error for nil entry")
	}
}

func TestNamesSorted(t *testing.T) {
	r := New()
	for _, n := range []string{"b.cmd", "a.cmd", "c.cmd"} {
		if err := r.Add(mkEntry(t, n)); err != nil {
			t.Fatalf("Add %s: %v", n, err)
		}
	}
	names := r.Names()
	want := []string{"a.cmd", "b.cmd", "c.cmd"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}
