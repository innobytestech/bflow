package cli

import (
	"fmt"
	"os"
	"os/exec"
)

// binStamp identifica el bflow instalado en el PATH (tamaño y fecha). Cambia
// cuando go install o bflow update lo reemplazan; vacío si no se encuentra.
func binStamp() string {
	p, err := exec.LookPath("bflow")
	if err != nil {
		return ""
	}
	st, err := os.Stat(p)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", st.Size(), st.ModTime().UnixNano())
}

// staleCheck dice si el binario instalado cambió desde que arrancó el panel.
type staleCheck struct {
	stamp func() string
	start string
}

func newStaleCheck(stamp func() string) *staleCheck {
	return &staleCheck{stamp: stamp, start: stamp()}
}

func (s *staleCheck) Stale() bool {
	if s == nil || s.start == "" {
		return false
	}
	now := s.stamp()
	return now != "" && now != s.start
}

const staleNotice = "Hay una versión nueva de bflow instalada: reinicia el panel."
