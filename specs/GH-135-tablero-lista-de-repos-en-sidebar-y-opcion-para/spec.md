# GH-135 · Tablero: lista de repos en sidebar y opción para quitar repos de la vista

## Brief

**Objetivo:** que la lista de repos del panel web sea un sidebar vertical ordenado por urgencia, desde donde se puedan ocultar repos sin perder un turno.

**Entra** (solo `internal/cli/ui/index.html`, más pruebas, guía y changelog):
- La tira `<nav id="repos">` pasa a `<aside id="repos">` a la izquierda, con scroll propio; ya no hay tira horizontal.
- Filas con carril, nombre y estado (TÚ / AGENTE / vía libre). Orden: TÚ, AGENTE, vía libre; dentro de cada grupo, por `now.since` más reciente.
- "Quitar" por fila, aviso con "Deshacer" de 6 s, "Ocultos (n)" con "Mostrar", contador en ámbar si un oculto pasa a TÚ.
- Ocultos y colapsado en localStorage (`bflow.hidden-repos`, `bflow.sidebar-collapsed`), siempre con try/catch.
- Colapsar a solo carril (desde 1180px); por debajo de 1180px, panel que se abre con el botón "Repos" de la franja.
- Teclado: todo es `<button>`, foco visible, Escape cierra el panel.

**No entra:** des-registrar repos, reordenar a mano, guardar ocultos en `config.yaml`, endpoints nuevos en el servidor, pruebas en navegador.

**Decisiones nuevas**
- [N] El borde del seleccionado usa `--full` (el azul de marca, el mismo de `--current-row`). Se descarta `--you-ink`, que propuso el discovery, porque es ámbar y se confundiría con TÚ.
- [N] Con menos de 2 repos no hay sidebar, como hoy con la tira. Se descarta mostrarlo con un solo repo: ocultar el único repo no aporta nada.
- [N] La fila es un `<li>` con dos botones (elegir y Quitar). Se descarta el botón único de hoy: un botón no puede llevar otro dentro.
- [N] La etiqueta de estado es solo TÚ / AGENTE / vía libre. "TE TOCA" se va (TÚ en ámbar ya lo dice) y "N abierta(s)" pasa al `title`. Se descarta conservar las cuatro etiquetas.
- [N] El aviso de Deshacer se pausa mientras tiene el cursor o el foco. Tras Quitar, el foco pasa a la fila siguiente. Se descarta el temporizador fijo, que dejaba fuera a quien usa teclado.
- [N] Si todos los repos están ocultos, el tablero muestra "Todos tus repos están ocultos" en lugar del detalle.
- [N] Un repo oculto sigue contando para las notificaciones y para el "Te toca en otro repo" del título de la pestaña.

**Riesgos**
- A 1180x720, el sidebar (`clamp(200px, 16vw, 240px)`) le quita ancho al tablero y a Tokens. Colapsado ocupa unos 64px. Se revisa a mano en el walkthrough.
- Sin pruebas de JS: el orden y los bordes se verifican a mano. Go solo revisa el HTML servido.
- `docs/guia.md` y las specs GH-32 y GH-46 describen la franja de chips. Se actualiza la guía; las specs viejas quedan como registro histórico.

**Tamaño:** M. Un archivo de UI (unas 250 líneas cambiadas), 2 o 3 pruebas Go, guía y changelog.

## Discovery

# Discovery GH-135: sidebar de repos y quitar repos de la vista

## Alcance
- La tira horizontal `<nav id="repos">` de `internal/cli/ui/index.html` se reemplaza por un sidebar vertical a la izquierda, con scroll propio y `position: sticky`. Ya no hay tira horizontal.
- Cada fila: carril (F/L/H), nombre del repo, estado (TÚ / AGENTE / vía libre). El seleccionado se marca con borde azul de marca; los de vía libre van atenuados. TÚ en ámbar.
- Lista única por urgencia (no secciones por perfil). Si hay más de un perfil, el perfil se muestra discreto en la fila.
- Botón "Quitar" por fila (visible al hover/foco; siempre visible en touch). Solo oculta en el tablero; no des-registra nada.
- Al quitar: aviso con "Deshacer" durante 6 segundos.
- Al final del sidebar: "Ocultos (n)", desplegable con cada repo oculto y un botón "Mostrar" que lo regresa a su lugar según urgencia.
- Si un repo oculto pasa a TÚ, el contador "Ocultos" se marca en ámbar.
- Sidebar colapsable a solo iconos/carril; el estado colapsado se recuerda en localStorage (`bflow.sidebar-collapsed`).
- Por debajo de 1180px de ancho (breakpoint existente, no 1100px) el sidebar se vuelve un panel que se abre con un botón.
- Si se oculta el repo que se está viendo, la selección salta al primer repo visible según urgencia y se actualiza `#r=`.
- Todo operable con teclado: foco visible, controles como `<button>` (Enter/Espacio), aria apropiado.

## Datos clave
- "TÚ" = `now.human` del repo (incluye el caso `waiting`). Orden: TÚ > AGENTE > vía libre; dentro de cada grupo, por `now.since` más reciente.
- Ocultos en localStorage clave `bflow.hidden-repos`: arreglo de keys de repo (el hash de 8 hex de `repoKey`, el mismo de `#r=`), no rutas.
- Todo acceso a localStorage envuelto en try/catch: sin almacenamiento, todo se ve y nada falla.
- Se re-ordena con el mismo bucle en vivo (`poll()` → `render()`); respetar `changed(...)` para no repintar sin cambios.

## Restricciones
- Solo HTML/CSS/JS en index.html, sin dependencias nuevas. Servidor sin endpoints nuevos.
- Se mantiene el requisito de GH-46: el panel cabe en una sola pantalla desde 1180x720 con el sidebar; ajustar `TestUIPageOneScreen` según haga falta y actualizar `docs/guia.md` (franja de chips) para describir el sidebar.
- Estilos: tokens y convención de clases actuales (sin prefijo `bf-`). Mapear: brand-blue/you → `--you-ink`, ámbar → `--amber`, line → `--rule`/`--rule-soft`, surface/panel → `--panel`. No crear tokens alias.
- Breakpoints existentes (1180px, 760px).

## Errores y bordes
- localStorage ausente o con JSON inválido: tratar como sin ocultos / expandido, sin lanzar.
- Keys ocultas de repos que ya no existen: ignorarlas en el conteo.
- Todos los repos ocultos: el sidebar muestra solo "Ocultos (n)" y el detalle queda sin repo seleccionado de forma limpia.

## Verificación
- Pruebas Go en `internal/cli/ui_test.go` sobre el HTML servido: sidebar presente, sin tira horizontal, clave `bflow.hidden-repos`, controles como button con aria, una pantalla. Extraer lógica verificable si ayuda. Revisión manual en el walkthrough. Sin herramientas de navegador.
- Toca `internal/cli/` (public_paths): changelog obligatorio.

## Fuera de alcance
- Des-registrar repos desde el tablero; reordenar a mano; mover ocultos a config.yaml.

## Requirements

- R1 [D] CUANDO hay 2 repos o más en `network`, el sistema DEBE mostrar la lista en `<aside id="repos">` a la izquierda del tablero, en vertical y con scroll propio, sin la tira horizontal `.repos` dentro de la franja.
- R2 [N] CUANDO hay menos de 2 repos, el sistema DEBE ocultar el sidebar, y el tablero DEBE ocupar todo el ancho.
- R3 [D] Cada fila DEBE mostrar el carril (insignia F/L/H, o el icono de inactivo si no hay tarea), el nombre del repo y una etiqueta de estado: `TÚ` si `task.now.human`, `AGENTE` si hay tarea y no es humana, `vía libre` si no hay tarea. Si hay más de un perfil, la fila DEBE mostrar el perfil de forma discreta.
- R4 [D] El sistema DEBE ordenar las filas por grupo, TÚ, luego AGENTE y luego vía libre; dentro de cada grupo, por `task.now.since` más reciente primero. Si falta `since`, la fila va al final de su grupo, ordenada por nombre.
- R5 [D] CUANDO el estado es TÚ, la etiqueta DEBE ir en ámbar (`--amber` de fondo). La fila del repo elegido DEBE llevar el borde `--full` y `aria-current="true"`. Las filas en vía libre DEBEN ir atenuadas.
- R6 [D] CUANDO llega un sondeo nuevo de `api/state`, el sistema DEBE reordenar la lista con el mismo `render()`, sin repintar si no cambió nada de lo que muestra (`changed("repos", ...)`), y conservando el foco en la fila que lo tenía.
- R7 [D] CUANDO la persona activa "Quitar" en una fila, el sistema DEBE sacar la fila de la lista, agregar su `key` a `bflow.hidden-repos` y mostrar un aviso `role="status"` con el botón "Deshacer" durante 6 s. El aviso se pausa mientras tiene el cursor o el foco, y el foco pasa a la fila siguiente (o a "Ocultos" si no hay otra).
- R8 [D] CUANDO la persona activa "Deshacer" antes de que venza el aviso, el sistema DEBE quitar la `key` de los ocultos y regresar la fila a su lugar según R4.
- R9 [D] El sistema DEBE mostrar al final del sidebar un botón "Ocultos (n)" con `aria-expanded`, donde n cuenta solo los ocultos que siguen en `network`. Al activarlo, despliega cada repo oculto con su estado y un botón "Mostrar". El botón no aparece con n = 0.
- R10 [D] CUANDO la persona activa "Mostrar", el sistema DEBE quitar la `key` de los ocultos y regresar la fila a su lugar según R4.
- R11 [D] CUANDO al menos un repo oculto está en TÚ, el botón "Ocultos (n)" DEBE marcarse en ámbar.
- R12 [D] CUANDO la página carga, el sistema DEBE leer `bflow.hidden-repos` (arreglo JSON de keys de 8 hex) y `bflow.sidebar-collapsed` ("1" o ausente). Cada lectura y escritura de localStorage DEBE ir en try/catch. Si falla o el valor no es válido, se trata como sin ocultos y sin colapsar, sin lanzar error. Las keys de repos que ya no están se ignoran, sin borrarse.
- R13 [D] CUANDO el repo elegido (`state.key`) está oculto y hay algún repo visible, el sistema DEBE cambiar `#r=` al primer repo visible según R4.
- R14 [D] CUANDO todos los repos están ocultos, el sidebar DEBE mostrar solo "Ocultos (n)". En el lugar del tablero, la línea, los medidores y Tokens, el sistema DEBE mostrar "Todos tus repos están ocultos" con la indicación de usar "Mostrar".
- R15 [D] CUANDO el ancho es de 1180px o más, el botón "Colapsar" DEBE alternar el sidebar entre completo y solo carril, con `aria-expanded`, y guardar el estado en `bflow.sidebar-collapsed`. Colapsado, cada fila muestra solo la insignia (con marca ámbar si está en TÚ) y lleva el nombre en `aria-label` y `title`. "Quitar" no se muestra.
- R16 [D] CUANDO el ancho es menor de 1180px, el sidebar DEBE ser un panel superpuesto, cerrado por defecto, que se abre con el botón "Repos" de la franja (`aria-expanded`, en ámbar si algún repo visible u oculto está en TÚ). Se cierra con Escape, con el botón "Cerrar", al tocar fuera o al elegir un repo. Al abrirse, el foco va a la primera fila; al cerrarse, regresa al botón "Repos".
- R17 [D] El sidebar DEBE quedar fijo (`position: sticky; top: 0`, alto máximo `100dvh`) mientras la página se desplaza. En el modo de una sola pantalla (desde 1180x720) solo se desplaza la lista.
- R18 [D] La página DEBE seguir cabiendo en una sola pantalla desde 1180x720, con el sidebar incluido (lo que pide GH-46).
- R19 [D] Todos los controles DEBEN ser `<button type="button">`, operables con Enter o Espacio y con foco visible (`:focus-visible`). "Quitar" y "Mostrar" llevan `aria-label` con el nombre del repo. "Quitar" se ve al pasar el cursor o con foco dentro de la fila, y siempre en `(hover: none)`.
- R20 [N] Un repo oculto DEBE seguir disparando las notificaciones (`maybeNotify`) y el "Te toca en otro repo" del título.
- R21 [D] `docs/guia.md` DEBE describir el sidebar en lugar de la franja de chips, y `CHANGELOG.md` DEBE tener la entrada del cambio.

## Design

### HTML (estructura nueva)

```html
<header class="strip"> ... <button class="btn frame" id="repos-open" type="button" aria-controls="repos" aria-expanded="false" hidden>Repos</button> ... </header>
<div class="shell" id="shell">
  <aside class="side frame" id="repos" aria-label="Tus repos" hidden>
    <div class="side-head">
      <button class="btn" id="repos-collapse" type="button" aria-controls="repos" aria-expanded="true" aria-label="Colapsar la lista de repos">«</button>
      <button class="btn" id="repos-close" type="button" aria-label="Cerrar la lista de repos">Cerrar</button>
    </div>
    <ul class="side-list" id="repo-list"></ul>
    <div class="side-hidden">
      <button class="btn" id="hidden-toggle" type="button" aria-expanded="false" aria-controls="hidden-list" hidden>Ocultos (0)</button>
      <ul id="hidden-list" hidden></ul>
    </div>
  </aside>
  <div class="layout no-tok" id="layout"> ...igual que hoy... <p class="empty" id="all-hidden" hidden>…</p></div>
</div>
<div class="undo frame" id="undo" role="status" aria-live="polite" hidden><span id="undo-msg"></span> <button class="btn" id="undo-btn" type="button">Deshacer</button></div>
<div class="scrim" id="scrim" hidden></div>
```

Fila visible (`#repo-list > li.repo`, clases extra `wait` si TÚ e `idle` si vía libre):
`<li class="repo" data-key="ab12cd34"><button class="pick" type="button" aria-current="true" title="…">[badge] <span class="rn">api</span> <span class="rp">perfil</span> <span class="rw human-c">TÚ</span> <span class="more">+2</span> <span class="err">…</span></button><button class="drop" type="button" aria-label="Quitar api de la vista">Quitar</button></li>`.

Fila oculta (`#hidden-list > li`): `<span class="rn">` + `<span class="rw">` + `<button class="show" type="button" aria-label="Mostrar api">Mostrar</button>`.

### CSS
- Clases sin prefijo `bf-` (la convención del archivo): `.shell`, `.side`, `.side-head`, `.side-list`, `.repo`, `.pick`, `.drop`, `.side-hidden`, `.undo`, `.scrim`, `.empty`. `.chip` y `.repos` desaparecen. Se reutilizan `.badge`, `.rn`, `.rw`, `.more`, `.err` y `.frame`.
- Tokens existentes, sin alias: fondo `--panel`, divisores `--rule-soft`, seleccionado `--f: var(--full)` con `--current-row`, TÚ `--amber` sobre `--led-face`, vía libre `color: var(--ink-3)` y `opacity` en la insignia.
- `.shell`: `display: grid; grid-template-columns: minmax(0,1fr)` por defecto; desde 1180px `grid-template-columns: clamp(200px,16vw,240px) minmax(0,1fr)`, y con `.shell.collapsed` `64px minmax(0,1fr)`. Sin sidebar (`#repos[hidden]`), una sola columna.
- `.side`: `position: sticky; top: 0; max-height: 100dvh; display: flex; flex-direction: column`, y `.side-list { overflow-y: auto; min-height: 0 }`. En una sola pantalla (`min-width:1180px and min-height:720px`), `body` tiene `grid-template-rows: auto minmax(0,1fr)` y `.shell` ocupa la segunda fila con `min-height: 0`.
- Por debajo de 1180px: `.side { position: fixed; inset: 0 auto 0 0; width: min(300px, 86vw); transform: translateX(-105%); z-index: 20 }`, y `.side.open { transform: none }`. `#repos-open` se muestra y `#repos-collapse` se oculta. Desde 1180px, al revés, y `#repos-close` oculto.
- `.drop { visibility: hidden }`, visible con `.repo:hover .drop, .repo:focus-within .drop` y siempre en `@media (hover: none)`. `.collapsed .drop, .collapsed .rn, .collapsed .rw, .collapsed .rp, .collapsed .more { display: none }`.
- `prefers-reduced-motion`: sin transición en `.side` ni `.undo`.

### JS (nombres exactos)
- `const HIDDEN_KEY = "bflow.hidden-repos", COLLAPSED_KEY = "bflow.sidebar-collapsed", UNDO_MS = 6000;`
- `loadHidden(): Set<string>` lee y valida (arreglo cuyos elementos casan con `/^[0-9a-f]{8}$/`); con cualquier error devuelve `new Set()`. `saveHidden(set)` escribe `JSON.stringify([...set])` en try/catch.
- `repoRank(r): 0|1|2`: 0 si `r.task && r.task.now && r.task.now.human`, 1 si hay `r.task`, 2 si no hay.
- `sortRepos(all)`: rank ascendente, luego `since` descendente (sin `since` al final), luego `name`.
- `hideRepo(key)`, `showRepo(key)`, `undoHide()`: actualizan el `Set` `hidden`, guardan, ponen `lastSig.repos = ""` y llaman a `renderRepos()` y a `ensureVisibleSelection()`.
- `ensureVisibleSelection()`: si `state.key` está oculto y hay visibles, hace `location.hash = "r=" + first.key`. Pone `document.body.classList.toggle("all-hidden", visibles === 0 && total >= 2)`.
- `setCollapsed(bool)`, `openDrawer()` y `closeDrawer()`: escuchan `keydown` Escape y el clic en `#scrim`.
- `renderRepos()` se reescribe: la firma de `changed("repos", …)` incluye `nw`, `state.key`, `state.others`, `[...hidden]`, `collapsed` y si `#hidden-list` está abierto. Con `profiles > 1` muestra `.rp`.
- El aviso: `showUndo(name, key)` pone el texto "Quitaste <nombre> de la vista." y arranca un `setTimeout` de `UNDO_MS`, que se pausa con `mouseenter` y `focusin` y se reanuda con `mouseleave` y `focusout`. Al vencer se oculta; queda solo el último quitado.
- `maybeNotify()` y el `waitingElsewhere` de `render()` no filtran ocultos (R20). El botón "Repos" y "Ocultos" calculan el ámbar con `repoRank === 0`.

### Decisiones descartadas
- Secciones por perfil en el sidebar: el discovery pidió una sola lista por urgencia.
- Breakpoint de 1100px: se usa el de 1180px que ya existe.
- Guardar los ocultos por ruta: la key de 8 hex de `repoKey` es estable por ruta y no expone la ruta en localStorage.
- Usar `<dialog>` para el panel angosto: obliga a mover el `aside` entre dos contenedores según el ancho. Basta con una clase y `aria-expanded`.
- Borrar del localStorage las keys de repos que ya no están: un repo puede volver a aparecer en `repos.json`.

### Seguridad
- Sin servidor nuevo ni authz nueva: la CSP y la validación de `Host` de `uiHandler` no cambian.
- Los datos de localStorage se validan contra `/^[0-9a-f]{8}$/` antes de usarse. Nunca se insertan como HTML: todo va por `el()`/`textContent`, y los selectores usan `CSS.escape`.
- Nombres de repo, perfil y errores se escriben solo con `textContent`, como hoy.
- No se guardan rutas ni datos sensibles en el navegador, solo las keys hash.

## Tasks

- [x] T1 Contrato. Escribir las pruebas en `internal/cli/ui_test.go` (fallan hasta T2-T4) y fijar en `index.html` los ids y nombres del Design.
  - `TestUIPageSidebar`: la página tiene `<aside class="side`, `id="repos"`, `id="repo-list"`, `id="repos-collapse"`, `id="repos-open"`, `id="repos-close"`, `bflow.sidebar-collapsed`, `position: sticky` y `(hover: none)`. No tiene `<nav class="repos"` ni `class="chip`. (R1, R15, R16, R17, R19)
  - `TestUIPageHiddenRepos`: tiene `bflow.hidden-repos`, `id="hidden-toggle"` con `aria-expanded`, `id="hidden-list"`, `id="undo"` con `role="status"`, `Deshacer`, `Mostrar`, `Quitar`, `UNDO_MS = 6000`, `id="all-hidden"` y las funciones `loadHidden`, `saveHidden`, `repoRank`, `sortRepos`, `ensureVisibleSelection`. (R7-R14)
  - `TestUIPageStorageGuarded`: toda línea de la página con `localStorage.` contiene `try {`. (R12)
  - `TestUIPageOneScreen`: se conserva y además exige `id="shell"`. (R18)
- [x] T2 Sidebar y orden. Mover la lista a `<aside id="repos">` dentro de `.shell`, con filas `li.repo` (botón `pick`), `repoRank` y `sortRepos`, etiquetas TÚ / AGENTE / vía libre, ámbar, borde `--full`, atenuado, perfil discreto, sticky con scroll propio, una sola pantalla a 1180x720, y sin sidebar con menos de 2 repos. (R1-R6, R17, R18)
- [x] T3 Quitar, Deshacer y Ocultos. `HIDDEN_KEY`, `loadHidden`, `saveHidden`, botón `drop`, aviso `#undo` con pausa, `#hidden-toggle` y `#hidden-list` con "Mostrar", ámbar en Ocultos, `ensureVisibleSelection`, estado `all-hidden`, foco tras Quitar, y notificaciones sin filtrar ocultos. (R7-R14, R19, R20)
- [x] T4 Colapsar y panel angosto. `setCollapsed` con `COLLAPSED_KEY`, `openDrawer` y `closeDrawer` con Escape, scrim y foco, botón "Repos" en la franja con ámbar, y `prefers-reduced-motion`. (R15, R16, R19)
- [x] T5 Documentación. Cambiar en `docs/guia.md` (línea ~126) la franja de chips por el sidebar, Quitar/Ocultos y el panel angosto, y agregar en `CHANGELOG.md` una entrada en "Cambiado" (la lista de repos ahora es un sidebar; se pueden ocultar repos sin perder avisos). (R21)
