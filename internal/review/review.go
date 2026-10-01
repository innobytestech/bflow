// Package review mide cuánto del diff leyó el reviewer y lee del review-map lo
// que bflow exige. Es lógica pura: el git y el disco los pone el engine.
//
// STUB (GH-20 T1): solo tipos y firmas; la lógica llega en T2.
package review

import (
	"io"
	"time"

	"innobytes.tech/bflow/internal/guard"
)

const (
	// ReadsFile es el archivo de la tarea con las lecturas del reviewer.
	ReadsFile = "reads.jsonl"
	// WholeDiffMax son las líneas bajo las cuales un git diff sin ruta cubre todo.
	WholeDiffMax = 1500
)

// Read es una herramienta que usó el reviewer, tal como se guarda.
type Read struct {
	TS       time.Time `json:"ts"`
	Tool     string    `json:"tool"`            // guard.Read | guard.Bash | guard.Edit | guard.Write
	Paths    []string  `json:"paths,omitempty"` // relativas al repo, con /; pueden ser carpetas (pathspec de git)
	Whole    bool      `json:"whole,omitempty"` // git diff sin ruta
	ReadHook bool      `json:"read_hook"`       // vino de guard --reads
}

// Normalize deja p como ruta relativa al repo con "/" (R4). ok es false si cae
// fuera del repo. Acepta absolutas, relativas a cwd, con "\" o "/", "/c/..." de
// Git Bash y la unidad de Windows en cualquier caja.
func Normalize(root, cwd, p string) (string, bool) { return "", false }

// FromAction traduce una acción del reviewer a lo que se guarda (R2, R3).
func FromAction(a guard.Action, root, cwd string, readHook bool, now time.Time) Read {
	return Read{}
}

// ParseReads lee reads.jsonl y salta las líneas inválidas.
func ParseReads(r io.Reader) []Read { return nil }

// IsDoc dice si la ruta es documentación: .md .mdx .rst .txt .adoc o bajo docs/.
func IsDoc(path string) bool { return false }

// Map es lo que bflow lee del review-map.
type Map struct {
	Red       []string // rutas de las viñetas 🔴, normalizadas (sin :línea ni #L), sin duplicados
	RedNoPath []string // viñetas 🔴 sin ruta inicial, recortadas a 80 runas
	Docs      []string // rutas de ## Docs
	HasDocs   bool
}

// ParseMap lee las secciones 🔴 y Docs del review-map.
func ParseMap(md string) Map { return Map{} }

// Coverage es la cobertura de una corrida del reviewer.
type Coverage struct {
	Measured    bool     `json:"measured"`
	Total       int      `json:"total"` // archivos del diff
	Read        int      `json:"read"`
	Whole       bool     `json:"whole,omitempty"`  // un diff entero cubrió todo
	Unread      []string `json:"unread,omitempty"` // no leídos que no son pruebas ni docs
	UnreadOther int      `json:"unread_other,omitempty"`
	RedMissing  []string `json:"red_missing,omitempty"`
	RedOutside  []string `json:"red_outside,omitempty"`
}

// Measure calcula la cobertura de reads contra los archivos del diff (R6, R10, R11).
func Measure(diff []string, diffLines int, reads []Read, red []string, isTest func(string) bool) Coverage {
	return Coverage{}
}

// Lines es el texto de la cobertura para el display y stats, sin emojis (R13).
func (c Coverage) Lines() []string { return nil }

// DocsPending devuelve las rutas de listed que no están en diff ni constan en
// docsReport como "- `ruta`: sin cambio: <motivo>" con motivo (R15).
func DocsPending(listed, diff []string, docsReport string) []string { return nil }
