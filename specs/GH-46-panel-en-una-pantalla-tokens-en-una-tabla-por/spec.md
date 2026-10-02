# GH-46 · Panel en una pantalla: tokens en una tabla por agente y fase, detalle por llamada aparte

## Brief

**Objetivo**: que el panel web quepa en una pantalla (>= 1180px de ancho) con Tokens como una sola tabla por agente y fase, y que el detalle por llamada se abra en una ventana aparte.

**Entra**
- Layout de alto fijo (`100dvh`): a la izquierda Tablero, Línea y, abajo, Tiempo y Bitácora lado a lado; a la derecha, Tokens a todo el alto.
- Franja superior con versión y enlace (se va el pie) y "Tus repos" como fila de chips.
- Tabla de Tokens: una fila por agente (barra, llamadas, nuevo) y debajo una por corrida, nombrada por su fase, con llamadas, ctx final, releído y nuevo alineados. El plegado ("+N corridas") evita que la tarjeta crezca.
- Ventana (`<dialog>`) con la tabla por llamada de GH-29; Esc, botón o clic fuera la cierran.
- `bflow stats <ID> --calls` con la misma etiqueta "agente · fase".

**No entra**: el formato de calls.jsonl, el texto de `bflow watch` en terminal y el layout de menos de 1180px (se apila como hoy). Tampoco pruebas automáticas en navegador: el ajuste a la pantalla se verifica a mano (T6).

**Decisiones del discovery**: la etiqueta es la fase; si la corrida cruza fases, "primera → última". El `#N` solo aparece si se repiten agente y etiqueta. Al plegar, la corrida más reciente queda abierta y las demás se pliegan de la más vieja a la más nueva. Los chips van en una fila con scroll horizontal y los que dicen TE TOCA van primero. La ventana flota encima y no mueve nada.

**Decisiones nuevas**
- [N1] La etiqueta se calcula una vez en `metrics.Runs` (`Name`, `Stage`, `Label`) y la usan el CLI y el panel. Descartado: armarla por separado en el JS y en el CLI.
- [N2] Las filas de agente salen de `st.Agents` (cuadran con el total y conservan "sin desglose"), con las corridas anidadas. Un agente que solo aparece en calls.jsonl recibe su fila con la suma de sus corridas. Descartado: salir solo de las corridas, porque el gasto anterior a GH-29 desaparecería.
- [N3] La página solo deja de hacer scroll con >= 1180px de ancho y >= 720px de alto. Con menos alto se conservan las dos columnas y hay scroll. Descartado: recortar el contenido en ventanas bajas.
- [N4] La ventana es un `<dialog>` nativo con `showModal()`: Esc, foco atrapado y capa superior vienen del navegador. Descartado: un div hecho a mano.
- [N5] La ventana abierta se actualiza con cada sondeo (cada 2 s) y conserva su scroll. Si la corrida desaparece, se cierra.
- [N6] El pie se va: la marca queda en el lockup, el repo en `where`, y versión y enlace pasan a la franja. Las otras tareas del repo actual, que hoy salen bajo su botón, pasan al chip como "+N" con la lista en `title`.

**Riesgos**
- No hay pruebas de navegador. "Cabe en 1440x900" depende del alto de Tablero y Línea; si no cabe, T3 compacta márgenes. Se revisa a mano con GH-15 (251 llamadas, 10 corridas).
- El plegado mide el DOM: si hay un error, la tarjeta recorta filas. Como respaldo, la tabla hace scroll dentro de su caja.
- El JSON de `stats --calls` cambia `label` y agrega `name` y `stage` (aditivo, salvo el texto de `label`).

**Tamaño**: M. Unas 60 líneas de Go (metrics, cli) y unas 250 de HTML, CSS y JS en index.html, en 6 tareas.

## Discovery

Alcance: el del issue completo. `Run.Phase` ya existe y cada `CallRow` trae su fase, así que calls.jsonl no cambia. La etiqueta es la fase (o "primera → última") y lleva `#N` solo si se repite. CLI y panel usan la misma función. Plegado: la corrida más reciente queda abierta y, si aun así no cabe, la tabla hace scroll dentro de la tarjeta. Los chips van en una fila con scroll horizontal y TE TOCA primero. La ventana es fija sobre el panel. La Bitácora muestra los eventos que quepan. Por debajo de 1180px el panel queda como hoy.

## Requirements

Etiquetas (metrics, CLI)
- R1 [D] CUANDO `metrics.Runs` arma una corrida, el sistema DEBE ponerle `Stage` igual a su fase si todas sus filas son de una sola fase. Si hay varias, `Stage` es "<primera> → <última>" en el orden de las filas; si la primera y la última coinciden, solo esa fase. Si no tiene fase, `Stage` es "sin fase".
- R2 [D] CUANDO dos o más corridas tienen el mismo `Name` y el mismo `Stage`, el sistema DEBE agregar " #N" al `Stage` de cada una, numerado por su primera llamada. Si no se repiten, no lleva número.
- R3 [N] El sistema DEBE dar a cada corrida `Name` ("sesión principal" para `MainSession`; si no, el agente) y `Label` = "<Name> · <Stage>".
- R4 [D] CUANDO se corre `bflow stats <ID> --calls`, el encabezado de cada corrida DEBE ser "<Label> · <resumen>", por ejemplo "implementer · contract · 9 llamadas · contexto final ...". La salida JSON DEBE traer `name`, `stage` y `label` en cada corrida.

Datos del panel
- R5 [N] CUANDO hay tokens, `panelTask.Agents` DEBE traer una fila por agente de `st.Agents`, ordenadas por gasto nuevo de mayor a menor, cada una con nombre, `share` (0-100 del nuevo total), llamadas y nuevo (`metrics.Human`). También DEBE traer una fila por cada agente que solo esté en calls.jsonl, con la suma de sus corridas.
- R6 [D] Cada fila de agente DEBE anidar sus corridas en orden de primera llamada, cada una con `key`, `stage`, `label`, llamadas, ctx final, releído y nuevo en formato corto de un decimal (GH-11), más `rows` y `total` exactos (`callCells` y `callTotal` de GH-29).
- R7 [D] El sistema DEBE marcar `latest` en el agente de la corrida con la llamada más reciente y dar `last` (hora de su última llamada) a cada agente con corridas.
- R8 [D] `calls_note` DEBE seguir saliendo igual que hoy.

Layout (>= 1180px)
- R9 [D] CUANDO la ventana mide >= 1180px de ancho y >= 720px de alto [N3], la página DEBE ocupar `100dvh` sin scroll de página. A la izquierda van Tablero, Línea y una fila con Tiempo y Bitácora lado a lado; a la derecha, Tokens a todo el alto.
- R10 [N] CUANDO mide >= 1180px de ancho y < 720px de alto, el sistema DEBE conservar las dos columnas y permitir scroll de página.
- R11 [D] CUANDO mide < 1180px de ancho, el sistema DEBE apilar las cajas como hoy, con scroll.
- R12 [D] La franja superior DEBE mostrar marca, repo, versión, enlace (con el mismo `aria-live`), tema y aviso. No DEBE haber pie.
- R13 [D] CUANDO hay 2 o más repos, la franja DEBE mostrar una fila de chips (badge del carril, nombre, quién sigue) con scroll horizontal y alto fijo. Los chips con TE TOCA van primero, resaltados como hoy, y el repo actual lleva `aria-current`. Un clic o Enter cambia de repo como hoy.
- R14 [N] El chip del repo actual DEBE mostrar "+N" cuando hay otras tareas abiertas en ese repo, con la lista ("ID · fase · sigue") en su `title` [N6].
- R15 [D] La Bitácora DEBE mostrar solo los eventos que caben completos en su caja, los más recientes primero, sin estirar la página.

Tokens
- R16 [D] La tarjeta Tokens DEBE ser una sola tabla con la línea total arriba, columnas fijas alineadas a la derecha (llamadas, ctx final, releído, nuevo) y `calls_note` en una línea al pie. No DEBE tener barras sueltas ni `<details>`.
- R17 [D] CUANDO las filas no caben en la caja, el sistema DEBE plegar en su fila de agente las corridas de los agentes que no son `latest`, de `last` más antiguo a más nuevo, hasta que quepan. Un agente plegado muestra un botón "+N corridas" que lo despliega; uno desplegado, un botón "plegar".
- R18 [D] CUANDO la persona despliega o pliega un agente, el sistema DEBE respetar esa elección en los siguientes refrescos y al cambiar el tamaño de la ventana. Si al desplegar no cabe, la tabla hace scroll dentro de la tarjeta. La tarjeta nunca crece más que su caja.
- R19 [N] CUANDO la tabla se vuelve a dibujar, el foco DEBE volver al elemento con la misma clave (fila de corrida o botón de agente).

Detalle por llamada
- R20 [D] CUANDO la persona hace clic o pulsa Enter en una fila de corrida, el sistema DEBE abrir un `<dialog>` modal con título `label` y resumen, y la tabla por llamada con las columnas de R7 de GH-29, cifras exactas y fila de totales.
- R21 [D] CUANDO la ventana está abierta, Esc, el botón "Cerrar" o un clic fuera de su contenido DEBEN cerrarla, y el foco DEBE volver a la fila de origen (buscada por `key` si se volvió a dibujar).
- R22 [D] Abrir o cerrar la ventana NO DEBE cambiar el tamaño de ninguna caja.
- R23 [N] CUANDO llega un sondeo con la ventana abierta, el sistema DEBE actualizar su tabla conservando el scroll y cerrarla si la corrida ya no existe [N5].

Transversal
- R24 [D] Temas Blanco y Marino, sin emojis, texto en color y uso por teclado, como hoy.

## Design

### metrics (internal/metrics/calls.go)
```go
type Run struct {
    Run          string     `json:"run"`
    Agent        string     `json:"agent"`
    Name         string     `json:"name"`  // "sesión principal" | agente           (R3)
    Stage        string     `json:"stage"` // "contract", "discovery → spec", "spec #2" (R1, R2)
    Label        string     `json:"label"` // Name + " · " + Stage                    (R3)
    Phase        flow.Phase `json:"phase"` // de la primera fila (sin cambio)
    Rows, Total, FinalContext, Last          // sin cambio
}
// stageOf: fase única, "primera → última" o "sin fase" (R1).
func stageOf(rows []CallRow) string
```
`Runs` reemplaza el bloque de numeración actual (que cuenta por agente) por uno que cuenta por `Name+"\x00"+Stage` (R2). El separador "→" es U+2192, el mismo que convierte `withArrows` en el panel.

### CLI (internal/cli/calls.go)
`renderCalls`: `fmt.Fprintf(&b, "\n%s · %s\n", r.Label, runSummary(r))` (R4). La salida JSON ya serializa `Run`, así que los campos nuevos salen solos.

### Panel (internal/cli/ui.go)
```go
// panelAgent es una fila de agente de la tabla de Tokens (R5, R7).
type panelAgent struct {
    Key    string     `json:"key"`   // agente crudo ("main", "implementer", "")
    Name   string     `json:"name"`  // agentName(Key)
    Share  int        `json:"share"` // 0-100 del nuevo total
    Calls  int64      `json:"calls"` // 0 = sin dato (el JS deja la celda vacía)
    New    string     `json:"new"`   // metrics.Human
    Latest bool       `json:"latest,omitempty"`
    Last   *time.Time `json:"last,omitempty"`
    Runs   []panelRun `json:"runs,omitempty"`
}
type panelRun struct { // reemplaza al de GH-29 (se van Summary y Open)
    Key     string     `json:"key"`
    Stage   string     `json:"stage"`
    Label   string     `json:"label"`
    Summary string     `json:"summary"` // runSummary, para el encabezado de la ventana
    Calls   int64      `json:"calls"`
    Context string     `json:"context"` // Human(FinalContext)
    Read    string     `json:"read"`    // Human(Total.CacheRead)
    New     string     `json:"new"`     // Human(Total.New())
    Rows    [][]string `json:"rows"`
    Total   []string   `json:"total"`
}
```
`panelTask.Agents` cambia a `[]panelAgent` y se quita `panelTask.Runs`; `CallsNote` sigue igual. Una función `tokenTable(st metrics.TaskStats, runs []metrics.Run) []panelAgent` arma la tabla con `byNew`, `agentName`, `callCells` y `callTotal`.

### index.html
- Distribución con `@media (min-width:1180px)`: `.layout` toma `grid-template-columns: minmax(0,1fr) 440px` y las áreas `"board net" "line net" "gauges net"` (`net` es ahora Tokens). `#gauges` pasa a ser una rejilla de dos columnas: Tiempo y Bitácora.
- Con `@media (min-width:1180px) and (min-height:720px)`: `body` es una rejilla de `100dvh` con `overflow:hidden`, la franja va en `auto` y `.layout` en `1fr`; las filas de `.layout` son `auto auto minmax(0,1fr)`. Bitácora y Tokens llevan `min-height:0; overflow:hidden`.
- Con `.layout.solo` (un solo repo) la distribución es la misma; ya no depende de la red.
- Franja: lockup, `where`, `#version`, `#conn` (`aria-live="polite"`), tema y aviso en una fila. Debajo, `<nav id="repos" aria-label="Tus repos">` con `overflow-x:auto` y alto fijo. Se borran `<aside id="net">`, `<footer id="foot">` y `#repo`. `renderNet` pasa a ser `renderRepos`: botones `.chip` con badge, nombre y quién sigue, ordenados con los `waiting` primero (orden estable); el perfil va en `title`.
- Bitácora: después de dibujar, y en `resize`, quita los `li` cuyo `offsetTop + offsetHeight` pasa el alto del `ol` (R15).
- Tokens: `<table class="tok">` con `<colgroup>` (nombre, llam, ctx, rel, nvo) y celdas numéricas `text-align:right`. La fila de agente lleva el nombre, `blocks(10, share/10)` y el botón "+N corridas" o "plegar". La fila de corrida lleva `tabindex=0`, `aria-haspopup="dialog"`, `data-key` y la etapa con sangría. Su contenedor `.tok-wrap` tiene `overflow-y:auto` como respaldo (R18).
- Plegado: `fold()` corre después de dibujar y en `resize`. Con `agentFold = new Map()` (clave de agente -> elección de la persona) parte de lo elegido; el resto se despliega. Mientras `wrap.scrollHeight > wrap.clientHeight`, pliega el siguiente candidato (sin elección, no `latest`, con corridas, por `last` ascendente). Reemplaza a `runOpen`.
- Ventana: `<dialog id="calls" aria-labelledby="calls-h">` con `h2#calls-h`, el resumen, `.calls-wrap > table.calls` (la misma tabla de hoy) y `<button>Cerrar</button>`. Se abre con `showModal()`. El clic en el `dialog` fuera de su caja interior cierra; el evento `close` devuelve el foco a `[data-key=…]`. `render()` llama a `refreshDialog()` (R23). Dentro del `dialog`, `max-height:85dvh` y scroll propio. `::backdrop` usa colores del tema.

### Seguridad
Sin superficie nueva: no hay endpoints ni entradas nuevas y todo el texto entra con `textContent` (`el`). `data-key` se busca con `CSS.escape`. La clave del repo no cambia de origen (hash).

### Descartado
Mantener el pie, porque come alto. Barras más corridas, porque cuentan dos veces. Ventana con `<details>`, porque estira la caja. Plegado en el servidor, porque no conoce el alto. Columna lateral para los repos, porque quita ancho a Tokens.

## Tasks

- [ ] **T1 Contrato** (R1-R8, R20-R23): campos `Name` y `Stage` en `metrics.Run`, `stageOf` sin lógica, `panelAgent`, el nuevo `panelRun` y la firma de `tokenTable`. Estas pruebas deben fallar:
  - metrics: `TestRunsLabels` (reescrita: una fase, varias fases "a → b", "sin fase", `#N` solo si se repiten nombre y etapa, `Label`)
  - cli: `TestStatsCallsText` (encabezado "<Label> · <resumen>", con una corrida repetida "spec #2"), `TestStatsCallsJSON` (`name`, `stage`, `label`), `TestPanelTokensTable` (reemplaza a `TestPanelRuns`: orden por nuevo, `share`, llamadas, Human, corridas anidadas en orden, `latest` y `last`, filas exactas y total, agente sin corridas, agente que solo está en calls.jsonl, `calls_note`)
  - ui: `TestUIPageOneScreen` (index.html tiene `100dvh`, `<dialog id="calls"`, `id="repos"`; no tiene `<details`, `id="foot"` ni `id="net"`)
- [ ] **T2 Etiquetas** (R1-R4): `stageOf`, la numeración nueva en `Runs` y el encabezado de `renderCalls`. Pasan `TestRunsLabels`, `TestStatsCalls*` y el resto de metrics.
- [ ] **T3 Distribución y franja** (R9-R15, R24): CSS de dos columnas, alto fijo con la consulta de alto, franja con versión y enlace, chips de repos (`renderRepos`, TE TOCA primero, "+N" con `title`) y Bitácora recortada. Se borran el pie y `#net`. Tokens sigue con su contenido actual en la columna derecha. Pasa la parte de `TestUIPageOneScreen` sobre el pie y los repos.
- [ ] **T4 Tabla de Tokens** (R5-R8, R16-R19): `tokenTable` en `buildPanel` y la tabla `.tok` con `fold()`, `agentFold` y la vuelta del foco. Se va el `<details>` y `runOpen`. Pasa `TestPanelTokensTable`.
- [ ] **T5 Ventana de detalle** (R20-R23): `<dialog id="calls">`, apertura con clic o Enter, cierre con Esc, botón o clic fuera, foco de vuelta y `refreshDialog`. Pasa `TestUIPageOneScreen` completa.
- [ ] **T6 Docs y verificación** (R4, R9, R22, R24): `docs/guia.md` (encabezado "agente · fase" y "el panel abre cada corrida en una ventana desde Tokens") y `CHANGELOG.md`. Verificación a mano con `bflow ui` sobre GH-15, en 1920x1080, 1440x900 y 1180x700, en los dos temas: sin scroll de página, ctx en una sola columna, la ventana no mueve cajas y todo funciona solo con teclado.
