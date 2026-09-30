# GH-26 · guard: los subagentes no deben poder aprobar compuertas

## Brief

**Objetivo:** que `bflow guard` impida a un subagente correr los comandos que responden una decisión humana (`approve`, `reject`, `unblock`, `start`, `new`) y que el log diga qué agente lo intentó.

**Entra**
- Regla `human_only` para subagentes (cualquier `Action.Subagent`, no solo `bflow-*`) sobre `approve`, `reject`, `unblock`, `start`, `new`. La sesión principal los sigue corriendo. `freeze` sigue negado a todos.
- Reconocer bflow por el nombre base del ejecutable: `bflow`, `bflow.exe`, rutas con `/` o `\`, entre comillas, con prefijos `VAR=x` / `env VAR=x`, y `go run <ruta>/cmd/bflow`. El mismo reconocimiento para `freeze`.
- El evento `guard` del log registra el agente (`agent_type` del hook) y el comando; `bflow watch` muestra el agente en esa línea.
- Pruebas unitarias (guard, adaptador Claude) y e2e con un PreToolUse de `bflow-documenter`.

**No entra**
- Impedir la ejecución fuera del hook (otra herramienta sin guard, o una persona). Guard no es un sandbox.
- Atribuir quién corrió el evento `approve` (el CLI no sabe si lo invocó un subagente).
- Cambiar cómo el adaptador decide qué es subagente.

**Decisiones nuevas**
- [N] El agente viaja en un campo nuevo `guard.Action.Agent` (el `agent_type` tal cual, p. ej. `bflow-documenter`) y se guarda en `Entry.Agent` del evento `guard`. Se descarta `Data.agent`: `Entry.Agent` ya existe y `watch`/`stats` lo leen.
- [N] El global `--json`/`-json` antes del subcomando se salta (`bflow --json approve` es un approve), porque `cli.Run` lo acepta en cualquier posición. Se descarta ignorarlo: sería el atajo evidente.
- [N] El comando se guarda en `Data.command`, cortado a 200 caracteres. Se descarta guardarlo entero: una nota larga llenaría el log.
- [N] `watch` muestra `guard bloqueó: human_only (bflow-documenter)`. Se descarta dejarlo sin agente: el objetivo del issue es poder detectarlo sin abrir el JSONL.

**Riesgos**
- Claude Code manda `agent_type` también a la sesión principal si se lanza con `claude --agent X`. El adaptador lo toma como subagente (ya pasa hoy con `strict_leader`), así que esa sesión no podría aprobar. bflow no lanza sesiones con `--agent`, así que no se cambia aquí.
- `go run` con flags que llevan valor (`-tags x`) puede escapar al reconocimiento. Se acepta: guard frena el atajo evidente.
- Hay que actualizar un caso de `hook_test.go`: la acción esperada ahora trae `Agent`.

**Tamaño:** S-M. Unas 150 líneas entre `guard.go`, `hook.go`, `hookcmds.go` y `watch.go`, más pruebas y docs.

## Discovery

El hook PreToolUse ya distingue subagente (`agent_type`/`agent_id` → `Action.Subagent`, internal/adapters/agent/claude/hook.go:31). Hoy `human_only` solo cubre `bflow freeze` con un regex rígido (internal/guard/guard.go:106). `logGuard` (internal/cli/hookcmds.go:116) registra la regla, pero no el agente. Decisiones: negar a cualquier subagente `approve/reject/unblock/start/new`, reconocer bflow de forma robusta y registrar el agente en el log. El discovery completo está en el tracker.

## Requirements

- R1 [D] CUANDO un subagente (`Action.Subagent=true`) corre por Bash un segmento cuyo subcomando bflow es `approve`, `reject`, `unblock`, `start` o `new`, el guard DEBE negarlo con la regla `human_only` y un motivo que nombre el subcomando, diga que la decisión es de la persona y que el subagente reporte con `bflow report` (NEEDS_DECISION si necesita que alguien decida).
- R2 [D] CUANDO la sesión principal (`Subagent=false`) corre esos mismos subcomandos, el guard DEBE permitirlos.
- R3 [D] CUANDO cualquier sesión (principal o subagente) corre `bflow freeze`, el guard DEBE negarlo con `human_only`, como hoy.
- R4 [D] CUANDO un subagente corre otros subcomandos (`report`, `block`, `status`, `show`, `check`, `pr`, `task add`...), el guard DEBE permitirlos (salvo que otra regla aplique).
- R5 [D] El guard DEBE reconocer el ejecutable de bflow por su nombre base sin distinguir mayúsculas: `bflow`, `bflow.exe`, `./bin/bflow.exe`, rutas absolutas con `/` o `\`, con comillas simples o dobles alrededor del token, precedido de asignaciones `VAR=x` y/o `env [flags] VAR=x`.
- R6 [D] El guard DEBE reconocer `go run [flags] <ruta>` como bflow cuando `<ruta>`, normalizada a `/` y sin `/` final, es `cmd/bflow` o termina en `/cmd/bflow`.
- R7 [N] CUANDO antes del subcomando aparecen `--json` o `-json`, el guard DEBE saltarlos para identificar el subcomando.
- R8 [D] CUANDO el comando es compuesto (`&&`, `||`, `;`, `|`), el guard DEBE evaluar cada segmento. Basta que uno rompa la regla para negarlo todo.
- R9 [D] La regla de R1 DEBE aplicar haya o no tarea activa, y en cualquier fase. Flags como `--help` no la eximen.
- R10 [D] CUANDO el hook de Claude Code trae `agent_type`, el adaptador DEBE copiarlo a `Action.Agent`. Sin `agent_type`, `Agent` queda vacío.
- R11 [D] CUANDO el guard niega una acción y hay tarea activa, el evento `guard` DEBE llevar `Agent` = `Action.Agent` (vacío para la sesión principal) y, en acciones Bash, `Data.command` con el comando cortado a 200 caracteres, además de `Data.rule`.
- R12 [N] CUANDO un evento `guard` tiene `Agent`, `bflow watch` DEBE mostrarlo entre paréntesis después de la regla.

## Design

### guard (internal/guard/guard.go)

```go
type Action struct {
    Tool     string `json:"tool"`
    Command  string `json:"command,omitempty"`
    Path     string `json:"path,omitempty"`
    Subagent bool   `json:"subagent,omitempty"`
    Agent    string `json:"agent,omitempty"` // agent_type del hook; "" en la sesión principal
}

// HumanOnly son los subcomandos que responden una decisión humana: un
// subagente no los corre (los corre la sesión principal después de preguntar).
var HumanOnly = []string{"approve", "reject", "unblock", "start", "new"}

// BflowSubcommand devuelve el subcomando de bflow de un segmento ya recortado
// ("approve" en `./bin/bflow.exe --json approve X`), o "" si el segmento no
// invoca a bflow.
func BflowSubcommand(seg string) string
```

- `bash(cmd string, c Context)` pasa a `bash(a Action, c Context)` para ver `a.Subagent`. `Evaluate` le pasa la acción.
- `bflowFreeze` (regex) se elimina. En el bucle por segmento, antes del `switch`: `sub := BflowSubcommand(s)`. Luego:
  - `sub == "freeze"` → `deny("human_only", <mensaje actual>)` (R3).
  - `a.Subagent && slices.Contains(HumanOnly, sub)` → `deny("human_only", "bflow %s responde una decisión de la persona: lo corre la sesión principal después de preguntarle. Reporta con bflow report (NEEDS_DECISION si necesitas que alguien decida).", sub)` (R1).
- Algoritmo de `BflowSubcommand`:
  1. `strings.Fields(seg)`. A cada token se le quitan comillas `"` o `'` de los extremos.
  2. Se saltan tokens que calzan `^[A-Za-z_][A-Za-z0-9_]*=`. Si el siguiente es `env`, se salta junto con los tokens `-…` y las asignaciones que le siguen.
  3. Ejecutable: con `/` en lugar de `\`, se toma lo que va después de la última `/` y se pasa a minúsculas. Si es `bflow` o `bflow.exe`, los argumentos empiezan en el token siguiente. Si es `go` y el siguiente es `run`, se saltan los tokens `-…`. El siguiente token (normalizado a `/`, sin `/` final) debe ser `cmd/bflow` o terminar en `/cmd/bflow`, y los argumentos empiezan después. Si no calza nada, se devuelve "".
  4. De los argumentos se saltan `--json` y `-json`. Se devuelve el primero en minúsculas, o "" si no hay.
- Descartado: ampliar el regex. No cubre comillas, rutas ni `env` sin volverse ilegible, y la función se prueba sola.

### Adaptador Claude (internal/adapters/agent/claude/hook.go)

`ParsePreToolUse` llena `Agent: in.AgentType` en las cuatro acciones. `Subagent` sigue siendo `AgentType != "" || AgentID != ""`.

### Log (internal/cli/hookcmds.go)

`logGuard(c *Ctx, root string, a guard.Action, rule string)`: la entrada queda `store.Entry{Event: "guard", By: e.User, Agent: a.Agent, Data: {"rule": rule, "command": clip(a.Command, 200)}}`. `command` solo va si `a.Tool == guard.Bash`. `clip` corta por runas. `needsTask` no cambia: la regla no depende de la fase.

### watch (internal/cli/watch.go)

En `case "guard"`, si `e.Agent != ""` se agrega ` (<agent>)`.

### Seguridad

- Authz: es exactamente el cambio. La autoridad para responder compuertas queda en la sesión principal (la persona). No hay authz nueva en el CLI: `bflow approve` desde una terminal sigue funcionando.
- Validación: `BflowSubcommand` solo lee texto, no ejecuta nada. Una entrada malformada devuelve "" y el comando se permite (fail-open, como el resto de guard con la config rota).
- Errores: exit 2 con el motivo por stderr, igual que las demás reglas.
- Datos sensibles: `Data.command` puede incluir la nota de un approve. Se corta a 200 caracteres y queda en `.bflow/`, igual que las notas de `approve`. Los tokens no pasan por comandos bflow de la lista.

## Tasks

- [ ] T1 Contrato (R1-R12). `Action.Agent`, `HumanOnly` y la firma `BflowSubcommand(seg string) string` en guard.go, con un stub que devuelve "". Pruebas (fallan con el stub):
  - `internal/guard/guard_test.go`: `TestHumanOnlySubagent` (approve/reject/unblock/start/new negados a subagente con `human_only`, permitidos a la principal; `report`, `block`, `status`, `task add` permitidos a subagente; `bflow status && bflow approve X` negado; `bflow approve --help` negado; sin fase). `TestFreezeEveryone` (freeze negado a ambos, también como `./bin/bflow.exe freeze`). `TestBflowSubcommand` (tabla: `bflow`, `BFLOW.EXE`, `./bin/bflow.exe`, `C:\tools\bflow.exe`, `"/usr/local/bin/bflow"`, `X=1 bflow`, `env -i X=1 bflow`, `go run ./cmd/bflow`, `go run -race C:\jaad\bflow\cmd\bflow\`, `bflow --json approve`, `bflowx approve` → "", `echo bflow approve` → "", `go run ./cmd/other approve` → "").
  - `internal/adapters/agent/claude/hook_test.go`: en `TestParsePreToolUse`, el caso con `agent_type` espera `Agent: "implementer"`, y se agrega `TestParsePreToolUseAgent` (Bash con `agent_type":"bflow-documenter"` → `Agent` y `Subagent`).
  - `cmd/bflow/e2e_hook_test.go`: `TestGuardHumanOnlySubagent`. Con tarea activa, PreToolUse Bash `bflow approve <ID> --gate walkthrough` con `agent_type: bflow-documenter` → exit 2, stderr con `bflow report`, y el log con el evento `guard`, `agent=bflow-documenter`, `rule=human_only` y `command`. Sin `agent_type` → exit 0.
- [ ] T2 Implementar `BflowSubcommand` y la regla en `bash(a Action, c)`; quitar `bflowFreeze` (R1-R9).
- [ ] T3 Adaptador Claude: `Agent: in.AgentType` en `ParsePreToolUse` (R10).
- [ ] T4 `logGuard` con agente y comando cortado; `watch` muestra el agente (R11, R12).
- [ ] T5 Docs: entrada en CHANGELOG.md; en README.md (lista de `bflow guard`, línea 66) agregar que los subagentes no aprueban, rechazan, desbloquean ni empiezan tareas; una línea en docs/guia.md (tabla de la línea 102) (R1, R11).
