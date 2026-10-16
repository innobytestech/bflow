package guard

// LoopCount es cuántas veces se vio una huella en la corrida, y con qué
// herramienta (para el motivo del rechazo y el evento guard).
type LoopCount struct {
	Tool string `json:"tool"`
	N    int    `json:"n"`
}

// LoopState es la cuenta de repeticiones de una corrida de subagente
// (.bflow/cache/loops/<sha256(session)[:16]>.json).
type LoopState struct {
	Counts map[string]LoopCount `json:"counts"`
}

// Observe suma 1 a la huella de a y dice si ya llegó a repeats (deny=true):
// el guard la rechaza. Un Edit o Write permitido reinicia la cuenta de los
// Bash y Read de la corrida (R19): volver a correr las pruebas después de un
// cambio no es un ciclo.
func (s *LoopState) Observe(a Action, repeats int) (n int, deny bool) {
	if s.Counts == nil {
		s.Counts = map[string]LoopCount{}
	}
	c := s.Counts[a.Fingerprint]
	c.Tool = a.Tool
	c.N++
	s.Counts[a.Fingerprint] = c
	n = c.N
	deny = n >= repeats
	if !deny && (a.Tool == Edit || a.Tool == Write) {
		for k, v := range s.Counts {
			if v.Tool == Bash || v.Tool == Read {
				delete(s.Counts, k)
			}
		}
	}
	return n, deny
}
