// Package secrets resuelve credenciales sin que toquen un archivo de
// configuración: primero variables de entorno (útil en CI), luego el llavero
// del sistema operativo a través de un Store que inyecta cmd/bflow.
package secrets

import (
	"errors"
	"net/url"
	"strings"
)

// ErrNotFound indica que no hay secreto para la clave.
var ErrNotFound = errors.New("secreto no encontrado")

// Store es un almacén de secretos (el llavero del sistema en producción).
type Store interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

// Source dice de dónde salió un secreto (para doctor).
type Source string

const (
	FromEnv     Source = "env"
	FromKeyring Source = "keyring"
)

// Key arma la clave de un servicio para un host: "plane:plane.example.com".
func Key(service, host string) string {
	h := strings.TrimSpace(host)
	if u, err := url.Parse(h); err == nil && u.Host != "" {
		h = u.Host
	}
	h = strings.ToLower(strings.TrimSuffix(strings.SplitN(h, "/", 2)[0], "/"))
	return service + ":" + h
}

// Resolver combina variables de entorno y el llavero.
type Resolver struct {
	Store  Store // puede ser nil si el sistema no tiene llavero
	Getenv func(string) string
}

// Get busca en las variables dadas (en orden) y luego en el llavero.
func (r Resolver) Get(key string, envVars ...string) (string, Source, error) {
	for _, v := range envVars {
		if r.Getenv != nil {
			if s := strings.TrimSpace(r.Getenv(v)); s != "" {
				return s, FromEnv, nil
			}
		}
	}
	if r.Store == nil {
		return "", "", ErrNotFound
	}
	s, err := r.Store.Get(key)
	if err != nil {
		return "", "", err
	}
	return s, FromKeyring, nil
}
