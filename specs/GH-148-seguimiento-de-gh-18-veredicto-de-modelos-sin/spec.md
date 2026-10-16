# GH-148 · Seguimiento de GH-18: veredicto de modelos sin datos, modelo heredado y pruebas faltantes

## Brief

**Objetivo**: que el veredicto de modelos de GH-18 compare los modelos que de verdad corrieron, que `stats` diga cuando aún no hay datos y que las dos ramas sin prueba la tengan.

**Entra**
- `bflow stats` sin ID imprime `modelos: sin datos de modelos todavía` cuando el veredicto es `sin_datos` y hay al menos una tarea.
- Por agente, el modelo de una tarea sale de sus eventos `tokens` (traen `data.model`, lo que respondió el proveedor). `data.models` del start queda como respaldo para los agentes sin eventos `tokens`.
- La configuración vigente del veredicto pasa a ser el conjunto efectivo (start + tokens) de la tarea del start más reciente, no solo la config del start.
- Pruebas: el warn `veredicto-modelos` de `doctor` con el log ilegible, y el start con `e.Cfg` nil (no escribe `data.models`).

**No entra**
- Cambiar qué escribe el start en `data.models`. Sigue siendo la config, y vacío sigue significando "heredado" (consumer-changelog de GH-18).
- Versionar `models.opencode` en el repo ni tocar el render de OpenCode.
- Cambiar la ventana (3), los umbrales ni los estados del veredicto.

**Decisiones nuevas**
- [N1] Se prefiere la inferencia por tokens y no se marca "heredado" en el start. Descartado: marcar "heredado" en el start cuando el alias no tiene equivalente en el adaptador. El start no sabe qué herramienta va a correr la tarea (este repo tiene `agent: [claude, opencode]`) y en Claude el alias sí resuelve. Además cambiaría la semántica de `data.models`, que ya tiene consumidores.
- [N2] El overlay es por agente: el modelo que más veces aparece en los `tokens` del agente (en empate, el último), como ya hace `TaskModels`. Descartado: usar tokens solo si la tarea no tiene start. Es lo que hay hoy y deja pasar el error.
- [N3] `model_source` gana el valor `start+tokens` cuando se combinan los dos orígenes. Es un valor aditivo. Descartado: reportar `tokens` cuando hubo mezcla, porque ocultaría que parte del conjunto viene de la config.
- [N4] Con historial vacío (sin tareas) `stats` sigue diciendo `sin historial todavía`, sin la línea de modelos. Descartado: imprimir siempre la línea.

**Riesgos**
- Las tareas viejas con tokens de OpenCode dejan de coincidir con la config del start. Pueden partir el grupo "después" y bajar el veredicto a `sin_veredicto`. Es lo correcto (antes comparaba configuraciones falsas), pero cambia lo que ve el usuario.
- Usar Claude y OpenCode a la vez fragmenta los grupos. Se acepta: refleja lo que corrió.
- Hay que corregir el consumer-changelog: la línea `modelos:` ahora aparece sin datos y existe `start+tokens`.

**Tamaño**: S. Toca `internal/metrics/models.go` y `internal/cli/metricscmds.go`, más pruebas en metrics, cli y engine.

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Carril light: los criterios van dentro de Tasks. -->

## Design

<!-- Carril light: sin sección de diseño; las firmas exactas están en Tasks. -->

## Tasks

- [x] **T1 · Contrato (pruebas que fallan)**. Cubre R1-R5.
  - `internal/metrics/models_test.go`: `TestTaskModelsTokensOverrideStart` (R2, R3), `TestTaskModelsStartFallbackPerAgent` (R2, R3), `TestCompareModelsCurrentUsesEffectiveSet` (R4).
  - `internal/cli/models_test.go`: `TestStatsModelVerdictNoData` (R1), `TestStatsEmptyHistoryNoModelLine` (R1), `TestDoctorModelVerdictLogUnreadable` (R5).
- [x] **T2 · Línea sin datos en `stats`** (`internal/cli/metricscmds.go`, `statsCmd`). Cubre R1.
  - R1 [N]: CUANDO se corre `bflow stats` sin ID con al menos una tarea y el veredicto es `sin_datos`, el sistema DEBE imprimir `modelos: sin datos de modelos todavía` con `renderModelVerdict`. CUANDO no hay tareas ni uso sin asignar, DEBE seguir imprimiendo solo `sin historial todavía`. `data.models` en JSON no cambia.
- [x] **T3 · Modelos efectivos por agente** (`internal/metrics/models.go`). Cubre R2-R4.
  - R2 [N]: CUANDO una tarea tiene `data.models` en su start y eventos `tokens` con `data.model` no vacío de un agente (sin contar la sesión principal), `TaskModels` DEBE usar para ese agente el modelo inferido de tokens (normalizado con `NormalizeModel`; el más frecuente y, en empate, el último). Para los demás agentes DEBE quedarse con el valor del start.
  - R3 [N]: `TaskModels` DEBE devolver como origen `start` si ningún agente vino de tokens, `tokens` si la tarea no tiene start con modelos, y `start+tokens` si se combinaron. Sin ninguno de los dos devuelve `(nil, "")`, como hoy.
  - R4 [N]: CUANDO `CompareModels` fija la configuración vigente, DEBE usar `TaskModels` de la tarea del start más reciente con `data.models`, no el `data.models` crudo. Si no hay ningún start con modelos, el veredicto sigue siendo `sin_datos`.
  - `stats <ID>` muestra el nuevo origen sin cambios de código: la línea es `modelos (start+tokens): ...`.
- [x] **T4 · Pruebas de ramas sin cubrir**. Cubre R5.
  - R5 [D]: CUANDO `log.jsonl` tiene una línea que no es JSON, `bflow doctor` DEBE reportar el ítem `veredicto-modelos` en `warn` con un mensaje que empiece por `no se pudo leer el log`, y su código de salida DEBE ser el mismo que sin ese ítem.
- [x] **T5 · Changelog de consumidores**. Cubre R1 y R3.
  - Agregar la entrada de GH-148: la línea `modelos:` ahora aparece en `stats` con `sin_datos` cuando hay tareas, existe el valor `start+tokens` en `data.stats.model_source`, y el veredicto usa los modelos que corrieron (tokens) por encima de la config del start.
