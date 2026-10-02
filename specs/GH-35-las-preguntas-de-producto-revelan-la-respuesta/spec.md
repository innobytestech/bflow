# GH-35 · Las preguntas de producto revelan la respuesta

## Brief

**Objetivo:** que las opciones del gate `questions` no delaten cuál es la que hace el código, para que la comparación del walkthrough sirva.

**Entra**
- `review.ParseMap` también lee `## Preguntas de producto` y junta las opciones que delatan la respuesta ("(lo que hace el código)", "(actual)", "(implementado)", "✓"…).
- Si hay opciones así, el APPROVED del reviewer se rechaza con `review_incomplete`, igual que las viñetas 🔴 sin ruta.
- `bflow show <id> questions` muestra las opciones de cada pregunta en un orden mezclado que no cambia entre llamadas.
- `internal/agents/craft/reviewer.md`: las preguntas tratan sobre comportamiento que no quedó fijado en el discovery ni en las decisiones [N] del brief (casos borde, errores, interacción con otras funciones), y ninguna opción puede marcar cuál es la respuesta.

**No entra**
- Detectar con código que una pregunta repite una decisión del discovery o del brief: eso queda en el oficio del reviewer.
- Cambiar la línea `el código responde:` o el formato del walkthrough.
- Volver a validar los review-maps de tareas ya cerradas.

**Decisiones nuevas**
- [N] Rechazar en vez de limpiar. Descartado: quitar la marca y mostrar la opción. Aunque se quite la marca, la opción sigue escrita para justificar el código (más larga y más precisa que las otras), y la pregunta tampoco se rehace. Con el rechazo, el reviewer la reescribe.
- [N] Un orden mezclado y estable: la semilla es un hash FNV-1a del id de la tarea más el texto de la pregunta. Descartado: un orden al azar en cada llamada. `show questions` se llama más de una vez (en el display del gate y a mano), y si el orden cambia el humano se confunde. Descartado también: ordenar alfabéticamente, porque se puede adivinar.
- [N] Las marcas se detectan con una lista cerrada de patrones (ver T1). Descartado: rechazar cualquier opción que diga "código", porque en este repo hay opciones legítimas como "error con código 422".

**Riesgos**
- Falsos positivos ("actual" en una opción legítima): el rechazo dice qué opción y qué patrón coincidió, y el reviewer la reescribe. Es una ronda más, no un bloqueo.
- Falsos negativos con marcas que no están en la lista: el oficio sigue prohibiéndolas.

**Tamaño:** S. Cambia 3 archivos de Go (`internal/review/review.go`, `internal/engine/review.go`, `internal/engine/display.go`) más sus pruebas, el oficio del reviewer y el changelog.

## Discovery

Carril light: no hay discovery.

## Requirements

Carril light: los criterios están en Tasks.

## Design

Carril light: el diseño está en cada tarea.

## Tasks

- [x] **T1 · Detectar las opciones que delatan la respuesta** (`internal/review/review.go`).
  - `Map` gana el campo `QuestionTells []string`: cada opción delatora, recortada a 80 runas.
  - `ParseMap` reconoce como sección de preguntas el encabezado que contiene "preguntas" (sin distinguir mayúsculas), con el mismo cierre por nivel que 🔴 y Docs. Dentro de esa sección, una opción es una viñeta `-`/`*` con sangría o que va debajo de un ítem de pregunta. Las líneas `el código responde:` no son opciones.
  - `var tellRe` es una regex sin distinguir mayúsculas. Busca, como palabras completas: `el código`, `lo que hace`, `implementad[oa]s?`, `implementación`, `actual(mente)?`, `hoy`, `correct[oa]s?`, `recomendad[oa]s?`, `esperad[oa]s?`. También busca los símbolos `✓`, `✔`, `✅` y `←`.
  - R1: CUANDO una opción de `## Preguntas de producto` coincide con `tellRe`, `ParseMap` DEBE agregarla a `QuestionTells`.
  - R2: CUANDO una opción no coincide (por ejemplo, "Error con código 422", "Lo rechaza con 422"), `ParseMap` NO DEBE agregarla.
  - R3: Las viñetas de otras secciones y la línea `el código responde:` NO DEBEN contar como opciones.
  - Pruebas: `TestParseMapQuestionTells` (con el caso de GH-29 literal) y `TestParseMapQuestionTellsIgnoresOtherSections`.

- [x] **T2 · Rechazar el APPROVED con opciones delatoras** (`internal/engine/review.go`).
  - `reviewIncomplete` agrega un bloque cuando `len(m.QuestionTells) > 0`: "Estas opciones de `## Preguntas de producto` delatan cuál hace el código; reescríbelas neutras, sin marcas ni menciones al código:\n- …".
  - R4: CUANDO el reviewer reporta APPROVED y el review-map tiene opciones delatoras, bflow DEBE rechazar con el código `review_incomplete` y listar esas opciones.
  - R5: CUANDO no hay opciones delatoras, el rechazo NO DEBE cambiar respecto de hoy.
  - Pruebas: `TestReviewIncompleteQuestionTells` en `internal/engine` (función directa), y un caso en la prueba del flujo de reporte del reviewer si ya existe una para RedNoPath.

- [x] **T3 · Mezclar las opciones al mostrarlas** (`internal/engine/display.go`).
  - `productQuestions` junta cada bloque contiguo de viñetas de opción que va debajo de una pregunta y lo reordena con `shuffleOptions(seed uint64, opts []string)`. La función usa `math/rand/v2` con `rand.NewPCG(seed, seed)`. La semilla sale de `hash/fnv` 64a sobre `id + "\n" + texto de la pregunta`. El texto de la pregunta y su numeración no se tocan.
  - R6: CUANDO se muestran las preguntas, el orden de las opciones de cada pregunta DEBE ser una permutación de las originales y DEBE ser el mismo en dos llamadas para la misma tarea.
  - R7: Las líneas `el código responde` DEBEN seguir ocultas (no se rompe `TestProductQuestionsHideAnswers`: se ajusta el `want` al orden determinista o se compara como conjunto por pregunta).
  - R8: CUANDO hay varias preguntas, cada una se mezcla con su propia semilla. Con un review-map de 4 preguntas de prueba, al menos una DEBE quedar en un orden distinto al original. La prueba fija ese caso.
  - Pruebas: `TestProductQuestionsShuffleStable`, `TestShuffleOptionsPermutation`.

- [x] **T4 · Oficio del reviewer y changelog** (`internal/agents/craft/reviewer.md`, `CHANGELOG.md`).
  - En la viñeta de `## Preguntas de producto`, hay que reemplazar "en orden neutro (una es lo que hace el código, las otras alternativas razonables)" por: las preguntas tratan sobre comportamiento que no quedó fijado en el discovery ni en las decisiones [N] del brief (casos borde, errores, interacción con otras funciones); una opción es lo que hace el código y las otras son alternativas razonables del mismo largo y tono; ninguna opción menciona el código ni marca cuál es la respuesta ("actual", "implementado", ✓…); bflow mezcla el orden y rechaza las opciones con marcas.
  - Correr `bflow render` para regenerar `.claude/agents/bflow-reviewer.md`.
  - R9: El oficio renderizado DEBE pedir preguntas fuera de lo fijado en el discovery y en las [N], y DEBE prohibir las marcas en las opciones.
  - Si existe una prueba de golden o de contenido del oficio en `internal/agents`, hay que actualizarla.
  - Agregar una entrada al CHANGELOG en Unreleased, del lado del consumidor: las opciones delatoras se rechazan y el orden se mezcla.
