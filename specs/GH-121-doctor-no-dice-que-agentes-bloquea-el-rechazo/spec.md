# GH-121 · doctor no dice qué agentes bloquea el rechazo de región del proveedor ni cómo cambiarlos de modelo

## Brief

**Objetivo:** cuando el log de OpenCode muestra el rechazo `requires Global regions`, `bflow doctor` dice qué agentes de bflow y qué modelos quedan bloqueados, deja `modelos` en warn y señala cómo cambiarlos de modelo sin activar Global.

**Entra**
- El adaptador de OpenCode calcula el modelo efectivo de cada `bflow-*`: el `model:` del frontmatter en `.opencode/agents/` (equipo) y, si no lo trae, `agent.<nombre>.model` de `opencode.json` (personal).
- El warn de `proveedor` por región lista los agentes afectados agrupados por modelo, con su origen y la salida que corresponde a cada uno: activar Global, o cambiar `agent.<nombre>.model` en `opencode.json` (solo tú) o `models.opencode` en `bflow.yaml` y correr `bflow render` (equipo).
- `modelos` deja de dar `ok` si algún agente quedó marcado.
- La línea de la skill `/bflow` sobre un subagente que falla nombra también el mapeo personal y `bflow doctor`. Se actualizan el README del adaptador y `docs/guia.md`.

**No entra**
- La sonda real, es decir, llamar al modelo para probarlo. `doctor` no gasta tokens ni sale a la red, y la sonda pide credenciales del proveedor. Si hace falta, va en otra tarea.
- Cambiar el modelo por el usuario: doctor solo diagnostica.
- La máquina de estados y el flujo de relanzar, que ya se comportan bien.

**Decisiones nuevas**
- [N] Qué modelo cuenta como rechazado. Si la línea del rechazo nombra un modelo (`modelID`/`model` en logfmt, o el id dentro del texto), se marca solo ese modelo. Si no nombra ninguno, se marcan los agentes con proveedor `opencode-go/` como «probable», porque el texto dice «This Go model». Descartada: marcar todos los agentes. Eso marcaría también los de otros proveedores, que no tienen nada que ver.
- [N] Un rechazo anterior al último cambio de modelo no cuenta. Se ignora el rechazo si es más viejo que el mtime más reciente de `opencode.json` y de `.opencode/agents/bflow-*.md`. Es el mismo criterio que ya usan los fallos de carga del plugin. Así, quien cambia a `glm-5.2` no sigue viendo el warn durante 7 días. Descartada: solo la ventana de 7 días, que contradice el arreglo que el mismo warn recomienda.
- [N] El warn sigue saliendo una sola vez por tipo (R6 de GH-99) y agrupa los agentes por modelo para no repetir el modelo seis veces.
- [N] `doctorOpenCode` llama a `DoctorLog` antes del bloque de `modelos` y deja los hallazgos donde aparecen hoy. Descartada: una interfaz nueva solo para el rechazo, que duplicaría la lectura del log.

**Riesgos**
- No sabemos si OpenCode escribe el modelo en la línea del error. Sin ese dato, el caso «probable» marca de más a un agente de `opencode-go` que sí funcione. El mensaje lo dice con la palabra «probable».
- Activar Global no toca ningún archivo, así que el warn sigue hasta que vence la ventana de 7 días. Para eso el mensaje lleva la fecha del último rechazo.

**Tamaño:** S-M. Toca `logscan.go`, `opencode.go` (una función de lectura), `setupcmds.go`, la skill, dos documentos y sus pruebas.

## Discovery

Carril light: no hubo discovery. El contexto viene de la tarea y del scout: `DoctorLog` en `logscan.go` (bloque `global`), `doctorOpenCode` en `setupcmds.go` y el mapeo personal de GH-96.

## Requirements

Carril light: los criterios van en Tasks.

## Design

- En `internal/adapters/agent/opencode/opencode.go` (o en `models.go`):
  - `type AgentModel struct{ Agent, Model, Source string }`. `Source` vale `"repo"` (frontmatter) o `"personal"` (`opencode.json`).
  - `func agentModels(root, home string) []AgentModel`: ordenado por `Agent`. Usa `loadUser` para leer `opencode.json`. Un agente sin modelo no entra en la lista porque hereda el de la sesión.
- En `logscan.go`, el bloque `global` cambia así:
  - Por cada línea con `requires Global regions` dentro de la ventana, junta los ids de modelo que nombra: campos `modelID`, `model` y `providerID/modelID`, o un id de `agentModels` que aparezca en el texto.
  - La línea solo cuenta si su `ts` es posterior a `modelsMtime(root, home)`, el mtime más reciente de `opencode.json` y de `.opencode/agents/bflow-*.md`.
  - Marcados: los agentes cuyo modelo está entre los ids nombrados. Si ninguna línea nombra un modelo, los de prefijo `opencode-go/`, como probables.
- Hallazgos que emite `DoctorLog` con rechazo vigente:
  - `proveedor` warn: `el proveedor rechazó modelos por región (último: <AAAA-MM-DD HH:MM UTC>): bflow-scout, bflow-documenter → opencode-go/deepseek-v4.1-flash (personal); … ; activa Global en Privacy de tu workspace de OpenCode o cambia de modelo: agent.<nombre>.model en <ruta opencode.json> (solo tú) o models.opencode en bflow.yaml y bflow render (equipo)`.
    - Con marcado «probable», se agrega ` (probable: el log no nombra el modelo)`.
    - Solo se nombran las salidas que corresponden al `Source` de los agentes marcados.
  - `modelos` warn: `opencode: <n> agentes con un modelo rechazado por región; ver proveedor`.
  - Con rechazo vigente pero sin agentes marcados, sale el warn de hoy (Global en Privacy) más la salida de cambiar el modelo, sin `modelos`.
- En `doctorOpenCode`: llama `ld.DoctorLog` una sola vez antes del bloque de `planRender`. Si hay un hallazgo `modelos` warn, no emite `modelos ok`. Los hallazgos `modelos` salen junto a los de modelos y el resto queda donde está hoy.
- Skill `adapters/leader/bflow.md` l. 15: «modelo no disponible o rechazado por región (`bflow doctor` dice qué agentes; cambia `models.opencode` en `bflow.yaml` y `bflow render`, o `agent.<subagente>.model` en la config de OpenCode del usuario)». No cambia nada más.
- Seguridad: lectura local, sin red. En los mensajes solo van ids de modelo, nombres de agente y rutas; nunca el texto de un prompt (lo cubre `TestDoctorLogNoFiltraPrompt`). Cada campo se recorta con `clip`. Si `opencode.json` no existe o no se puede leer, la lista personal queda vacía, sin error.

## Tasks

- [ ] **T1 Contrato.** Firmas de `AgentModel`, `agentModels` y `modelsMtime`. Pruebas que fallan:
  - `TestAgentModels` (opencode): frontmatter gana, el personal cubre el resto, el agente sin modelo queda fuera.
  - `TestDoctorLogRegionAgentes`: el fixture tiene una línea con `modelID=deepseek-v4-pro`. Marca solo esos agentes y el mensaje trae agente → modelo, el origen y las dos salidas.
  - `TestDoctorLogRegionProbable`: con el log sin modelo, marca los `opencode-go/` con «probable» y deja fuera un agente `anthropic/…`.
  - `TestDoctorLogRegionTrasCambio`: con `opencode.json` tocado después del rechazo, no sale el warn de región ni `modelos`.
  - `TestDoctorOpenCodeModelosRegion` (cli): con rechazo vigente no aparece `modelos ok` y sí `modelos` warn.
  - Criterios:
    - C1: CUANDO hay un rechazo de región vigente, `DoctorLog` DEBE listar cada agente afectado con su modelo y su origen, agrupados por modelo, en un solo warn de `proveedor`.
    - C2: CUANDO el rechazo nombra un modelo, solo se marcan los agentes con ese modelo. CUANDO no nombra ninguno, se marcan los `opencode-go/` como probables.
    - C3: CUANDO algún agente queda marcado, `doctor` NO DEBE dar `modelos ok` y DEBE dar `modelos` warn.
    - C4: CUANDO el rechazo es anterior al último cambio de `opencode.json` o de `.opencode/agents/bflow-*.md`, no cuenta.
    - C5: El warn DEBE nombrar la salida sin Global que toca: `agent.<nombre>.model` en la ruta real de `opencode.json` o `models.opencode` + `bflow render`.
- [ ] **T2 Modelo efectivo por agente.** Implementar `agentModels` y `modelsMtime`; `TestAgentModels` pasa. (C1, C4)
- [ ] **T3 Rechazo por agente en `DoctorLog`.** Rehacer el bloque `global`. Agregar al `testdata/` un fixture con `modelID` y ajustar `TestDoctorLogProveedor` al texto nuevo, que sigue conteniendo `Global` y `Privacy`. Las tres `TestDoctorLogRegion*` pasan y `TestDoctorLogNoFiltraPrompt` sigue en verde. (C1, C2, C4, C5)
- [ ] **T4 `modelos` en `doctorOpenCode`.** Llamar a `DoctorLog` antes y suprimir `modelos ok` cuando corresponda. `TestDoctorOpenCodeModelosRegion`, `TestDoctorOpenCodeModels`, `TestDoctorOpenCode` y `TestDoctorOpenCodeLog` pasan. (C3)
- [ ] **T5 Skill y documentación.** Cambiar la línea 15 de `adapters/leader/bflow.md`. Si alguna prueba compara la skill renderizada (`SkillState`), actualizar lo que haga falta. En `adapters/opencode/README.md` (l. 42), dar las dos salidas y explicar que doctor lista los agentes. En `docs/guia.md` (l. 107), mencionar `modelos`. `go test ./...` en verde. (C5)
