package cli

import (
	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/metrics"
)

// readReadEvents lee reads-all.jsonl; si no puede, devuelve ReadsSummary{} (R19).
func readReadEvents(e *engine.Engine, id string) metrics.ReadsSummary { panic("TODO") }

// renderReads es el bloque de texto de stats --reads (R13, R16).
func renderReads(s metrics.ReadsSummary) string { panic("TODO") }
