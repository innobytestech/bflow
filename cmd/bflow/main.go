// Command bflow es Byteflow by innobytes: motor de flujo Spec-Driven Development.
//
// Este paquete es la raíz de composición: el único lugar que conoce a la vez el
// núcleo (internal/...) y los adaptadores (internal/adapters/...).
package main

import (
	"os"

	"innobytes.tech/bflow/internal/adapters/agent/claude"
	"innobytes.tech/bflow/internal/cli"
)

// version se inyecta con -ldflags "-X main.version=…".
var version = "dev"

func main() {
	dir, _ := os.Getwd()
	os.Exit(cli.Run(os.Args[1:], &cli.Env{
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Stdin:    os.Stdin,
		Version:  version,
		Dir:      dir,
		Build:    build,
		Connect:  connect,
		Secrets:  secretStore(),
		Agent:    claude.Agent{},
		Projects: projects,
	}))
}
