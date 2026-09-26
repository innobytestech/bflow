package keyring

import (
	"errors"
	"testing"

	gokeyring "github.com/zalando/go-keyring"

	"innobytes.tech/bflow/internal/secrets"
)

func TestRoundTripWithMock(t *testing.T) {
	gokeyring.MockInit()
	var s Store
	if _, err := s.Get("plane:x"); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("vacío: %v", err)
	}
	if err := s.Set("plane:x", "tok"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get("plane:x"); err != nil || v != "tok" {
		t.Fatalf("get: %q %v", v, err)
	}
	if err := s.Delete("plane:x"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("plane:x"); err != nil {
		t.Errorf("borrar dos veces no debe fallar: %v", err)
	}
}
