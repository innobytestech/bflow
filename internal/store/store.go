// Package store persiste el estado de trabajo en .bflow/ (ignorado por git):
// un state.json por tarea, un log.jsonl de solo agregar y archivos de trabajo
// (contrato, reportes, check). Varias sesiones o procesos pueden tocar la misma
// tarea, así que toda lectura-modificación-escritura va bajo un lock de archivo
// y toda escritura es atómica (temporal + rename).
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"innobytes.tech/bflow/internal/flow"
)

// SchemaVersion es la versión del formato de state.json.
const SchemaVersion = 1

var (
	ErrNotFound = errors.New("tarea no encontrada en .bflow")
	ErrLocked   = errors.New("la tarea está bloqueada por otro proceso de bflow")
)

// PR es el pull request de la tarea.
type PR struct {
	Number int    `json:"number,omitempty"`
	URL    string `json:"url"`
	Merged bool   `json:"merged,omitempty"`
}

// Record es lo que se guarda por tarea: el estado de flujo del núcleo más
// datos operativos que el núcleo no necesita.
type Record struct {
	Schema    int               `json:"schema"`
	Flow      flow.State        `json:"flow"`
	Title     string            `json:"title,omitempty"`
	URL       string            `json:"url,omitempty"` // la tarea en el tracker
	Branch    string            `json:"branch,omitempty"`
	PR        *PR               `json:"pr,omitempty"`
	Adapter   map[string]string `json:"adapter,omitempty"`    // datos propios del adaptador del tracker
	Pending   []flow.Effect     `json:"pending,omitempty"`    // efectos del tracker que fallaron y se reintentan
	Since     time.Time         `json:"since"`                // entrada a la fase actual
	GateSince time.Time         `json:"gate_since,omitempty"` // apertura del gate pendiente (SLA)
	Nudges    map[string]int    `json:"nudges,omitempty"`     // agente → veces que terminó sin reportar desde el último evento
	Created   time.Time         `json:"created"`
	Updated   time.Time         `json:"updated"`
}

// Entry es una línea de log.jsonl.
type Entry struct {
	TS      time.Time      `json:"ts"`
	ID      string         `json:"id"`
	Event   string         `json:"event"`
	From    flow.Phase     `json:"from,omitempty"`
	To      flow.Phase     `json:"to,omitempty"`
	Gate    string         `json:"gate,omitempty"`
	Agent   string         `json:"agent,omitempty"`
	Verdict string         `json:"verdict,omitempty"`
	Round   int            `json:"round,omitempty"`
	Note    string         `json:"note,omitempty"`
	By      string         `json:"by,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// Store es el acceso a .bflow/ de un repo.
type Store struct {
	Root        string
	Now         func() time.Time
	LockTimeout time.Duration // cuánto esperar un lock ocupado
	StaleAfter  time.Duration // antigüedad a partir de la cual un lock se considera huérfano
}

// Open devuelve el store del repo en root.
func Open(root string) *Store {
	return &Store{Root: root, Now: time.Now, LockTimeout: 10 * time.Second, StaleAfter: 2 * time.Minute}
}

// Dir es la carpeta .bflow.
func (s *Store) Dir() string { return filepath.Join(s.Root, ".bflow") }

var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidID indica si id sirve como nombre de carpeta de tarea.
func ValidID(id string) bool { return idRe.MatchString(id) && !strings.Contains(id, "..") }

func (s *Store) taskDir(id string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("identificador de tarea inválido: %q", id)
	}
	return filepath.Join(s.Dir(), "tasks", id), nil
}

func (s *Store) ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return ignoreSelf(s.Dir())
}

// ignoreSelf escribe .bflow/.gitignore: .bflow/ se ignora a sí misma, aunque
// el repo no la tenga en su .gitignore.
func ignoreSelf(bflowDir string) error {
	gi := filepath.Join(bflowDir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(gi, []byte("*\n"), 0o644)
	}
	return nil
}

// EnsureDirFor crea la carpeta de path, que vive dentro de <raíz>/.bflow/, y
// el .gitignore de .bflow/. Es para quien escribe ahí sin pasar por el Store
// (cachés de adaptadores, statusline, cursor de tokens).
func EnsureDirFor(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for d := dir; ; d = filepath.Dir(d) {
		if filepath.Base(d) == ".bflow" {
			return ignoreSelf(d)
		}
		if filepath.Dir(d) == d {
			return nil
		}
	}
}

// Load lee el estado de una tarea.
func (s *Store) Load(id string) (Record, error) {
	dir, err := s.taskDir(id)
	if err != nil {
		return Record{}, err
	}
	return readRecord(filepath.Join(dir, "state.json"))
}

func readRecord(path string) (Record, error) {
	var r Record
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("%s está dañado: %w", path, err)
	}
	if r.Schema > SchemaVersion {
		return r, fmt.Errorf("%s usa el formato %d; actualiza bflow (esta versión entiende hasta %d)", path, r.Schema, SchemaVersion)
	}
	return r, nil
}

// Update lee, modifica y escribe una tarea bajo lock. Si fn devuelve error no
// se escribe nada. exists indica si la tarea ya estaba en .bflow.
func (s *Store) Update(id string, fn func(r *Record, exists bool) error) (Record, error) {
	dir, err := s.taskDir(id)
	if err != nil {
		return Record{}, err
	}
	if err := s.ensureDir(dir); err != nil {
		return Record{}, err
	}
	unlock, err := s.lock(filepath.Join(dir, ".lock"))
	if err != nil {
		return Record{}, fmt.Errorf("%s: %w", id, err)
	}
	defer unlock()

	path := filepath.Join(dir, "state.json")
	rec, err := readRecord(path)
	exists := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Record{}, err
	}
	prevPhase := rec.Flow.Phase
	if err := fn(&rec, exists); err != nil {
		return Record{}, err
	}
	now := s.Now()
	rec.Schema = SchemaVersion
	if !exists {
		rec.Created = now
		rec.Since = now
	}
	if exists && rec.Flow.Phase != prevPhase {
		rec.Since = now
	}
	rec.Updated = now
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return Record{}, err
	}
	if err := WriteAtomic(path, append(b, '\n')); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// List devuelve todas las tareas, ordenadas por prefijo y número.
func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(filepath.Join(s.Dir(), "tasks"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := readRecord(filepath.Join(s.Dir(), "tasks", e.Name(), "state.json"))
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return lessID(out[i].Flow.ID, out[j].Flow.ID) })
	return out, nil
}

// lessID ordena "ABC-2" antes que "ABC-10".
func lessID(a, b string) bool {
	pa, na := splitID(a)
	pb, nb := splitID(b)
	if pa != pb {
		return pa < pb
	}
	if na != nb {
		return na < nb
	}
	return a < b
}

func splitID(id string) (string, int) {
	i := strings.LastIndexAny(id, "-_")
	if i < 0 {
		return id, 0
	}
	n, err := strconv.Atoi(id[i+1:])
	if err != nil {
		return id, 0
	}
	return id[:i], n
}

// Path devuelve la ruta de un archivo de trabajo de la tarea.
func (s *Store) Path(id, rel string) (string, error) {
	dir, err := s.taskDir(id)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if r, err := filepath.Rel(dir, p); err != nil || strings.HasPrefix(r, "..") {
		return "", fmt.Errorf("ruta fuera de la carpeta de la tarea: %q", rel)
	}
	return p, nil
}

// WriteFile guarda un archivo de trabajo de la tarea (atómico).
func (s *Store) WriteFile(id, rel string, data []byte) error {
	p, err := s.Path(id, rel)
	if err != nil {
		return err
	}
	if err := s.ensureDir(filepath.Dir(p)); err != nil {
		return err
	}
	return WriteAtomic(p, data)
}

// ReadFile lee un archivo de trabajo de la tarea.
func (s *Store) ReadFile(id, rel string) ([]byte, error) {
	p, err := s.Path(id, rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// Append agrega entradas a log.jsonl. Cada entrada es una línea escrita con
// una sola llamada, bajo el lock del log.
func (s *Store) Append(entries ...Entry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := s.ensureDir(s.Dir()); err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	unlock, err := s.lock(filepath.Join(s.Dir(), ".log.lock"))
	if err != nil {
		return fmt.Errorf("log: %w", err)
	}
	defer unlock()
	f, err := os.OpenFile(filepath.Join(s.Dir(), "log.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Log lee el log; si id no es vacío, solo las entradas de esa tarea.
func (s *Store) Log(id string) ([]Entry, error) {
	f, err := os.Open(filepath.Join(s.Dir(), "log.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("log.jsonl línea %d: %w", line, err)
		}
		if id == "" || e.ID == id {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

func (s *Store) lock(path string) (func(), error) { return LockFile(path, s.LockTimeout, s.StaleAfter) }

// LockFile crea path en exclusiva. Si ya existe, espera hasta timeout; si es
// más viejo que stale, lo considera de un proceso muerto y lo rompe.
func LockFile(path string, timeout, stale time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "pid %d %s\n", os.Getpid(), time.Now().Format(time.RFC3339Nano))
			f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > stale {
			// Renombrar antes de borrar: si dos procesos ven el mismo lock
			// huérfano, solo uno logra moverlo.
			tomb := fmt.Sprintf("%s.stale.%d.%d", path, os.Getpid(), rand.Int64())
			if os.Rename(path, tomb) == nil {
				_ = os.Remove(tomb)
			}
			continue
		}
		if time.Now().After(deadline) {
			return nil, ErrLocked
		}
		time.Sleep(time.Duration(2+rand.IntN(8)) * time.Millisecond)
	}
}

// WriteAtomic escribe en un temporal del mismo directorio y lo renombra. En
// Windows el rename falla si otro proceso tiene el destino abierto; se reintenta.
func WriteAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	for i := 0; ; i++ {
		err = os.Rename(name, path)
		if err == nil || i >= 50 {
			break
		}
		time.Sleep(time.Duration(2+i) * time.Millisecond)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}
