# GH-39 · guard: reconocer ejecutables entre comillas con espacios en la ruta

## Brief

**Objetivo:** que el guard reconozca un ejecutable escrito entre comillas con espacios en la ruta (`"C:\Program Files\Git\bin\bash.exe" -c ...`, `"C:\Program Files\bflow\bflow.exe" approve X`) y le aplique las mismas reglas que al nombre sin comillas.

**Entra**
- Un tokenizador `quotedFields` en `internal/guard/guard.go`: parte por espacios fuera de comillas; un tramo `"…"` o `'…'` queda dentro de un solo token, con sus comillas.
- Usarlo en vez de `strings.Fields` en `unwrapShell` (incluido el cálculo del texto tras `-c`), `bflowTokens` (y así `BflowSubcommand`/`BflowArgs`) y `feedsShell`.
- Usarlo también en `internal/guard/frozen_bash.go` (`frozenWrite` l.127 y la cabecera de heredoc l.221), que hoy tienen el mismo defecto.
- Pruebas nuevas con los dos ejemplos de la tarea y casos del tokenizador.

**No entra**
- Un parser de shell completo: no se expanden variables, `$(...)`, globs ni secuencias `$'...'`.
- Cambiar cómo `splitCommand` corta segmentos (ya respeta comillas).

**Decisiones nuevas**
- [N] `\` no escapa nada dentro ni fuera de comillas: en Windows es separador de ruta. Descartado: el escape `\"` de bash, que rompería `"C:\dir\" x`. Consecuencia: `"a\"b"` se corta distinto que en bash; el token resultante sigue empezando por la ruta, así que el nombre del ejecutable se reconoce igual.
- [N] Comillas simples y dobles valen lo mismo. Descartado: tratar solo las dobles (PowerShell y bash aceptan ambas).
- [N] Los tokens conservan sus comillas; `skipPrefix` y `exeName` las siguen quitando como hoy. Descartado: que el tokenizador las quite, porque `unwrapShell` necesita que cada token sea un tramo literal del segmento para recortar el texto tras `-c`.
- [N] Una comilla sin cerrar extiende el token hasta el final del segmento. Descartado: volver a `strings.Fields`, que reabriría el hueco con una comilla suelta.
- [N] frozen_bash.go entra en alcance: es el mismo defecto y el mismo arreglo de una línea. Descartado: dejarlo para otra tarea y tener dos tokenizaciones distintas en el guard.

**Riesgos**
- `BflowArgs` ahora devuelve `"título con espacios"` como un argumento (con comillas quitadas por `skipPrefix`) en vez de varios. Su único consumidor (`hookcmds.go:174`) busca IDs en mayúsculas; no le afecta.
- Un comando que hoy pasa por estar mal tokenizado podría negarse: es el objetivo.

**Tamaño:** S. Un archivo de código principal, uno secundario y pruebas en `guard_test.go`.

## Discovery

Carril light, sin discovery. El scout ubicó todo el código en `internal/guard/guard.go` y `internal/guard/frozen_bash.go`; las preguntas abiertas que dejó (tokenizador compartido, escape con `\`, comillas simples, alcance de frozen_bash.go) se resuelven en las decisiones [N] del Brief.

## Requirements

Ver criterios dentro de Tasks (carril light).

## Design

Ver Tasks (carril light).

## Tasks

- [ ] T1. Tokenizador `func quotedFields(s string) []string` en `internal/guard/guard.go`.
  - Parte por espacios en blanco (`unicode.IsSpace`) fuera de comillas; `"` o `'` abre un tramo que cierra la misma comilla; un token puede mezclar tramos (`a"b c"d` es un token). `\` no es especial. Comilla sin cerrar: el token llega al final.
  - Cada token es un substring literal de `s` (comillas incluidas); sin tokens vacíos.
  - R1: CUANDO `s` es `"C:\Program Files\Git\bin\bash.exe" -c "bflow approve X"`, `quotedFields` DEBE devolver 3 tokens: la ruta entre comillas, `-c` y `"bflow approve X"`.
  - R2: CUANDO `s` no contiene comillas, `quotedFields(s)` DEBE ser igual a `strings.Fields(s)`.
  - R3: CUANDO `s` tiene una comilla sin cerrar o es vacío/solo espacios, `quotedFields` NO DEBE entrar en pánico; con `""` o espacios DEBE devolver un slice vacío.
  - Prueba: `TestQuotedFields` (tabla `{in, want}`).
- [ ] T2. `unwrapShell` y `feedsShell` usan `quotedFields`.
  - En `unwrapShell`, `fields := quotedFields(seg)` y el bucle que avanza `rest` usa `quotedFields(rest)` para medir cada token.
  - R4: CUANDO un subagente corre `"C:\Program Files\Git\bin\bash.exe" -c "bflow approve X"`, el guard DEBE negarlo por `human_only`.
  - R5: CUANDO el ejecutable entre comillas es `'C:\Program Files\Git\bin\sh.exe'` (comillas simples), `Segments` DEBE devolver el comando interno.
  - R6: CUANDO `bash <<EOF` aparece con la ruta entre comillas con espacios (`"C:\Program Files\Git\bin\bash.exe" <<EOF`), `feedsShell` DEBE reconocer el shell.
  - R7: los casos actuales de `TestShellWrapped`, `TestSegmentsUnwrap`, `TestShellEmptyTokens` y `TestHeredocExecutedByShell` DEBEN seguir pasando sin cambios.
  - Pruebas: casos nuevos en `TestShellWrapped` y `TestSegmentsUnwrap`.
- [ ] T3. `bflowTokens` usa `quotedFields`.
  - R8: CUANDO el segmento es `"C:\Program Files\bflow\bflow.exe" approve X`, `BflowSubcommand` DEBE devolver `approve`, y un subagente que lo corre DEBE recibir negación por `human_only`.
  - R9: CUANDO el segmento es `bflow new "título con espacios"`, `BflowArgs` DEBE devolver `["título con espacios"]`.
  - R10: los casos actuales de `TestBflowSubcommand`, `TestBflowArgs` y `TestHumanOnlySubagent` DEBEN seguir pasando sin cambios.
  - Pruebas: casos nuevos en `TestBflowSubcommand`, `TestBflowArgs` y `TestHumanOnlySubagent`.
- [ ] T4. `frozen_bash.go` usa `quotedFields` en `frozenWrite` (l.127) y en la cabecera de heredoc (l.221).
  - R11: CUANDO un subagente en fase con pruebas congeladas corre `"C:\Program Files\Git\bin\bash.exe" -c "echo x > <prueba congelada>"`, el guard DEBE negarlo igual que con `bash -c`.
  - R12: CUANDO la redirección apunta a una ruta entre comillas con espacios que es una prueba congelada (`echo x > "dir con espacio/a_test.go"`), el guard DEBE negarla.
  - R13: los casos actuales de `TestBashFrozenWrites`, `TestBashFrozenReads`, `TestBashFrozenOutsidePhase` y `TestBashFrozenReason` DEBEN seguir pasando sin cambios.
  - Pruebas: casos nuevos en `TestBashFrozenWrites`.
