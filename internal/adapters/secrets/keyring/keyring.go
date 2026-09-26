// Package keyring guarda secretos en el llavero del sistema operativo
// (Credential Manager en Windows, Keychain en macOS, Secret Service en Linux).
package keyring

import (
	"errors"

	gokeyring "github.com/zalando/go-keyring"

	"innobytes.tech/bflow/internal/secrets"
)

const service = "bflow"

// Store implementa secrets.Store sobre el llavero del sistema.
type Store struct{}

var _ secrets.Store = Store{}

func (Store) Get(key string) (string, error) {
	v, err := gokeyring.Get(service, key)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return "", secrets.ErrNotFound
	}
	return v, err
}

func (Store) Set(key, value string) error { return gokeyring.Set(service, key, value) }

func (Store) Delete(key string) error {
	err := gokeyring.Delete(service, key)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return nil
	}
	return err
}
