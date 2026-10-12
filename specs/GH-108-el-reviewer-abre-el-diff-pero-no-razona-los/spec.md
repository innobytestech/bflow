# GH-108 · El reviewer abre el diff pero no razona los caminos de error ni rechaza pruebas que no ejecutan el código

## Brief

**Objetivo:** que el reviewer razone cada salida de las ramas que cambian y deje constancia en el review-map, y que una prueba que solo lee el fuente no baste como única prueba de un criterio.

**Entra**
- Oficio `internal/agents/craft/reviewer.md`: punto nuevo **Corrección** (por cada rama nueva o cambiada, seguir `return`/`continue`/`break`/error y verificar limpieza de estado, orden de efectos y datos opcionales ausentes) y regla de pruebas que solo inspeccionan texto del fuente.
- Formato del review-map: cada viñeta 🔴 lleva una línea `caminos:` con las salidas revisadas; sección `## Pruebas sin ejecutar` para justificar una prueba por texto que sea la única de un criterio.
- bflow valida, con APPROVED, que cada viñeta 🔴 tenga su `caminos:` (rechazo `review_incomplete`, como `## Docs`).
- Docs: `docs/guia.md` (párrafo del walkthrough) y `CHANGELOG.md`.

**No entra**
- La salida temprana de `adapters/opencode/plugin2/index.js:112` (`continue` antes de `steps.delete`, deja la entrada huérfana): es un defecto real del plugin; va a otra tarea.
- Reescribir las pruebas por regex de `plugin_test.go` para que ejecuten el JS (requiere node en CI; otra tarea).
- Medir en bflow si el razonamiento es bueno: bflow solo comprueba que la constancia exista.

**Decisiones nuevas**
- [N1] `caminos:` obligatorio por 🔴 y validado por bflow. Descartado: dejarlo solo en el oficio; abrir el diff ya era regla del oficio y el reviewer la cumplía en lo formal sin razonar. Exigir constancia por escrito deja al humano ver qué se razonó.
- [N2] Si una prueba solo lee texto del fuente es algo que juzga el reviewer, no bflow. Descartado: detectar en bflow `regexp` sobre fuentes embebidos; hay pruebas legítimas que leen texto (plantillas, oficios) y daría falsos positivos.
- [N3] La justificación va en `## Pruebas sin ejecutar` del review-map, una viñeta `` `ruta_test:línea`: R<n>, motivo ``. Sin justificación, el reviewer reporta REJECTED. Descartado: justificarla en `## Seguimiento`, que es para hallazgos menores y fue justo donde se perdió en GH-103.

**Riesgos**
- Que el reviewer llene `caminos:` con texto genérico. Para mitigarlo, el oficio pide nombrar las salidas con archivo:línea y el estado que deja cada una.
- Review-maps viejos sin `caminos:` no se revalidan: la regla solo aplica al reportar APPROVED.

**Tamaño:** S. Oficio (~4 líneas), `ParseMap` + `reviewIncomplete`, sus pruebas y dos docs.

## Discovery

Carril light: sin discovery. El scout confirmó que el defecto de GH-103 ya está corregido en `index.js:113-114`, encontró otro igual en `index.js:112` y vio que las pruebas de `plugin_test.go` (`TestPlugin2SinDependencias`, `TestPlugin2Guard`) solo aplican regex al fuente.

## Requirements

Carril light: los criterios van en cada tarea.

## Design

Carril light: los nombres exactos van en cada tarea.

## Tasks

- [x] **T1 · Oficio del reviewer** (`internal/agents/craft/reviewer.md`)
  - R1: en las viñetas de revisión, después de **Pruebas**, va un punto **Corrección**: por cada rama nueva o modificada, sigue cada salida (`return`, `continue`, `break`, error, excepción) y comprueba que el estado queda consistente (entradas de mapas, recursos, cursores, archivos), el orden de los efectos (limpiar antes de salir) y el caso sin datos opcionales. Una salida que deja estado inconsistente es bloqueante: REJECTED con archivo:línea en `--note`.
  - R2: **Pruebas** agrega que una prueba que solo inspecciona el texto del fuente (regex o `strings.Contains` sobre el código) en vez de ejecutarlo es bloqueante si es la única prueba de un criterio R, salvo que `## Pruebas sin ejecutar` lo justifique.
  - R3: en el formato del review-map, cada viñeta 🔴 lleva debajo una sub-viñeta `- caminos: …` con las salidas revisadas (archivo:línea y estado que deja cada una), o `- caminos: sin ramas nuevas`. Se documenta la sección `## Pruebas sin ejecutar` con viñetas `` `ruta_test:línea`: R<n>, <por qué no se puede ejecutar> ``.
  - El oficio sigue sin duplicar reglas existentes y no pasa de 40 líneas.
  - Prueba: `TestReviewerCraftCorreccion` en `internal/agents/agents_test.go` verifica que el cuerpo del reviewer contiene `Corrección`, `caminos:` y `## Pruebas sin ejecutar`.
- [x] **T2 · bflow exige `caminos:` en cada 🔴** (`internal/review/review.go`, `internal/engine/review.go`)
  - R4: `review.Map` gana `RedNoExits []string`: viñetas 🔴 de primer nivel (las que hoy entran en `Red` o `RedNoPath`) cuyo bloque (la viñeta y sus líneas con sangría hasta la siguiente viñeta de primer nivel o encabezado) no contiene `caminos:` (sin distinguir mayúsculas, regex `(?i)(?:^|[^\pL])caminos\s*:`). Cada entrada es el texto de la viñeta recortado a 80 runas. Vale también `caminos:` en la misma línea de la viñeta.
  - R5: CUANDO el reviewer reporta APPROVED y `RedNoExits` no está vacío, `reviewIncomplete` DEBE añadir el bloque "Estas viñetas 🔴 no dicen qué caminos de salida revisaste; agrega debajo `- caminos: <archivo:línea y estado que deja>` o `- caminos: sin ramas nuevas`:" con la lista, dentro del mismo rechazo `review_incomplete`. REJECTED nunca se bloquea por esto.
  - R6: un review-map sin sección 🔴 no produce `RedNoExits`.
  - Pruebas: `TestParseMapRedNoExits` en `internal/review/review_test.go` (sub-viñeta, misma línea, mayúsculas, viñeta sin `caminos:`, sangría de continuación, encabezado que cierra la sección) y ampliar el `reviewMap` de `TestParseMap` para que sus 🔴 traigan `caminos:`; una prueba en `internal/engine` (junto a las de `reviewIncomplete`) que reporte APPROVED con un 🔴 sin `caminos:` y espere `review_incomplete` con el texto de la viñeta.
- [x] **T3 · Docs** (R7)
  - `docs/guia.md`, párrafo del walkthrough (l.34): agregar que bflow rechaza un 🔴 sin `caminos:` y que `## Pruebas sin ejecutar` justifica pruebas por texto.
  - `CHANGELOG.md`: una viñeta en la versión en curso, junto a las de `review_incomplete`.
- [ ] **T4 · Verificación manual de los criterios de la tarea** (R1, R2)
  - En una rama desechable, mover `steps.delete` después del `continue` de `step.failed` en `adapters/opencode/plugin2/index.js` y correr el reviewer: debe reportar REJECTED citando `index.js:<línea>`. Anotar el resultado en `reports/impl.md`; la rama no se mergea.
  - Revisar un diff donde la única prueba de un R sea una regex sobre el fuente, sin `## Pruebas sin ejecutar`: no debe salir APPROVED.
