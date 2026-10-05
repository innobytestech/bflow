# GH-94 · Documentación completa: por qué, rumbo, instalación y uso para quien prueba la rc.9

## Brief

**Objetivo:** que un dev que llega de cero entienda por qué existe bflow, cómo instalarlo, usarlo de punta a punta (Claude Code y OpenCode), qué garantiza, el rumbo y cómo reportar issues de la rc.9, sin preguntarle a nadie.

**Entra**
- README corto (puerta de entrada): qué es, por qué (resumen), para quién, instalar (resumen), inicio rápido, rumbo (resumen), enlaces a `docs/`. Estado: "MVP" pasa a "prerelease v0.1.0-rc.9".
- Nuevos en `docs/`: `por-que-y-rumbo.md`, `instalacion.md`, `conceptos.md`, `configuracion.md`, `comandos.md`, `como-probar-y-reportar.md`.
- Ajustar `docs/guia.md` (enlaces, sin duplicar) y enlazar `docs/marcas-sesion.md` desde `conceptos.md`.
- Corregir lo que el README dice y ya no es cierto (ver Riesgos).

**No entra**
- Traducción al inglés (GH-62, GH-63). Cambios de código o de mensajes del CLI. CHANGELOG (es solo docs).

**Decisiones nuevas**
- [N] Desinstalar se documenta a mano (borrar el binario, `~/.claude/skills/bflow`, las entradas de bflow en `.claude/settings.json`, los archivos que escribe `render`, las credenciales del llavero): no hay comando `uninstall`. Descartado: anunciar un `bflow uninstall` que no existe.
- [N] Los problemas conocidos de la rc.9 se listan con enlace a sus issues abiertas (#39, #52, #53, #54, #55, #84, #86, #91). Descartado: un texto genérico que envejece sin enlace.
- [N] La tabla de garantías pasa los ✅/❌ a texto ("Sí", "No", "Sí, se bloquea antes…"): sin emojis en todo el texto nuevo. Descartado: dejarlos (la regla del repo es sin emojis).
- [N] El rumbo enlaza los milestones existentes (1 v0.1.0, 2-5 Fase 1-4) por URL `https://github.com/innobytestech/bflow/milestone/<n>`; la 1.0 va sin enlace (no hay milestone).

**Riesgos**
- Inventar: el README actual ya trae errores. "hoy solo Claude Code tiene adaptador" (OpenCode ya lo tiene); la tabla de comandos omite `hook subagent-stop`, `install`, `update` y `version`; el CHANGELOG menciona `bflow metrics`, que no es un comando (`bflow help` no lo lista). Cada comando y flag se verifica contra `bflow help` y `internal/cli/*.go`.
- La motivación en primera persona es borrador: Alfonso la ajusta en este gate.
- Pérdida de información al mover el README: se cubre con T8.

**Tamaño:** 1 PR solo de docs, ~8 archivos. Carril light.

## Discovery

Carril light: sin discovery. Fuente: el issue GH-94 (decisiones de Alfonso del 2026-10-05) y el scout.

## Requirements

Carril light: los criterios van en cada tarea.

## Design

Carril light: sin design aparte. Fuentes de verdad para verificar: `bflow help`, `internal/cli/*.go` (flags), `install.sh`/`install.ps1` (variables `BFLOW_*`), `CHANGELOG.md`, `internal/flow/config.go` (fases y carriles), milestones e issues de GitHub.

## Tasks

- [x] **T1 · `docs/por-que-y-rumbo.md`.** Motivación en primera persona de Alfonso (Innobytes) a partir de "Por qué existe" del README (harness en dos repos, reglas en prosa respetadas "casi" siempre, estado movido a mano, ~7.000 tokens fijos por sesión, 73-80% del harness versionado era estado, copias que se desviaban); el problema; la meta; la idea (criterio para el agente, lo determinista para el CLI). Rumbo: v0.1.0, Fase 1-4 con su criterio de salida tal como está en GH-94 y enlace a cada milestone, 1.0 (API de comandos estable). Criterios: cada fase enlaza su milestone; lo no hecho se dice como previsto.
- [x] **T2 · `docs/instalacion.md`.** Requisitos (Claude Code 2.1.271+ si es el agente, OpenCode, Go 1.25+ solo para `go install`), Linux/macOS (`install.sh`, `BFLOW_INSTALL_DIR`), Windows (`install.ps1`, `BFLOW_INSTALL_NO_PATH`), `BFLOW_VERSION`, checksums y `gh attestation verify`, `go install` y desde el código, verificar (`bflow version`, `bflow doctor`), actualizar (`bflow update [--check] [--force]`, canales), instalar en la herramienta (`bflow install claude|opencode [--skill-only]`), desinstalar a mano. Criterios: cada variable existe en los scripts; ningún comando `uninstall`.
- [x] **T3 · `docs/conceptos.md`.** Núcleo y adaptadores (diagrama Mermaid actual), fases y carriles (full, light, hotfix, tabla actual), gates, el contrato `next` (envelope, códigos 0/1/2, acciones ask/spawn/wait/done, `clear`), qué queda en el repo y qué no, tabla de garantías en texto (sin ✅/❌), lo que no garantiza. Enlaza `marcas-sesion.md` y `guia.md`. Criterio: las fases coinciden con `internal/flow/config.go`.
- [x] **T4 · `docs/configuracion.md`.** `bflow.yaml` y capas (perfil global, repo), `profile add|list|use`, `connect plane|github`, trackers local, Plane y GitHub (`tracker setup`, `tracker states`, `--project owner/N`), `agent:` lista y `models`, `agents:` y `render [--check]`, `check` (pasos, `--quick`, `--verify`), validación. Mueve el detalle de "Configuración" y "Conectar Plane y GitHub" del README.
- [x] **T5 · `docs/comandos.md`.** Referencia de todos los comandos de `bflow help`, agrupados como la tabla actual, con firma y flags; incluye `stats --calls --reads`. Criterio: la lista coincide uno a uno con `bflow help` (ni más ni menos); `--json` y códigos de salida mencionados una vez.
- [x] **T6 · `docs/como-probar-y-reportar.md`.** Qué esperamos de quien prueba (recorrer una tarea con el tracker local, luego con su tracker y agente), cómo reportar un issue útil (`bflow version`, `bflow doctor`, salida completa con `--json` cuando sale con código 2, pasos, agente y tracker, SO), problemas conocidos de la rc.9 con enlace a #39, #52, #53, #54, #55, #84, #86, #91.
- [x] **T7 · README y `docs/guia.md`.** README corto: qué es, estado "prerelease v0.1.0-rc.9", por qué (resumen + enlace a T1), para quién (corregido: Claude Code y OpenCode), instalar (un comando por SO + enlace a T2), inicio rápido (tracker local, `bflow install claude|opencode`), rumbo resumido con enlace, índice de `docs/`, desarrollo, licencia. Mermaid se mantiene donde aparezca. `guia.md`: enlaces a conceptos, configuración y comandos donde hoy repite; sin borrar contenido propio.
- [x] **T8 · Verificación.** Cada sección del README de 418 líneas está en el README nuevo o en un archivo de `docs/` (lista de sección → destino en el PR). Ningún emoji en el texto nuevo (`grep` de ✅❌🟡⏳🔴 vacío salvo citas literales de salidas del CLI). Los enlaces relativos entre docs resuelven. Cada comando citado existe en `bflow help`.
