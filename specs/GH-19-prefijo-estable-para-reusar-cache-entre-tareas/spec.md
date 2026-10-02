# GH-19 · Prefijo estable para reusar caché entre tareas

## Brief

**Objetivo:** que `bflow stats <ID>` muestre cuánta caché escribe cada agente y cuánto le cuesta su prefijo, y que una prueba impida que el prefijo de los agentes (contrato + oficio) se vuelva variable por tarea.

Prefijo: la parte fija del prompt de un agente (frontmatter, contrato y oficio del archivo generado por `bflow render`); si no cambia entre llamadas, el modelo la lee de la caché en vez de volver a escribirla.

**Entra**
- Tabla "por agente" de `bflow stats <ID>`: columnas `escrita` (caché escrita total del agente) y `prefijo` (caché escrita en la 1a llamada de cada corrida, promedio por corrida).
- `--json`: `data.stats.agents.<agente>.cache_write` (ya existe) y `data.stats.prefix.<agente>` = `{runs, sum, avg}` (nuevo).
- Prueba que falla si el cuerpo o el archivo generado de cualquier agente del catálogo trae algo variable por tarea (id de tarea, fecha, ruta `.bflow/tasks/<id real>`, ruta del repo) o cambia entre dos repos.
- Guía: el prefijo ya es estable; el reuso entre tareas lo limita la duración de la caché (~5 min), no el orden.

**No entra**
- Resumen por agente entre tareas en `bflow stats` sin ID.
- Caché de 1 hora u otro cambio para forzar reuso entre tareas.
- Panel web / tabla de Tokens (GH-46).
- Cambiar el orden del prompt: el discovery confirmó que ya está bien.

**Decisiones nuevas**
- [N1] `prefijo` es el **promedio** por corrida (redondeado); la suma y el número de corridas van en `--json`. Descartado: la suma, porque crece con las rondas y se confunde con `escrita`.
- [N2] `escrita` sale del log (`tokens`), como `nuevos` y `caché`; solo `prefijo` necesita `calls.jsonl` y muestra "-" sin él. Descartado: sacar ambas de calls.jsonl, que dejaría sin `escrita` a tareas viejas que sí la tienen.
- [N3] El dato va en `TaskStats.Prefix` (llave `prefix`, omitida si no hay calls.jsonl), presente con y sin `--calls`. Descartado: una llave nueva junto a `stats` en `data`, que rompe `TestStatsJSONUnchangedWithoutCalls`.
- [N4] `bflow stats <ID>` sin `--calls` ahora lee `calls.jsonl` de la tarea (un archivo local).

**Riesgos**
- La prueba de variables usa expresiones regulares; un oficio futuro con un ejemplo tipo `API-171` la rompería a propósito (hay que escribir `<id>`).
- La tabla por agente se ensancha dos columnas (de 70 a ~88 caracteres).

**Tamaño:** S — 3 tareas, ~4 archivos de código y 1 de docs.

## Discovery

Dentro de una tarea el prefijo ya se reusa (2a corrida del implementer lee 6.8k de caché). Entre tareas no: la 1a llamada de cada agente escribe 8.1k–9.4k y lee 0, porque la caché dura ~5 min y las tareas se separan por horas; el orden ya es correcto (contrato y oficio fijos en el archivo del agente, args en el prompt). Son ~43k de ~455k por tarea. Hoy `stats` no muestra caché escrita por agente. Alcance: dos columnas en la tabla por agente (y en `--json`) y una prueba que proteja el prefijo, más documentar el hallazgo.

## Requirements

- **R1** [D] CUANDO se corre `bflow stats <ID>` y la tarea tiene tokens por agente, la tabla "por agente" DEBE mostrar una columna `escrita` con la caché escrita total de cada agente (`Usage.CacheWrite`), formateada como las demás (`tok`: "-" si es 0).
- **R2** [D] CUANDO se corre `bflow stats <ID>` y la tarea tiene `calls.jsonl`, la tabla "por agente" DEBE mostrar una columna `prefijo` con la caché escrita en la primera llamada (ya fusionada, por `Msg`) de cada corrida del agente, promediada entre sus corridas [N1].
- **R3** [N] El promedio de R2 DEBE redondear al entero más cercano (mitades hacia arriba): `(sum + runs/2) / runs`.
- **R4** [D] SI la tarea no tiene `calls.jsonl`, o un agente no tiene corridas en él (incluida la fila "sin desglose"), ENTONCES la columna `prefijo` DEBE mostrar "-" para esa fila y `escrita` DEBE seguir saliendo del log [N2].
- **R5** [D] La sesión principal DEBE tratarse como un agente más: su fila "sesión principal" lleva `escrita` y `prefijo`, con la llave `main` (`metrics.MainSession`).
- **R6** [D] CUANDO se corre `bflow stats <ID> --json` (con o sin `--calls`), `data.stats.prefix` DEBE traer por agente `{"runs": n, "sum": s, "avg": a}`; DEBE omitirse si no hay `calls.jsonl` o está vacío [N3]. `data.stats.agents.<agente>.cache_write` sigue igual.
- **R7** [N] La tabla "por modelo" NO DEBE cambiar.
- **R8** [D] El cálculo DEBE salir solo de `calls.jsonl`, sin depender del adaptador (Claude Code u OpenCode).
- **R9** [D] CUANDO se construyen los agentes del catálogo (todos, incluido el scout) con `agents.Build`, ningún `Spec.Body` DEBE contener un id de tarea (`[A-Z][A-Z0-9]*-[0-9]+`), una fecha (`[0-9]{4}-[0-9]{2}-[0-9]{2}`), una ruta `.bflow/tasks/` seguida de algo distinto de `<id>`, ni la ruta raíz del repo.
- **R10** [N] CUANDO se construyen los agentes con dos raíces distintas, los `Spec` (cuerpo, descripción, modelo, esfuerzo, herramientas) DEBEN ser idénticos.
- **R11** [N] CUANDO se renderizan esos agentes con el adaptador de Claude Code y con el de OpenCode, cada archivo DEBE cumplir R9 y salir igual en dos renders seguidos; en Claude Code, el cuerpo DEBE empezar con `## Contrato con bflow` justo después de `GeneratedMark`.
- **R12** [D] La guía (`docs/guia.md`) DEBE explicar las columnas `escrita` y `prefijo` y el hallazgo: el prefijo es estable y el reuso entre tareas lo limita la duración de la caché, no el orden.

## Design

### Métricas (`internal/metrics/calls.go`)

```go
// PrefixCost es la caché escrita en la primera llamada de cada corrida de un agente.
type PrefixCost struct {
	Runs int   `json:"runs"`
	Sum  int64 `json:"sum"`
	Avg  int64 `json:"avg"`
}

// PrefixByAgent agrupa por Run.Agent (MainSession o nombre sin prefijo) la
// CacheWrite de Rows[0] de cada corrida. Corridas sin filas se saltan.
func PrefixByAgent(runs []Run) map[string]PrefixCost
```

- Usa `Runs` ya fusionado: la "1a llamada" es la 1a fila tras fusionar muestras con el mismo `Msg` y ordenar por `TS` (como `--calls`).
- Una corrida cuya 1a llamada escribió 0 cuenta en `Runs` y suma 0 (no se descarta: es reuso real).
- Devuelve `nil` si `runs` está vacío.

### `TaskStats` (`internal/metrics/metrics.go`)

```go
// Prefix es el costo del prefijo por agente, de calls.jsonl (GH-19). Lo llena stats.
Prefix map[string]PrefixCost `json:"prefix,omitempty"`
```

`Compute` no lo llena (solo lee el log); lo llena `runStats`.

### CLI (`internal/cli/metricscmds.go`)

- `runStats` con ID: `runs, _ := readCalls(e, id)` siempre (con y sin `--calls`); `st.Prefix = metrics.PrefixByAgent(runs)` antes de armar el envelope. Con `--calls` reutiliza los mismos `runs`.
- `usageTable(b, title, m, names, prefix map[string]metrics.PrefixCost, withCache bool)`: con `withCache` agrega `escrita` y `prefijo` al final; "por agente" llama con `true` y `st.Prefix`, "por modelo" con `false` y `nil` (R7).
- Fila nueva: `"  %-22s %8s %8s %8s %8s %9s %8s %8s\n"` con encabezados `nuevos, caché, llamadas, ctx máx, ctx final, escrita, prefijo`.
- `prefijo` = `tok(prefix[k].Avg)` si `prefix[k].Runs > 0`; si no, "-". Con `Runs > 0` y `Avg == 0` también muestra "-" (formato `tok`); el 0 exacto queda en `--json`.

Descartado: una tabla aparte "prefijo por agente" (duplica las filas) y mostrar `sum` en texto (ver [N1]).

### Prueba del prefijo

- `internal/agents/agents_test.go`: `allCatalogConfig()` arma un `flow.Config` con todos los agentes de `catalog` (spec: spec-author, ui-designer; contract/implementing: implementer; quality: reviewer, security-auditor, ux-auditor; documenting: documenter; `Scout: "scout"`). Las expresiones de R9 viven en una variable del paquete de prueba `variableInPrefix []*regexp.Regexp`; el mensaje de falla nombra agente, patrón y la línea.
- `internal/adapters/agent/claude/adapter_render_test.go` y `internal/adapters/agent/opencode/opencode_test.go`: renderizan los mismos Specs y aplican R9 y R11. Para no duplicar las expresiones, cada paquete tiene su copia corta (3 regex + raíz); no se exporta nada de producción solo para la prueba.
- No se toca código de producción de `agents` salvo que la prueba encuentre algo variable (hoy no hay: el contrato usa `<id>` literal y los oficios no traen ids ni fechas).

### Seguridad

Sin superficie nueva: solo lectura de `.bflow/tasks/<id>/calls.jsonl` del propio repo, con el `id` ya normalizado por `runStats` y la misma `Store.ReadFile` que usa `--calls`. Líneas malas se saltan (`ParseCalls`). No hay datos sensibles: solo conteos de tokens.

### Docs

`docs/guia.md`, párrafo de `bflow stats <ID>` (línea ~97) y lista de "Costo" (~124): qué son `escrita` y `prefijo`, por qué el prefijo de la 1a corrida de cada agente cuesta ~8–9k en cada tarea (caché de ~5 min) y que el prefijo ya es estable (lo protege la prueba).

## Tasks

- [x] **T1 — Contrato** (R1–R11): firmas `metrics.PrefixCost`, `metrics.PrefixByAgent` (stub que devuelve `nil`), campo `TaskStats.Prefix`; pruebas nuevas que fallan hasta T2:
  - `internal/metrics/calls_test.go`: `TestPrefixByAgent` (dos corridas del implementer 8000 y 1701 → runs 2, sum 9701, avg 4851; reviewer 1 corrida; `main`; 1a llamada con muestras fusionadas por `Msg`; vacío → nil) (R2, R3, R5).
  - `internal/cli/calls_test.go`: `TestStatsAgentCacheColumns` (texto: encabezados `escrita` y `prefijo` solo en "por agente"; valores de una fila; "-" en "sin desglose") (R1, R2, R4, R5, R7); `TestStatsPrefixJSON` (`data.stats.prefix` con y sin `--calls`, omitido sin calls.jsonl) (R6, R8); `TestStatsPrefixWithoutCallsFile` (`escrita` del log, `prefijo` "-") (R4).
  - `internal/agents/agents_test.go`: `TestAgentBodiesHaveNoTaskData` (R9) y `TestAgentSpecsSameAcrossRoots` (R10) — pasan ya; protegen el prefijo.
  - `internal/adapters/agent/claude/adapter_render_test.go`: `TestRenderedAgentsStablePrefix`; `internal/adapters/agent/opencode/opencode_test.go`: `TestOpenCodeRenderedAgentsStablePrefix` (R11) — pasan ya.
- [x] **T2 — Cálculo y salida** (R1–R8): implementar `PrefixByAgent`; `runStats` lee `calls.jsonl` siempre y llena `st.Prefix`; `usageTable` con columnas `escrita` y `prefijo` solo para "por agente". `TestStatsJSONUnchangedWithoutCalls` sigue en verde.
- [x] **T3 — Guía** (R12): actualizar `docs/guia.md` con las columnas nuevas y el hallazgo del prefijo estable / duración de la caché.
