package migrate

import (
	"context"
	"errors"
	"testing"
)

type fake struct{ from, to string }

func (f fake) From() string { return f.from }
func (f fake) To() string   { return f.to }
func (f fake) Migrate(context.Context, Request) (Result, error) {
	return Result{Migrated: 1}, nil
}

func TestEmptyRegistryHasNoTestedMigrator(t *testing.T) {
	_, err := NewRegistry().Lookup("engram", "memory-mcp")
	if !errors.Is(err, ErrNoTestedMigrator) {
		t.Fatalf("want ErrNoTestedMigrator, got %v", err)
	}
}

func TestRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fake{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(fake{"a", "b"}); err == nil {
		t.Fatal("duplicate must fail")
	}
	if m, err := r.Lookup("a", "b"); err != nil || m == nil {
		t.Fatalf("lookup: %v", err)
	}
	if _, err := r.Lookup("b", "a"); !errors.Is(err, ErrNoTestedMigrator) {
		t.Fatal("reverse pair must be unregistered")
	}
}
