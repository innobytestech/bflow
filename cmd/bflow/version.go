package main

import "runtime/debug"

// buildVersion es la versión inyectada al publicar; si no hay (go install
// …@v0.1.0 o @latest), la del módulo que Go guarda en el binario. Compilado
// desde el código queda "dev".
func buildVersion(injected string, info func() (*debug.BuildInfo, bool)) string {
	if injected != "dev" {
		return injected
	}
	if bi, ok := info(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return injected
}
