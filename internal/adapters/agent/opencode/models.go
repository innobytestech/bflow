package opencode

// userConfig es el archivo de OpenCode del usuario donde van los overrides de modelo.
const userConfig = "opencode.json"

// ModelSync es el resultado de llevar los overrides a opencode.json.
type ModelSync struct {
	Path    string            // opencode.json (o .jsonc si es el que existe)
	Changed bool              // se escribió el archivo
	Paste   string            // bloque {"agent":{...}} si no se pudo editar; "" si se editó
	Want    map[string]string // subagente → proveedor/modelo
}

// SyncModels lleva want a agent.<subagente>.model de opencode.json (R6-R9).
// STUB (T1): lo implementa T3.
func (Agent) SyncModels(home string, want map[string]string) (ModelSync, error) {
	return ModelSync{}, nil
}

// ModelsState compara opencode.json con want: ok|stale|manual (R11).
// STUB (T1): lo implementa T3.
func (Agent) ModelsState(home string, want map[string]string) (ModelSync, string) {
	return ModelSync{}, ""
}
