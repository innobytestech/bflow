package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"innobytes.tech/bflow/internal/engine"
	"innobytes.tech/bflow/internal/metrics"
)

const noTokensText = "tokens: no disponibles (se registran con el hook de tokens del agente)"

var callHeader = []string{"#", "hora", "modelo", "entrada", "caché escrita", "releído", "salida", "contexto", "acum. releído", "acum. nuevo"}

// readCalls lee calls.jsonl de la tarea; false si el archivo no existe.
func readCalls(e *engine.Engine, id string) ([]metrics.Run, bool) {
	b, err := e.Store.ReadFile(id, metrics.CallsFile)
	if err != nil {
		return nil, false
	}
	return metrics.Runs(metrics.ParseCalls(strings.NewReader(string(b)))), true
}

// callsNote avisa cuando el detalle no cubre todo el gasto registrado.
func callsNote(st metrics.TaskStats, runs []metrics.Run) string {
	if !st.TokensAvailable {
		return ""
	}
	if len(runs) == 0 {
		return "sin detalle por llamada (registrado antes de GH-29)"
	}
	var sum metrics.Usage
	for _, r := range runs {
		sum.Add(r.Total)
	}
	if st.Tokens.New() > sum.New() || st.Tokens.CacheRead > sum.CacheRead {
		return "parte del gasto se registró antes de GH-29"
	}
	return ""
}

// callCells son las celdas de una fila, ya formateadas (CLI y panel).
func callCells(r metrics.CallRow) []string {
	return []string{fmt.Sprint(r.N), r.TS.Local().Format("15:04:05"), r.Model, metrics.Exact(r.Input), metrics.Exact(r.CacheWrite),
		metrics.Exact(r.CacheRead), metrics.Exact(r.Output), metrics.Exact(r.Context), metrics.Exact(r.AccRead), metrics.Exact(r.AccNew)}
}

// callTotal es la fila total de una corrida; el contexto es el máximo.
func callTotal(r metrics.Run) []string {
	t := r.Total
	return []string{"total", "", "", metrics.Exact(t.Input), metrics.Exact(t.CacheWrite), metrics.Exact(t.CacheRead),
		metrics.Exact(t.Output), metrics.Exact(t.MaxContext), "", ""}
}

// runSummary: "9 llamadas · contexto final 60,079 · releído 344,086 · nuevo 61,204".
func runSummary(r metrics.Run) string {
	return fmt.Sprintf("%d llamadas · contexto final %s · releído %s · nuevo %s", r.Total.Calls,
		metrics.Exact(r.FinalContext), metrics.Exact(r.Total.CacheRead), metrics.Exact(r.Total.New()))
}

func renderCalls(runs []metrics.Run, note string) string {
	var b strings.Builder
	for _, r := range runs {
		fmt.Fprintf(&b, "\n%s · %s · %s\n", r.Label, r.Phase, runSummary(r))
		rows := [][]string{callHeader}
		for _, row := range r.Rows {
			rows = append(rows, callCells(row))
		}
		rows = append(rows, callTotal(r))
		w := make([]int, len(callHeader))
		for _, row := range rows {
			for i, c := range row {
				w[i] = max(w[i], utf8.RuneCountInString(c))
			}
		}
		for _, row := range rows {
			b.WriteString(" ")
			for i, c := range row {
				b.WriteString(" " + strings.Repeat(" ", w[i]-utf8.RuneCountInString(c)) + c)
			}
			b.WriteString("\n")
		}
	}
	if note != "" {
		b.WriteString("\n  " + note + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
