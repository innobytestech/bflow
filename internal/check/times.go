package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"innobytes.tech/bflow/internal/store"
)

// Un paso que pasa en mucho menos tiempo que antes suele haberse saltado algo:
// pruebas de integración sin su variable de entorno o sin sus tags. En la
// piloto de BE-05 test pasó en 22.7 s contra 227 s y nadie lo notó en el check.
const (
	minRefSecs  = 20  // por debajo, la variación normal da falsos avisos
	fastRatio   = 0.2 // más rápido que esto respecto a la referencia es sospechoso
	acceptAfter = 3   // corridas rápidas seguidas para tomarlo como el tiempo nuevo
)

// timeRef es la duración de un paso en la última corrida verde. Se guarda por
// paso y cantidad de paquetes, porque {affected} cambia el tamaño a propósito, y
// vale mientras el comando no cambie.
type timeRef struct {
	Run  string  `json:"run"`
	Secs float64 `json:"secs"`
	Fast int     `json:"fast,omitempty"` // corridas rápidas seguidas
}

func timesPath(st *store.Store) string {
	return filepath.Join(st.Dir(), "cache", "check-times.json")
}

// CompareTimes avisa en res de los pasos que pasaron mucho más rápido que en la
// última corrida verde y, si el check pasó, actualiza las referencias. Una
// corrida sospechosa no baja la referencia hasta repetirse acceptAfter veces.
func CompareTimes(st *store.Store, res *Result) error {
	path := timesPath(st)
	refs := map[string]timeRef{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &refs)
	}
	next := map[string]timeRef{}
	for k, v := range refs {
		next[k] = v
	}
	for _, s := range res.Steps {
		if s.Status != "pass" || s.Secs <= 0 {
			continue
		}
		key := s.Name
		if s.Detail != "" {
			key += " (" + s.Detail + ")"
		}
		ref, ok := refs[key]
		fast := ok && ref.Run == s.Cmd && ref.Secs >= minRefSecs && s.Secs < ref.Secs*fastRatio
		if fast {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s tardó %.0f s y la última corrida verde %.0f s: puede que se saltaran pruebas (variables de entorno, tags). Revísalo antes de reportar DONE.", s.Name, s.Secs, ref.Secs))
		}
		switch {
		case fast && ref.Fast+1 < acceptAfter:
			ref.Fast++
			next[key] = ref
		default:
			next[key] = timeRef{Run: s.Cmd, Secs: s.Secs}
		}
	}
	if res.Result != "PASS" {
		return nil
	}
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := store.EnsureDirFor(path); err != nil {
		return err
	}
	return store.WriteAtomic(path, b)
}
