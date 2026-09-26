package flow

import (
	"fmt"
	"slices"
)

// Config es lo que el núcleo necesita saber del proyecto.
type Config struct {
	// Lanes: fases que recorre cada carril, en el orden de Order.
	Lanes map[Lane][]Phase
	// Agents: agentes que trabajan en cada fase. En Quality corren en paralelo;
	// en las demás fases, uno tras otro en el orden dado.
	Agents map[Phase][]string
	// MaxQualityRounds: al llegar a este número de rechazos de calidad se
	// pregunta al humano antes de otra ronda.
	MaxQualityRounds int
}

// DefaultLanes son los carriles por defecto. `paused` solo en full.
func DefaultLanes() map[Lane][]Phase {
	return map[Lane][]Phase{
		Full:   {Discovery, Spec, Contract, Implementing, Paused, Quality, Documenting, Walkthrough, InReview, Done},
		Light:  {Spec, Implementing, Quality, Documenting, Walkthrough, InReview, Done},
		Hotfix: {Implementing, Quality, Documenting, Walkthrough, InReview, Done},
	}
}

// DefaultConfig es la configuración sin bflow.yaml.
func DefaultConfig() Config {
	return Config{
		Lanes: DefaultLanes(),
		Agents: map[Phase][]string{
			Spec:         {"spec-author"},
			Contract:     {"implementer"},
			Implementing: {"implementer"},
			Quality:      {"reviewer", "security-auditor"},
			Documenting:  {"documenter"},
		},
		MaxQualityRounds: 2,
	}
}

// Validate comprueba que la configuración respete las reglas del flujo.
func (c Config) Validate() error {
	if c.MaxQualityRounds < 1 {
		return fmt.Errorf("max_quality_rounds debe ser ≥ 1 (es %d)", c.MaxQualityRounds)
	}
	if len(c.Lanes) == 0 {
		return fmt.Errorf("no hay carriles definidos")
	}
	for lane, phases := range c.Lanes {
		if err := validLane(lane, phases); err != nil {
			return err
		}
	}
	for _, p := range []Phase{Implementing, Quality, Documenting} {
		if len(c.Agents[p]) == 0 {
			return fmt.Errorf("la fase %s necesita al menos un agente", p)
		}
	}
	for _, lane := range c.Lanes {
		for _, p := range []Phase{Spec, Contract} {
			if slices.Contains(lane, p) && len(c.Agents[p]) == 0 {
				return fmt.Errorf("la fase %s necesita al menos un agente", p)
			}
		}
	}
	return nil
}

func validLane(lane Lane, phases []Phase) error {
	if len(phases) == 0 {
		return fmt.Errorf("carril %s: sin fases", lane)
	}
	last := -1
	for _, p := range phases {
		i := slices.Index(Order, p)
		if i < 0 {
			return fmt.Errorf("carril %s: fase desconocida %q", lane, p)
		}
		if i <= last {
			return fmt.Errorf("carril %s: fases fuera de orden o repetidas en %q", lane, p)
		}
		last = i
	}
	// Ningún carril se salta la compuerta de calidad ni el cierre por merge.
	for _, must := range []Phase{Implementing, Quality, Walkthrough, InReview, Done} {
		if !slices.Contains(phases, must) {
			return fmt.Errorf("carril %s: la fase %s es obligatoria", lane, must)
		}
	}
	if slices.Contains(phases, Contract) && !slices.Contains(phases, Spec) {
		return fmt.Errorf("carril %s: contract requiere spec", lane)
	}
	return nil
}

func (c Config) has(l Lane, p Phase) bool { return slices.Contains(c.Lanes[l], p) }

// after devuelve la fase que sigue a p en el carril.
func (c Config) after(l Lane, p Phase) Phase {
	phases := c.Lanes[l]
	i := slices.Index(phases, p)
	if i < 0 || i+1 >= len(phases) {
		return Done
	}
	return phases[i+1]
}
