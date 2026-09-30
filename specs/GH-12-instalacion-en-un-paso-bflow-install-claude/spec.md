# GH-12 · Instalación en un paso: bflow install claude|opencode

## Brief

**Objetivo:** que `bflow install claude` deje la skill y los hooks de bflow instalados y al día en un paso, sin copiar archivos a mano.

**Entra**
- `bflow install claude [--skill-only]`: skill en `~/.claude/skills/bflow/SKILL.md`; en un repo con bflow.yaml, fusión en `<repo>/.claude/settings.json` (hooks, permiso, statusLine, attribution) y render.
- `bflow install opencode`: error claro (sin adaptador todavía).
- `bflow update` refresca la skill con el binario nuevo si ya había una instalada.
- `doctor`: skill instalada distinta o ausente; consejo de hooks apunta a `bflow install claude`; aviso de tracker local con 2+ autores en 90 días.
- Docs (README, docs/guia.md, adapters/claude/README.md) pasan a `bflow install claude`.

**No entra**
- `agent:` como lista y render para varias herramientas (GH-13).
- Adaptador de OpenCode (GH-13/GH-14).

**Decisiones nuevas**
- [N] Los archivos se embeben con un `embed.go` en el paquete `adapters/claude` (el que ya tiene adapter_test.go), importado solo por el adaptador `internal/adapters/agent/claude`. Descartado: copiar SKILL.md/settings.json dentro de internal/ (duplica la fuente de verdad).
- [N] adapter_test.go pasa a `package claude_test` para evitar el ciclo de importación en pruebas. Descartado: mover las pruebas a otro paquete.
- [N] La fusión usa un árbol JSON ordenado propio (decoder por tokens, `json.Number`), sin dependencias nuevas. Descartado: `map[string]any` (reordena claves del usuario) y sjson/jsonpatch (dependencia nueva).
- [N] Solo se escribe settings.json si el contenido cambia; al escribir se usa indentado de 2 espacios (el formato del usuario puede cambiar la primera vez; claves, orden y valores no).
- [N] El puerto `AgentAdapter` gana `InstallSkill`, `SkillState` y `InstallSettings`; la CLI no conoce el formato de settings.json.
- [N] `install` sin `agent: claude` en el repo instala skill y hooks pero no corre render (tampoco con `agent:` vacío).

**Riesgos**
- Fusionar mal un settings.json ajeno: mitigado con pruebas de orden, claves desconocidas, idempotencia y JSON inválido.
- update ejecuta el binario recién descargado: si falla, update sigue OK y dice cómo refrescar.
- El conteo de autores depende de git; sin git o sin commits no avisa.

**Tamaño:** mediano; 5 tareas, sin cambios en el tracker ni en el flujo.

## Discovery

La skill y settings.json viven en adapters/claude/ y hoy se copian a mano; no están embebidos. doctor ya revisa hooks y attribution; update solo reemplaza el binario; render escribe solo en la carpeta de la herramienta. Alcance: install claude con fusión "bflow manda en lo suyo", opencode con error, update refresca la skill, doctor compara la skill y avisa del tracker local compartido. Fuera: agent como lista y OpenCode (GH-13).

## Requirements

**install**
- R1 [D] CUANDO se corre `bflow install claude`, el sistema DEBE escribir la skill embebida en `<home>/.claude/skills/bflow/SKILL.md`, creando carpetas y sobrescribiendo lo que haya.
- R2 [D] CUANDO se corre `bflow install claude` dentro de un repo con bflow.yaml y sin `--skill-only`, el sistema DEBE fusionar los ajustes de bflow en `<root>/.claude/settings.json` según R6-R11, creando el archivo si no existe.
- R3 [D] CUANDO se corre `bflow install claude` fuera de un repo con bflow.yaml, el sistema DEBE instalar solo la skill y avisar que los hooks se instalan por repo corriendo el comando dentro de él.
- R4 [D] CUANDO el repo tiene `agent: claude`, el sistema DEBE correr render tras la fusión; CUANDO `agent` es otro valor o vacío, DEBE instalar skill y hooks, no correr render y avisar que `agent:` no incluye claude.
- R5 [D] CUANDO se pasa `--skill-only`, el sistema DEBE instalar solo la skill, sin tocar settings.json ni correr render.

**fusión de settings.json**
- R6 [D] Para cada evento de hook del binario, el sistema DEBE quitar del archivo las entradas cuyos comandos empiezan todos con `bflow ` y agregar las del binario al final del evento; las entradas del usuario quedan intactas y en su orden.
- R7 [D] SI el archivo no tiene `statusLine`, el sistema DEBE poner la del binario; SI tiene una cuyo comando no empieza con `bflow `, NO DEBE tocarla y DEBE avisar.
- R8 [D] El sistema DEBE agregar `Bash(bflow *)` a `permissions.allow` solo si falta.
- R9 [D] SI falta la clave `attribution`, el sistema DEBE ponerla con `{"commit": "", "pr": ""}`; si existe, no la toca.
- R10 [D] El sistema DEBE conservar claves desconocidas y el orden de claves del usuario, y DEBE escribir el archivo solo si su contenido cambia; correrlo dos veces seguidas NO DEBE cambiar el archivo ni duplicar hooks.
- R11 [D] SI settings.json no es JSON válido (o su raíz no es un objeto), el sistema DEBE fallar sin escribir nada en él y decir la ruta y el error.

**errores**
- R12 [D] CUANDO se corre `bflow install opencode`, el sistema DEBE fallar con un mensaje que diga que aún no hay adaptador de OpenCode (GH-13).
- R13 [D] CUANDO la herramienta no es claude ni opencode (o falta), el sistema DEBE fallar listando las disponibles.
- R14 [D] SI el home no se puede resolver o no se puede escribir, el sistema DEBE fallar con la ruta y el motivo.

**update**
- R15 [D] CUANDO update reemplaza el binario y existe `<home>/.claude/skills/bflow/SKILL.md`, el sistema DEBE ejecutar el binario nuevo con `install claude --skill-only`; si no existe, NO DEBE instalarla.
- R16 [D] SI ese paso falla, update DEBE terminar OK y avisar que se refresca con `bflow install claude`.

**doctor**
- R17 [D] CUANDO `agent: claude`, doctor DEBE avisar (warn) si la skill instalada falta o difiere de la embebida tras normalizar CRLF a LF, sugiriendo `bflow install claude`; si coincide, ok.
- R18 [D] Los avisos de hooks faltantes DEBEN sugerir `bflow install claude` en vez de copiar adapters/claude/settings.json; lo mismo el aviso de attribution.
- R19 [D] CUANDO el tracker es local y los commits de los últimos 90 días tienen 2+ emails de autor distintos, doctor DEBE avisar que .bflow/ no se comparte y sugerir tracker github o plane.

**salida y docs**
- R20 [N] install DEBE devolver envelope con code `installed` y data compacta `{"skill":"<ruta>","settings":"<ruta>|\"\"","changed":bool,"render":bool,"warnings":[...]}`; texto para personas sin emojis.
- R21 [D] README, docs/guia.md y adapters/claude/README.md DEBEN indicar `bflow install claude` en lugar de copiar archivos a mano.

## Design

**Embebido (fuente única).** Nuevo `adapters/claude/embed.go`:
```go
package claude
import _ "embed"
//go:embed skills/bflow/SKILL.md
var Skill []byte
//go:embed settings.json
var Settings []byte
```
`adapters/claude/adapter_test.go` cambia a `package claude_test` (importa `internal/cli`; sin ese cambio el paquete de prueba formaría ciclo con el adaptador). archtest no se afecta: `adapters/claude` está fuera de `internal/`, y solo lo importa `internal/adapters/agent/claude`.

**Puerto** (`internal/cli/cli.go`, `AgentAdapter`), tres métodos nuevos:
```go
// InstallSkill escribe la skill embebida en home y devuelve su ruta.
InstallSkill(home string) (path string, err error)
// SkillState compara la skill instalada en home con la embebida: "ok", "missing" o "stale".
SkillState(home string) (path, state string)
// InstallSettings fusiona los ajustes de bflow en la configuración del repo.
InstallSettings(root string) (agents.SettingsResult, error)
```
`agents.SettingsResult` (en `internal/agents`, sin dependencias): `Path string; Changed bool; Warnings []string`.

**Adaptador** `internal/adapters/agent/claude/install.go`: implementa los tres métodos. `mergeSettings(cur, ours []byte) (out []byte, changed bool, warnings []string, err error)` es pura y es lo que se prueba a fondo.
- Árbol ordenado `node` (objeto = `[]member{key string; val *node}`, arreglo, escalar con `json.RawMessage`), leído con `json.Decoder.Token` + `UseNumber`; serializa con indentado de 2 espacios y `\n` final.
- Una entrada de hook es "de bflow" si todos sus `hooks[].command` empiezan con `bflow ` (R6). Eventos que el usuario no tiene se agregan al final del objeto `hooks`.
- `changed` = bytes nuevos != bytes actuales tras normalizar CRLF; si no cambia, no se escribe (R10).
- Escritura: archivo temporal en la misma carpeta + rename, permisos 0o644; carpeta `.claude` con 0o755.
- Comparación de skill: `bytes.ReplaceAll(b, "\r\n", "\n")` en ambos lados (R17).

**Comando** `internal/cli/installcmds.go`: `Register(&Command{Name: "install", Summary: "instala la skill y los hooks de bflow para tu herramienta: install claude|opencode [--skill-only]", ...})`, flag `--skill-only`.
- Home: `os.UserHomeDir()`; error -> `output.Fail("install", ...)` con motivo (R14).
- Herramienta: `c.Args[0]`; `opencode` -> `output.Fail("install_unsupported", ...)` (R12); otra/vacía -> `output.Fail("usage", ...)` con "disponibles: claude, opencode" (R13).
- Repo: `config.FindRoot(c.Dir)` + existencia de bflow.yaml; `config.Load` para leer `Agent`.
- Render: se extrae de `runRender` la función `applyRender(c *Ctx, cfg *config.Config) (map[string]any, error)`, que usan `render` e `install`. Conflictos de render se reportan como warning en install (skill y hooks ya quedaron), no como fallo.

**update** (`updatecmds.go`): antes de `release.Replace`, se mira si existe la skill (`SkillState != "missing"`); después, `exec.Command(exe, "install", "claude", "--skill-only", "--json")` con timeout de 30 s. `data["skill"]` = `"updated" | "failed" | "skipped"`; en `failed` el texto agrega "corre bflow install claude para refrescar la skill" (R15, R16).

**doctor** (`setupcmds.go`, bloque `cfg.Agent == "claude"`):
- Nuevo ítem `skill` con `c.Agent.SkillState(home)` (R17).
- Textos de hooks faltantes y attribution: "corre bflow install claude" (R18).
- Nuevo ítem `tracker` si `cfg.Tracker.Adapter == "local"`: `gitOut(cfg.Root, "log", "--since=90.days", "--format=%ae")`, emails únicos en minúsculas; 2+ -> warn (R19). Sin git: no agrega ítem.

**Seguridad**
- Solo escribe en `<home>/.claude/skills/bflow/` y `<root>/.claude/settings.json`; rutas construidas con `filepath.Join`, sin entradas del usuario en la ruta.
- JSON inválido no se sobrescribe (R11); escritura atómica evita dejar un settings.json a medias.
- update ejecuta solo el binario que acaba de reemplazar (`os.Executable()`), con argumentos fijos.
- El aviso de autores no imprime emails, solo el conteo.

**Descartado**
- Instalar hooks en `~/.claude/settings.json` global: `bflow guard` solo aplica donde hay bflow.yaml y el diseño vigente los pone por repo.
- Que update instale la skill aunque no existiera: no instala cosas que el usuario no pidió.

## Tasks

- [x] T1 Contrato: `adapters/claude/embed.go` (`Skill`, `Settings`), adapter_test.go a `package claude_test`, los tres métodos en `AgentAdapter` con stubs en el adaptador, `agents.SettingsResult`, comando `install` registrado que solo valida la herramienta. Pruebas (nombres): `TestMergeSettingsEmpty`, `TestMergeSettingsKeepsUserHooksAndOrder`, `TestMergeSettingsReplacesBflowHooks`, `TestMergeSettingsIdempotent`, `TestMergeSettingsForeignStatusLine`, `TestMergeSettingsAttributionOnlyIfMissing`, `TestMergeSettingsInvalidJSON`, `TestSkillState`, `TestInstallClaude` (e2e), `TestInstallOutsideRepo`, `TestInstallOtherAgentNoRender`, `TestInstallSkillOnly`, `TestInstallOpencodeUnsupported`, `TestInstallUnknownTool`, `TestUpdateRefreshesSkill`, `TestDoctorSkillStale`, `TestDoctorLocalTrackerManyAuthors`. Cubre R12, R13.
- [x] T2 Adaptador: `InstallSkill`, `SkillState`, `mergeSettings` con árbol ordenado e `InstallSettings` con escritura atómica. R1, R6-R11, R14, R17 (comparación).
- [x] T3 Comando install completo: home, detección de repo, `--skill-only`, `applyRender` extraída de `runRender`, avisos y envelope. R1-R5, R14, R20.
- [x] T4 update y doctor: refresco de skill tras reemplazar binario; ítem skill, textos nuevos de hooks/attribution, aviso de tracker local con varios autores. R15-R19.
- [x] T5 Docs: README, docs/guia.md, adapters/claude/README.md y CHANGELOG. R21.
