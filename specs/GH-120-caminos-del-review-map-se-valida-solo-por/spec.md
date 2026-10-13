# GH-120 · caminos: del review-map se valida solo por existencia; medir si el reviewer razona de verdad

## Brief

**Objetivo:** con la medición de los review-maps posteriores a la regla, cerrar el hueco de `caminos:` vacío o de relleno y pedir que cada salida diga qué prueba la cubre, sin exigir `archivo:línea` por formato.

**Medición** (queda en esta spec para el criterio de la issue; 3 tareas, 6 líneas `caminos:`, de `bflow show <id> review-map`):
- GH-108: `review.go:297-380 flush … reinicia cur/curOK en todas las salidas` (concreto, con línea); `sin ramas nuevas salvo el bloque añadido…` (válido).
- GH-121: dos líneas con condición → estado (`region=false -> sin warn; … rechazo anterior al mtime se salta sin estado`): concretas, **sin** número de línea.
- GH-122: dos `sin ramas nuevas`, correctas (el diff solo agrega `windowsHide:true` a dos `Bun.spawn`).
- Resultado: 0 de 6 genéricas; 3 nombran salidas concretas, 3 son `sin ramas nuevas` justificadas; solo 1 trae `archivo:línea`.
- La falla real vista (T4 de GH-108) no fue de formato: el `caminos:` se escribió antes de leer la prueba que fijaba el orden. Ninguna regex la detecta.

**Entra:**
- `ParseMap` deja de aceptar un `caminos:` vacío o de relleno (`ninguno`, `nada`, `n/a`, `-`, `...`, `…`, `?`) y lo cuenta en `RedNoExits`.
- El mensaje de `review_incomplete` lo explica.
- El oficio del reviewer pide escribir `caminos:` después de leer las pruebas del archivo y nombrar, por cada salida, la prueba que la cubre o `sin prueba`.
- Una línea en CHANGELOG.

**No entra:** exigir `archivo:línea` por regex; validar que la prueba nombrada exista; cambiar REJECTED (sigue sin bloquearse).

**Decisiones nuevas:**
- [N1] No se exige `archivo:línea` dentro de `caminos:`. Descartado: con esa regla se rechazarían las dos líneas de GH-121, que razonan bien, y `review.go:1 ok` pasaría igual.
- [N2] Solo se rechaza el contenido vacío o de relleno (lista cerrada, sin distinguir mayúsculas, ignorando el punto final). Descartado: medir la longitud mínima, porque castiga `sin ramas nuevas` y no detecta un relleno largo.
- [N3] El oficio pide nombrar la prueba de cada salida (`cubre: <prueba>` o `sin prueba`). Descartado: hacerlo obligatorio en el parser, porque la medición no muestra textos genéricos que lo justifiquen.

**Riesgos:** el relleno puede cambiar a otras palabras (`ok`, `revisado`); la lista es cerrada a propósito y se amplía si aparecen. Un reviewer que ya usa `caminos: ninguno` como sinónimo de `sin ramas nuevas` ahora recibe un rechazo con el texto exacto que debe usar.

**Tamaño:** S. 1 función en `internal/review`, 1 mensaje en `internal/engine`, oficio y copias renderizadas, pruebas y CHANGELOG.

## Discovery

Carril light: sin discovery. La medición está en el Brief.

## Requirements

Van dentro de Tasks (carril light).

## Design

Van dentro de Tasks (carril light).

## Tasks

- [ ] T1 · `internal/review/review.go`: reemplazar el uso directo de `exitsRe.MatchString` (viñeta 🔴 y líneas de continuación, l.350-352) por `exitsOK(s string) bool`. `exitsOK` busca `caminos\s*:` con el mismo `exitsRe` (agregar un grupo de captura para el resto de la línea), recorta espacios y un `.` final del resto, y devuelve `false` si queda vacío o coincide con `fillerRe = (?i)^(?:ninguno|ninguna|nada|n/?a|-+|\.{3}|…|\?+)$`.
  - R1: CUANDO una viñeta 🔴 tiene `caminos:` sin texto, o con un texto de la lista de relleno, `ParseMap` DEBE incluirla en `RedNoExits`.
  - R2: CUANDO `caminos:` trae cualquier otro texto (`sin ramas nuevas`, `a.go:3 return limpia`, `region=false -> sin warn`), `ParseMap` NO DEBE incluirla en `RedNoExits`.
  - R3: CUANDO una viñeta tiene un `caminos:` de relleno y otro válido en su bloque, `ParseMap` DEBE darla por válida (basta uno válido).
  - Pruebas en `internal/review/review_test.go`: `TestParseMapRedNoExitsFiller` (casos `caminos:`, `caminos: ninguno`, `caminos: N/A.`, `caminos: -`, `caminos: …` → en `RedNoExits`; `CAMINOS: Ninguna` también) y `TestParseMapRedNoExitsConcrete` (R2 con las tres líneas reales de GH-108/121/122, y R3). `TestParseMapRedNoExits` sigue pasando sin cambios.
- [ ] T2 · `internal/engine/review.go:131`: el bloque de `RedNoExits` dice "no dicen qué caminos de salida revisaste (falta `caminos:` o está vacío/de relleno como `ninguno`)" y conserva las dos formas aceptadas.
  - R4: CUANDO el reviewer reporta APPROVED con una viñeta 🔴 cuyo `caminos:` es de relleno, `report` DEBE rechazar con `review_incomplete` y el motivo DEBE nombrar la viñeta y la palabra `relleno`.
  - R5: CUANDO el reviewer reporta REJECTED con ese mismo mapa, `report` NO DEBE bloquearse.
  - Pruebas en `internal/engine/review_test.go`: `TestReviewRejectsFillerExits` (R4) y `TestReviewRejectedIgnoresFillerExits` (R5), con el mismo armado que los casos `review_incomplete` existentes.
- [ ] T3 · Oficio: en `internal/agents/craft/reviewer.md` l.14, después de "con las salidas revisadas", decir que `caminos:` se escribe después de leer las pruebas del archivo y que cada salida nombra la prueba que la cubre (`cubre: <prueba>`) o `sin prueba`; `ninguno` o un `caminos:` vacío se rechazan. Regenerar `.claude/agents/bflow-reviewer.md` y `.opencode/agents/bflow-reviewer.md` con `bflow render` (no editarlas a mano).
  - R6: el oficio del reviewer DEBE contener `sin prueba` y `después de leer las pruebas`.
  - Prueba: ampliar la lista de `want` de la prueba del oficio en `internal/agents/agents_test.go` (l.258) con esas dos frases. Si cambia el hash de algún adapter, actualizar su fixture.
- [ ] T4 · `CHANGELOG.md`: junto a la entrada "El reviewer razona los caminos de salida" (l.39), agregar que `caminos:` vacío o de relleno (`ninguno`, `n/a`, `-`…) cuenta como ausente y que el oficio pide nombrar la prueba de cada salida. Cita R1, R4 y R6.
