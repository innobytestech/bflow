# GH-18 · Modelo por fase con defaults medidos

## Brief

**Objetivo**: fijar el modelo de cada agente según su fase (haiku en scout y documenter, sonnet en implementer, opus en spec-author y reviewer) y que bflow diga, con datos de sus propias tareas, si un cambio de modelo empeoró rechazos, rondas o hotfixes.

**Entra**
- Defaults: spec-author y reviewer pasan a `opus` (spec-author deja de heredar el de la sesión); scout, documenter e implementer no cambian.
- `opus` en el ejemplo de `models.opencode` del README de OpenCode y de `docs/configuracion.md`, con cómo mapearlo en la config global; agentes regenerados con `bflow render`. El `bflow.yaml` de este repo no gana `models.opencode`: los agentes de OpenCode siguen heredando.
- `bflow start` guarda en el evento `start` los modelos por agente de la config (`data.models`).
- Tareas viejas: el modelo por agente se infiere de los eventos `tokens` del log (traen agente y modelo).
- `bflow stats` (sin ID) compara la configuración vigente con la anterior: 3 tareas cerradas por lado, promedio de rechazos por gate, rondas y hotfixes. `bflow doctor` muestra el mismo veredicto.
- Si sube: aviso en rojo (sin emojis) con los agentes que cambiaron y el modelo al que volver. No revierte nada.

**No entra**
- Revertir solo; cambiar effort; ui-designer, security-auditor y ux-auditor (siguen como están).
- Arreglar al reviewer superficial (GH-20/GH-50) o los emojis que ya usa `doctor`.

**Decisiones nuevas**
- [N] Los modelos se normalizan a familia (`opus`, `sonnet`, `haiku`) si el nombre la contiene: `claude-sonnet-4-5-...` del log y el alias `sonnet` de la config son lo mismo. Descartado: comparar el ID exacto (cada versión partiría los grupos).
- [N] La inferencia usa los eventos `tokens` de `log.jsonl`, no `calls.jsonl`: traen el mismo agente y modelo y `stats` ya los lee. Descartado: abrir el `calls.jsonl` de cada tarea.
- [N] Una tarea es "de" una configuración si coincide en todos los agentes que ambas tienen (al menos uno): una tarea vieja sin scout no rompe el grupo. Descartado: igualdad exacta del mapa (casi ninguna tarea vieja calificaría).
- [N] Cuentan las tareas en `done` de carril full o light; quedan fuera las de hotfix (ya suman como hotfix de su feature), las `dropped` y las cerradas fuera de esta copia. Descartado: mezclar carriles.
- [N] "Sube" en rechazos se mide gate por gate: basta que suba uno. Descartado: solo el total (un gate que empeora quedaría tapado por otro que mejora).
- [N] En `doctor` la regresión es `warn`, no `fail`: avisa sin cambiar el código de salida. Descartado: `fail` (rompería a quien corre doctor en scripts).
- [N] El ítem de `doctor` se llama `veredicto-modelos`, no `modelos`: ese ya lo usan los avisos de alias y región de OpenCode. Descartado: compartir `modelos` (mezcla dos avisos y rompe las pruebas que cuentan uno).

**Riesgos**
- Los hotfixes llegan después de cerrar: el lado "después" puede verse mejor de lo que es hasta que pase tiempo.
- 3 tareas por lado es poca muestra; el veredicto es un aviso, no una prueba.
- opus cuesta más: spec y review gastan más, y se ve en `stats` por modelo.
- Sin `models.opencode` en el repo, en OpenCode spec-author y reviewer heredan el modelo de la sesión hasta que cada quien mapee `opus` en su config global; `doctor` ya lo avisa en `modelos` (alias sin equivalente). Se descartó versionar `anthropic/claude-*-4`: IDs no verificados y riesgo de región (#98-#100).

**Tamaño**: M. 5 tareas en `agents`, `engine`, `metrics`, `cli` y docs.

## Discovery

# Discovery GH-18: modelo por fase con defaults medidos

## Alcance
- Defaults de modelo por agente: scout y documenter en haiku (barato); implementer en sonnet (intermedio); spec-author y reviewer en opus fijo (el más capaz). spec-author deja de heredar el modelo de la sesión.
- opus se añade a models.opencode en bflow.yaml y al README del adaptador OpenCode; bflow render --check debe seguir pasando en CI.
- bflow mide si un cambio de modelo se queda: cada tarea guarda, al arrancar, los modelos por agente que tenía la configuración.
- bflow stats agrupa por configuración de modelos los rechazos por gate, las rondas y los hotfixes, y compara la configuración vigente con la anterior.

## Criterio
- Ventana: 3 tareas cerradas después del cambio contra las 3 anteriores con la configuración previa. "Sube" quiere decir que el promedio queda por encima del de la configuración anterior en rechazos por gate, en rondas o en hotfixes.
- Con menos de 3 tareas en alguno de los dos lados no hay veredicto, y se dice así ("sin veredicto: N/3").

## Si sube
- Solo avisa, no revierte. bflow stats y bflow doctor lo marcan en rojo (texto en color, sin emojis) y muestran la configuración anterior para volver. El humano decide si revierte bflow.yaml y corre render.

## Datos históricos
- Las tareas cerradas no guardaron sus modelos. Si tienen detalle por llamada (metrics, --calls), el modelo por agente se infiere de ahí. Las que no lo tienen quedan fuera de la comparación.

## Restricciones
- La salida para agentes es mínima y en JSON compacto.
- La UI para personas usa texto en color, nunca emojis.
- Riesgo conocido (GH-20/GH-50): un reviewer superficial puede ocultar regresiones en el criterio de rechazos. No se resuelve aquí.

## Requirements

- R1 [D] El catálogo de agentes DEBE traer `opus` como modelo por defecto de spec-author y reviewer, `sonnet` de implementer y `haiku` de scout y documenter.
- R2 [D] CUANDO `bflow.yaml` define `agents.<nombre>.model`, el sistema DEBE seguir usándolo sobre el default.
- R3 [D] El `bflow.yaml` de este repo NO DEBE ganar `models.opencode` (los agentes de `.opencode/agents/` siguen sin `model:`), y `bflow render --check` DEBE pasar con los agentes regenerados.
- R4 [D] El README de OpenCode y `docs/configuracion.md` DEBEN mostrar `opus` en el ejemplo de `models.opencode`, listar los defaults por agente y explicar cómo mapear `opus` (y `sonnet`, `haiku`) en el `models:` del nivel superior de la config global.
- R5 [D] CUANDO una tarea arranca (`bflow start`, `bflow new`), el sistema DEBE guardar en `data.models` del evento `start` el modelo configurado de cada agente del flujo (incluido el scout), `""` si el agente no fija modelo.
- R6 [D] CUANDO una tarea no tiene `data.models`, el sistema DEBE inferir el modelo de cada agente de sus eventos `tokens` (sin contar la sesión principal ni el agente vacío); si un agente usó varios, el de más llamadas, y en empate el último.
- R7 [N] El sistema DEBE normalizar cada modelo: minúsculas, sin el prefijo `proveedor/`, y la familia `opus`, `sonnet` o `haiku` si el nombre la contiene; `""` se muestra como `heredado`.
- R8 [D] CUANDO una tarea no tiene `data.models` ni eventos `tokens` con agente, el sistema DEBE dejarla fuera de la comparación.
- R9 [N] El sistema DEBE contar en la comparación solo tareas en `done`, de carril full o light y sin `closed_outside`.
- R10 [N] La configuración vigente DEBE ser la `data.models` del `start` más reciente. El grupo "después" son las tareas, de la más reciente hacia atrás, que coinciden con ella, hasta la primera que no coincide; esa da la configuración anterior. El grupo "antes" son las que coinciden con la anterior desde ahí hacia atrás, hasta la primera que tampoco coincide. Las tareas sin datos (R8) ni cortan ni cuentan.
- R11 [N] Dos configuraciones coinciden si comparten al menos un agente y en todos los que comparten el modelo normalizado es igual.
- R12 [D] El sistema DEBE tomar las 3 tareas más recientes de cada grupo y comparar el promedio de rechazos de cada gate, el de rondas y el de hotfixes.
- R13 [D] CUANDO algún grupo tiene menos de 3 tareas, el sistema DEBE decir "sin veredicto" con la cuenta de cada lado (`después N/3 · antes M/3`) y no calcular si sube.
- R14 [D] CUANDO un promedio del grupo "después" queda por encima del de "antes" (cualquier gate, rondas u hotfixes), el veredicto DEBE ser "sube" y nombrar qué subió con ambas cifras.
- R15 [D] CUANDO el veredicto es "sube", el texto DEBE ir en rojo si la salida es una terminal con color (sin color con `NO_COLOR` o fuera de terminal), sin emojis, y mostrar por cada agente que cambió el modelo anterior al que volver, más "cambia bflow.yaml (agents) y corre bflow render".
- R16 [D] El sistema NUNCA DEBE revertir `bflow.yaml` ni los agentes por su cuenta.
- R17 [D] `bflow stats` sin ID DEBE agregar el veredicto en `data.models` (JSON compacto) y una línea `modelos:` al texto; `bflow stats <ID>` DEBE mostrar la configuración de modelos de la tarea y su origen (`start` o `tokens`).
- R18 [D] `bflow doctor` DEBE agregar el ítem `veredicto-modelos` (aparte de `modelos`, que ya usa OpenCode para los alias): `ok` sin regresión o sin veredicto, `warn` si sube, sin que esto cambie el código de salida.

## Design

### Defaults (`internal/agents/agents.go`)
- `catalog["spec-author"].model = "opus"`, `catalog["reviewer"].model = "opus"`. El resto igual.
- Función pública nueva, sin leer archivos:
  ```go
  // Models da el modelo configurado de cada agente del flujo (con el scout): el de
  // agents.<nombre>.model o el del catálogo; "" si ninguno lo fija.
  func Models(fc flow.Config, conf map[string]config.AgentConf) map[string]string
  ```
  Usa `fc.AgentNames()`, `fc.Scout` y `pick(c.Model, b.model)`, igual que `Build`.

### Registro al arrancar (`internal/engine/engine.go`)
- En `entry`, rama `ev.Kind == flow.EvStart`: `en.Data["models"] = agents.Models(e.flowCfg(), e.Cfg.Agents)`. `agents` no importa adaptadores, así que archtest sigue limpio. Con `e.Cfg == nil` no se agrega.

### Comparación (`internal/metrics/models.go`, nuevo)
```go
type ModelSet map[string]string // agente → modelo normalizado

func NormalizeModel(m string) string
func TaskModels(id string, entries []store.Entry) (ModelSet, string) // origen: "start" | "tokens" | ""
func (a ModelSet) Matches(b ModelSet) bool                            // R11
func (a ModelSet) String() string                                     // "documenter=haiku,reviewer=opus,..." ordenado

type ModelGroup struct {
	Models     ModelSet           `json:"models"`
	Tasks      []string           `json:"tasks"`      // IDs, más reciente primero, máximo 3
	Rejections map[string]float64 `json:"rejections"` // gate → promedio
	Rounds     float64            `json:"rounds"`
	Hotfixes   float64            `json:"hotfixes"`
}
type ModelChange struct {
	Agent string `json:"agent"`
	From  string `json:"from"` // anterior: al que volver
	To    string `json:"to"`   // vigente
}
type ModelVerdict struct {
	Verdict  string        `json:"verdict"` // "sube" | "sin_regresion" | "sin_veredicto" | "sin_datos"
	Current  ModelGroup    `json:"current"`
	Previous *ModelGroup   `json:"previous,omitempty"`
	Changes  []ModelChange `json:"changes,omitempty"`
	Rose     []string      `json:"rose,omitempty"` // "rondas 1.7 > 1.0", "rechazos spec 0.7 > 0.3"
}

const ModelWindow = 3

func CompareModels(entries []store.Entry, now time.Time) ModelVerdict
```
- Orden de las tareas: TS del evento `start` (el último si hay varios); sin `start`, la primera entrada de la tarea. Estado y cifras salen de `Compute(id, entries, now)` (Phase, ClosedOutside, RejectionsByGate, Rounds, Hotfixes); el carril, de `data.lane` del `start`.
- `sin_datos`: ningún `start` trae `data.models` (no hay vigente). Sin configuración anterior (`Previous == nil`) el veredicto es `sin_veredicto`.
- Un gate que solo aparece en un grupo cuenta como 0 en el otro.
- `TaskStats` gana `ModelSet ModelSet` (`json:"model_set,omitempty"`) y `ModelSource string` (`json:"model_source,omitempty"`). Los llena `runStats`, no `Compute`, para no cambiar su firma.

### Salida (`internal/cli`)
- `metricscmds.go`: sin ID, `data["models"] = metrics.CompareModels(log, now)` y una línea al final del texto, `modelos: <veredicto> · después N/3 · antes M/3 · ...`, con `renderModelVerdict(v metrics.ModelVerdict, color bool) string`. El color sale de `bannerFor(c) == bannerColor`; el rojo (`\x1b[31m`...`\x1b[0m`) va solo en la parte "sube" y en las líneas de vuelta (`  reviewer: opus → volver a sonnet`). Con ID: línea `  modelos (start|tokens): <ModelSet.String()>`.
- `setupcmds.go` (`runDoctor`): ítem `veredicto-modelos` con `CompareModels` (no `modelos`: ese área ya la usan los avisos de alias y región de OpenCode y sus pruebas cuentan un solo ítem); `warn` si `sube`, `ok` en los demás casos (con la cuenta o "sin datos de modelos todavía"). Si falla la lectura del log, `warn`, no `fail`.

### Config y docs
- `bflow.yaml` de este repo: sin cambios (no gana `models.opencode`). `bflow render` cambia solo `.claude/agents/bflow-{spec-author,reviewer}.md` (`model: opus`); los de `.opencode/agents/` siguen sin `model:` porque los alias no tienen equivalente en el repo.
- `adapters/opencode/README.md`, `docs/configuracion.md` (y el ejemplo de `docs/guia.md` si lista alias): `opus` en la tabla de ejemplo, defaults por agente, un ejemplo de `models: { opencode: { opus: <proveedor/modelo>, ... } }` en el nivel superior de la config global (`config.GlobalPath()`) con la nota de que este repo no lo versiona, y un párrafo sobre el veredicto de modelos en `stats` y `doctor` (`veredicto-modelos`).
- `internal/cli/opencode_test.go`: en `TestRenderMultiTool` la tabla `{sonnet, haiku}` ya no cubre spec-author (ahora `opus`), así que la afirmación de que no lleva `model:` sigue valiendo pero por alias sin equivalente: se reescribe su mensaje. El caso `full` de la prueba de doctor ("con la tabla completa no hay aviso de modelos") gana `opus: a/o` en su tabla. El caso de alias sin resolver suma `opus`, `bflow-spec-author` y `bflow-reviewer` a lo que debe nombrar el detalle.

### Descartado
- Guardar los modelos en `state.json` (`Record`): el log ya es la fuente de `stats` y deja el dato junto al `start`.
- Revertir solo o bloquear `start` si sube: el discovery pide solo avisar.

### Seguridad
- Sin authz nueva: lee y escribe `.bflow/` local como hoy. Los nombres de modelo vienen de la config o del log propio y se imprimen tal cual en texto y JSON; no se ejecuta nada con ellos. Sin datos sensibles: solo alias o `proveedor/modelo`, nunca claves. `Store.Log` ya salta las líneas malas; un `data.models` que no es un mapa de strings se ignora y la tarea cae a la inferencia (R6).

## Tasks

- [ ] T1 Contrato: firmas de `agents.Models`, `metrics.NormalizeModel`, `TaskModels`, `ModelSet.Matches`/`String`, `ModelGroup`, `ModelChange`, `ModelVerdict`, `ModelWindow`, `CompareModels` y los campos `TaskStats.ModelSet`/`ModelSource`, con pruebas en rojo: `TestCatalogDefaultModels`, `TestModelsRespectsRepoOverride` (agents); `TestStartRecordsModels` (engine); `TestNormalizeModel`, `TestTaskModelsFromStart`, `TestTaskModelsInferredFromTokens`, `TestTaskModelsWithoutData`, `TestModelSetMatches`, `TestCompareModelsNoVerdictUnderWindow`, `TestCompareModelsRises`, `TestCompareModelsNoRegression`, `TestCompareModelsSkipsHotfixDroppedAndOutside`, `TestCompareModelsNoData` (metrics); `TestStatsModelVerdict`, `TestStatsModelVerdictColor`, `TestDoctorModelsWarnKeepsExit` (cli). (R1-R18)
- [ ] T2 Defaults y registro: `opus` en spec-author y reviewer, `agents.Models` y `data.models` en el evento `start`; ajustar `opencode_test.go` (mensaje de spec-author sin `model:`, `opus` en la tabla `full` y en el detalle de alias sin resolver). (R1, R2, R5)
- [ ] T3 Comparación en `internal/metrics/models.go`: normalización, inferencia desde `tokens`, coincidencia, grupos y veredicto. (R6-R14, R16)
- [ ] T4 Salida: línea y `data.models` en `stats`, modelos de la tarea en `stats <ID>`, rojo solo con color e ítem `veredicto-modelos` en `doctor`. (R15, R17, R18)
- [ ] T5 Config y docs: `bflow render` (solo cambian los dos agentes de Claude; `bflow.yaml` sin `models.opencode`), README de OpenCode y `docs/configuracion.md` (y `docs/guia.md` si aplica) con `opus`, los defaults y cómo mapear en la config global; `bflow render --check` en verde. (R3, R4)
