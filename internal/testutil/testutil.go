// Package testutil tiene ayudas para pruebas.
package testutil

import (
	"os"
	"testing"
	"time"
)

// TempDir es como t.TempDir pero reintenta el borrado. En Windows un escáner
// (Defender) puede retener un instante archivos recién creados y el borrado
// falla con "directory is not empty" sin que el código probado tenga culpa.
func TempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bflow-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var err error
		for i := 0; i < 40; i++ {
			if err = os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Logf("no se pudo borrar %s: %v", dir, err)
	})
	return dir
}
