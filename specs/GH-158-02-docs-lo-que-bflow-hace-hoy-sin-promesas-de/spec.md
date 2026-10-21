# GH-158 · [02] Docs: lo que bflow hace hoy, sin promesas de más

## Brief

**Objetivo:** que README, `docs/`, `CONTRIBUTING.md`, `adapters/*/README.md` y la sección v0.1.0 del `CHANGELOG.md` describan solo lo que el código de hoy hace, listos para quien pruebe la rc.10.

**Entra:**
- Una sola descripción del flujo (carriles, fases y gates de cada carril, con cuándo abren `contract`, `pause` y `repro`), con el mismo texto en README, `conceptos.md` y `guia.md`.
- Test-first por contrato (pruebas de aceptación antes del código, congeladas al validarse) y una línea con lo que exige el hotfix desde #159.
- Sección "Qué hace bflow y qué no" en el README, sin mencionar Spec Kit ni Kiro.
- Estado y rumbo sin rc.9 ni Fases 1-4; enlaces a los milestones vigentes: v0.1.0 (1), v0.1.1 (8), Bloque 2 · Difusión (6), Bloque 3 · Después de publicar (7).
- `como-probar-y-reportar.md` reescrito para la rc.10, con la pregunta de #160.
- Cotejar la tabla "Qué garantiza (y qué no)" contra `internal/guard` y sus pruebas.
- Quitar `docs/marcas-sesion.md`.

**No entra:** código, pruebas Go ni oficios de agentes; la tabla de cifras de #160; traducir al inglés; las entradas del CHANGELOG que no sean de la v0.1.0.

**Decisiones nuevas:**
- [N] `docs/marcas-sesion.md` se borra. Su lista "Detección de problemas" pasa a `guia.md#medir`, que ya explica el reparto y la línea `sin tarea`. Los tres enlaces (`README.md:94`, `conceptos.md:13`, `guia.md:126`) se quitan o apuntan a `guia.md#medir`. Descartado: moverla a `comandos.md`, que es una referencia de comandos y no explica conceptos.
- [N] `docs/cifras.md` conserva `v0.1.0-rc.9...` como la versión con la que se midió, porque es un dato histórico con fecha de corte, no el estado de hoy. Descartado: cambiar la versión sin volver a medir.
- [N] "Problemas conocidos": hoy no hay bugs abiertos (#39, #52, #53, #86, #91 están cerrados). La sección lo dice y enlaza los issues abiertos. Descartado: dejar la lista vieja.
- [N] Las gates se cuentan como en el código (`internal/flow`). Fijas: full 5 (carril, discovery, spec, preguntas, walkthrough), light 4 (sin discovery), hotfix 3 (carril, preguntas, walkthrough). Condicionales: decisión, rondas, `pause` (solo con `bflow pause`), `budget` (solo con `flow.budget`), `contract` (solo en full y solo si bflow no puede validar) y `repro` (solo en hotfix con `--no-repro`). La gate carril no aparece si `new`/`start` ya traen `--lane`.
- [N] `CONTRIBUTING.md` pone "Pruebas primero" para quien contribuye a bflow, no para lo que bflow hace. Se queda, con otro nombre ("Cada cambio con su prueba") para que no se lea como TDD.

**Riesgos:** que README, conceptos y guía vuelvan a separarse con el tiempo; lo limita que el flujo se describa una vez en conceptos y que los otros dos lo resuman y lo enlacen. Que alguna afirmación de la tabla de garantías no tenga prueba en `internal/guard/guard_test.go`: si no la tiene, la fila se corrige, no se escribe la prueba aquí.

**Tamaño:** mediano, solo docs, unos 10 archivos.

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

No aplica: carril light. Los criterios van en cada tarea.

## Design

No aplica: carril light.

## Tasks

- [x] **T1. Flujo único de hoy en `conceptos.md`.** Reescribir la viñeta de `conceptos.md:7` sin "aprobar el contrato". Agregar bajo "Gates" una tabla carril → gates fijas → gates condicionales con las condiciones de la decisión de gates del Brief (`contract` abre en full solo sin checker, sin `check.test_step` en `check.steps`, con `[build failed]`/`[setup failed]` o si falla el congelado; `pause` solo con `bflow pause`; `repro` solo con `--no-repro`). Cada fila se coteja con `internal/flow/config.go:120-122`, `internal/flow/machine.go` y `internal/engine/contract.go`.
  - CUANDO alguien lee `conceptos.md`, DEBE poder decir qué gates verá en full, light y hotfix y cuándo aparecen las condicionales.
  - Ninguna línea de `conceptos.md` pone el contrato o la pausa entre las gates de siempre.
- [x] **T2. Test-first por contrato.** En `conceptos.md` (el párrafo de la gate contract o uno nuevo, "Contrato T1") describir: T1 escribe las pruebas de aceptación y los stubs antes del código, bflow comprueba que fallan (`contract_stale`, `contract_broken`, `contract_green`, `contract_hollow`) y las congela, y solo una persona las cambia con `bflow freeze`. Una línea para el hotfix: `report DONE` exige un commit de solo pruebas, anterior al último, cuyo check falló en `check.test_step`, o `--no-repro` con gate `repro`. Renombrar la regla de `CONTRIBUTING.md` según la decisión del Brief.
  - `grep -rn "TDD" README.md docs CONTRIBUTING.md adapters` no devuelve nada.
- [x] **T3. README.** Cambiar la frase de la primera sección: "discovery, spec, contrato…" se queda solo si aclara que el contrato es del carril full y no siempre es una gate. Estado a "prerelease v0.1.0-rc.10". En "Para quién es", la viñeta "el contrato" ya no aparece como decisión humana fija. Sección nueva "Qué hace bflow y qué no", de menos de 15 líneas: un resumen de la tabla de gates por carril de T1 que enlaza a `conceptos.md`, más lo que no garantiza (calidad del código, contención, Bash fuera de lo que reconoce `guard`), sin compararse con otras herramientas. "Rumbo" con los cuatro milestones vigentes. Quitar la fila de marcas de sesión de la tabla de documentación y cambiar "las fases" por "los hitos" en la fila de rumbo. Corregir "Qué garantiza" si T5 cambia la tabla.
  - CUANDO una persona que no conoce bflow lee el README, DEBE poder decir cuántas gates fijas tienen full, light y hotfix.
  - `grep -n "Spec Kit\|Kiro\|Fase [1-4]\|rc\.9" README.md` no devuelve nada.
- [x] **T4. Guía.** En `guia.md`, la tabla "Las gates" agrega la columna o la nota de carril en cada fila (las de `repro` y `budget` no están y se agregan), y el párrafo que sigue a la tabla resume las cifras de T1 sin repetirlas con otras palabras. Agregar a "Medir" las tres comprobaciones de `marcas-sesion.md` y quitar el enlace de `guia.md:126`.
  - Las gates fijas de cada carril dan las mismas cifras en README, conceptos y guía.
- [x] **T5. Garantías contra el guard.** Para cada fila de "Qué garantiza (y qué no)" de `conceptos.md` y cada exclusión de "Lo que bflow no garantiza", ubicar el código (`internal/guard/guard.go`, `frozen_bash.go`, `loop.go`, `internal/engine`) y una prueba que la cubra. Una fila sin código se corrige o se quita; una sin prueba se suaviza ("se revisa", no "se bloquea") o se quita. Revisar con el mismo criterio `adapters/claude/README.md` y `adapters/opencode/README.md` (corregir de paso la frase duplicada "los escribe `bflow render` los escribe").
  - Cada fila que queda tiene detrás una función y una prueba que el implementer cita en el review-map.
- [x] **T6. Estado y rumbo.** En `por-que-y-rumbo.md`: estado rc.10. La tabla de rumbo queda con v0.1.0 (milestone/1), v0.1.1 (milestone/8), Bloque 2 · Difusión (milestone/6), Bloque 3 · Después de publicar (milestone/7) y 1.0, y el criterio de salida de cada una sale de la descripción del milestone (`gh api repos/innobytestech/bflow/milestones/<n>`). Revisar que "Módulos del producto" no prometa nada que el código no tenga. En `instalacion.md`, cambiar `v0.1.0-rc.9` por `v0.1.0-rc.10` en los ejemplos y "rc.9 a rc.10" por "rc.10 a rc.11".
  - `grep -rn "rc\.9\|Fase [1-4]" README.md docs` solo encuentra `docs/cifras.md:6` (dato histórico, según el Brief).
- [x] **T7. `como-probar-y-reportar.md` para la rc.10.** Título y entrada para la rc.10. Sección "Qué cambió desde la rc.9", una línea por cambio con su issue: guard en Bash (#150), comentarios narrativos (#151), eval del reviewer (#152), full sin gates de contrato ni pausa y `bflow pause` (#153, #173), hotfix con prueba que reproduce (#159). "Qué pedimos que prueben": recorrer una tarea full con check configurado y confirmar que no aparece la gate contract, y un hotfix con prueba roja y luego verde. Por cada feature cerrada, la pregunta de #160: "¿Hubo que corregir algo después del merge que el reviewer debió atrapar?", con dónde contestarla (comentario en el issue de la feature o un issue nuevo). "Problemas conocidos" según el Brief.
- [x] **T8. Borrar `marcas-sesion.md` y cerrar el inventario.** Borrar el archivo y quitar el enlace de `conceptos.md:13`. Actualizar la entrada "Documentación" de la v0.1.0 del `CHANGELOG.md` (dice "para quien prueba la rc.9") y agregar ahí una línea que resuma este cambio de docs. Repaso final de `comandos.md` y `configuracion.md` buscando afirmaciones sobre gates o contrato que contradigan T1.
  - `grep -rn "marcas-sesion" README.md docs CONTRIBUTING.md adapters` no devuelve nada.
  - Los enlaces relativos entre docs resuelven (sin anclas rotas a secciones renombradas).
