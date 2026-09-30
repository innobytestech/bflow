# LOCAL-1 · Tracker de GitHub Issues y Projects

## Brief

**Objetivo:** que bflow lleve sus tareas en issues de GitHub (`tracker.adapter: github`) y, si se configura, en la columna Status de un Project v2.

**Entra**
- Adaptador `internal/adapters/tracker/github`: Get, List, Transition, Comment(s), Create, `tracker setup`, `tracker states`, lista de projects para `init`.
- Sin project, la fase es la etiqueta `bflow:<fase>`. Con project, es la opción de Status (nombres literales, `tracker.states` los sobrescribe).
- Al llegar a done se cierra el issue (si sigue abierto) y el PR lleva `Closes #N`.
- `doctor`, `init` y `bflow.yaml` (`prefix`, `start_field`) conocen el adaptador.
- La tabla de nombres por defecto sale de Plane a `internal/tracker` y los dos adaptadores la comparten.

**No entra:** mudar las tareas locales a issues, intake, `bflow new`, crear el Project desde bflow, cambiar el bflow.yaml de este repo (va en otro commit, después del merge y del `go install`).

**Decisiones nuevas**
- [N] `tracker setup` con project le pregunta a la API (introspección GraphQL) si la entrada de opciones acepta `id`. Si lo acepta, agrega las opciones que falten reenviando las existentes con su id. Si no, no escribe nada y lista lo que falta. Descartado: decidirlo una vez leyendo la documentación, porque GitHub Enterprise puede ir atrasado.
- [N] Sin project no hay fecha de inicio: `StampStart` no hace nada y la suite de contrato gana `Harness.NoStartDate`. Descartado: guardar la fecha en el cuerpo del issue o en un comentario.
- [N] bflow no reabre issues. Si un issue cerrado pasa a otra fase, cambia la etiqueta o el Status pero sigue cerrado. Descartado: reabrirlo, porque desharía cierres hechos por personas.
- [N] Un issue cerrado sin etiqueta `bflow:*` se lee como done. Con varias etiquetas `bflow:*`, gana la fase más avanzada.
- [N] `tracker.adapter: github` exige `vcs.host: github`. De ahí salen el repo, `vcs.api_url` y el token.
- [N] Nueva capacidad opcional `tracker.PRLinker` (`CloseRef`): el PR escribe `Closes #N` sin que el núcleo sepa de GitHub. Descartado: un `if adapter == "github"` en engine.
- [N] Las etiquetas se ponen con POST y las otras `bflow:*` se quitan con DELETE una por una. Descartado: PUT de todas las etiquetas, porque pisaría las que alguien agregue a la vez.

**Ratifica (defaults del discovery 7-13):** etiquetas con los colores de Plane; owner resuelto primero como organización y luego como usuario; caché en `.bflow/cache/github.json`; `GH` por defecto; el mapa de errores 401/403/404/límite; los chequeos de doctor; pruebas sobre httptest, sin red en CI.

**Riesgos**
- Si la introspección dice que no, `tracker setup` queda manual con project. Mitigación: lista exacta de opciones y colores para crearlas a mano.
- Los tokens fine-grained no ven los Projects de una organización sin el permiso de organización. doctor lo diagnostica con el tipo de token que corresponde.
- El fake de GraphQL puede alejarse de la API real. Mitigación: prueba manual en un repo de prueba antes del walkthrough.

**Tamaño:** L. Unas 900 líneas de Go más unas 600 de pruebas en 8 tareas. Un solo adaptador; no conviene dividirlo.

## Discovery

Decidido con el humano: List abarca los issues abiertos del repo (sin PRs; sin etiqueta = backlog) o, con project, solo los de este repo que están en el project. Get acepta cualquier issue y, al empezar, se agrega al project. En done se cierra el issue y el PR mantiene `Closes #N`. Los nombres de Status son la tabla de Plane, compartida. `setup` solo escribe si conserva los ids de las opciones. La fecha de inicio va en `tracker.start_field` ("Start date"), y si ese campo no existe no es error. La adopción en este repo va en otro commit. Los defaults 7-13 del discovery se ratifican en el brief.

## Requirements

**Configuración y wiring**
- R1 [D] CUANDO `tracker.adapter` es `github`, el sistema DEBE construir el adaptador con el repo de `vcs.repo` (o el que se deduce del remoto), la API de `vcs.api_url` (por defecto `https://api.github.com`) y el token de `bflow connect github` (`GH_TOKEN`/`GITHUB_TOKEN`, luego el llavero `github:<host>`).
- R2 [N] CUANDO `tracker.adapter` es `github`, `Validate` DEBE rechazar: `vcs.host` distinto de `github`, `tracker.project` que no cumpla `^[A-Za-z0-9][A-Za-z0-9-]*/[1-9][0-9]*$` y `tracker.prefix` que no cumpla `^[A-Za-z][A-Za-z0-9]*$`. `project` es opcional.
- R3 [D] El prefijo DEBE ser `tracker.prefix` en mayúsculas, o `GH` si falta. `start_field` DEBE ser "Start date" si falta.

**Identificadores**
- R4 [D] CUANDO se pide `<PREFIJO>-N`, el sistema DEBE leer el issue #N del repo. CUANDO el prefijo no coincide, el ID está mal formado, #N no existe o #N es un PR, DEBE devolver un error que envuelva `tracker.ErrNotFound`.

**Lectura**
- R5 [D] Sin project, CUANDO se lee un issue, la fase DEBE salir de su etiqueta `bflow:<fase>`. Un issue abierto sin etiqueta `bflow:*` DEBE leerse como backlog y uno cerrado sin etiqueta, como done [N]. Con varias etiquetas, DEBE ganar la fase más avanzada según `tracker.PhaseOrder` [N].
- R6 [D] Con project, CUANDO se lee un issue, la fase DEBE salir del nombre de la opción de Status, traducido con la tabla compartida (que también acepta Todo, In Progress y Done). CUANDO el issue no está en el project, DEBE leerse como backlog con `State` vacío.
- R7 [D] Sin project, `List(OpenOnly)` DEBE devolver todos los issues abiertos del repo, sin PRs, recorriendo todas las páginas. `List` sin `OpenOnly` DEBE incluir los cerrados.
- R8 [D] Con project, `List` DEBE devolver solo los items del project cuyo contenido es un issue de este repo. Excluye borradores, PRs y otros repos. Con `OpenOnly`, excluye los issues cerrados.
- R9 [D] `Task.Closed` DEBE ser el estado del issue (cerrado o no). `Task.URL` DEBE ser el `html_url` del issue. `Task.Start` DEBE salir del campo `start_field` del item y ser nil sin project.

**Escritura**
- R10 [D] Sin project, CUANDO se transiciona a una fase, el sistema DEBE agregar la etiqueta `bflow:<fase>` y quitar cualquier otra `bflow:*` del issue. Las demás etiquetas no se tocan.
- R11 [D] Con project, CUANDO se transiciona, el sistema DEBE agregar el issue al project si no está (idempotente) y poner en Status la opción de la fase (el primer nombre que exista en el project). CUANDO no hay opción para la fase, DEBE fallar con un error que nombre la fase y sugiera `bflow tracker setup` o `tracker.states`.
- R12 [D] CUANDO se transiciona a done y el issue sigue abierto, el sistema DEBE cerrarlo con `state_reason: completed`. Si ya está cerrado, no DEBE volver a hacer PATCH. Ningún otro destino DEBE reabrir un issue cerrado [N].
- R13 [D] Con project, CUANDO `Patch.StampStart` no es cero, el item no tiene fecha en `start_field` y el project tiene un campo de fecha con ese nombre, el sistema DEBE sellar esa fecha. Si el campo no existe o ya tiene valor, no DEBE hacer nada ni fallar. Nunca DEBE escribir otro campo de fecha.
- R14 [D] `Comment` DEBE publicar el Markdown tal cual y `Comments` DEBE devolver el cuerpo tal cual, sin conversión HTML, con todas las páginas.
- R15 [D] `Create` DEBE crear el issue con título y cuerpo y dejarlo en backlog: con la etiqueta `bflow:backlog`, o dentro del project con Status de backlog.
- R16 [D] CUANDO `PRBody` arma la descripción y el tracker implementa `tracker.PRLinker` con un `CloseRef` no vacío, el cuerpo DEBE incluir esa línea (`Closes #N`) antes del pie.

**Setup, estados y projects**
- R17 [D] Sin project, `EnsureStates` DEBE crear las etiquetas `bflow:<fase>` que falten (las 12 fases de `tracker.PhaseOrder`) con el color de la tabla y devolver sus nombres. Con `dryRun` DEBE solo listarlas.
- R18 [D] Con project, `EnsureStates` DEBE calcular las opciones de Status que faltan (el primer nombre de cada fase sin opción). Si la introspección de `ProjectV2SingleSelectFieldOptionInput` incluye `id`, DEBE llamar `updateProjectV2Field` con las opciones existentes (con su id, nombre, color y descripción) más las nuevas, y verificar en la respuesta que los ids de las existentes no cambiaron. Si la introspección no incluye `id`, NO DEBE escribir y DEBE devolver `*tracker.ManualSetupError` con la lista de opciones y colores que faltan [N]. Con `dryRun`, DEBE solo listar.
- R19 [D] `StateMap` DEBE listar las etiquetas `bflow:*` del repo (grupo `label`), o las opciones de Status (grupo `status`), con la fase que se lee de cada una y las fases que escriben en ella.
- R20 [D] `Projects` DEBE listar los Projects v2 del owner del repo (primero como organización y luego como usuario) con `ID` = `owner/<número>` y `Name` = título.
- R21 [D] `project: owner/N` DEBE resolverse primero como organización y luego como usuario. El id del project, el del campo Status, sus opciones y el campo de inicio DEBEN guardarse en `.bflow/cache/github.json`, con la clave `project`. CUANDO una mutación falla con NOT_FOUND sobre un id que vino de la caché, el sistema DEBE descartar la caché y reintentar una vez [N].

**Errores**
- R22 [D] 401 DEBE dar "token inválido o vencido (bflow connect github)". 403 sin límite, o un error de GraphQL `INSUFFICIENT_SCOPES`/`FORBIDDEN` en Projects, DEBE decir que falta el permiso de Projects e indicar el token que hace falta: fine-grained con Projects de la organización, si el owner es una organización, o clásico con scope `project`, si es un usuario. 404 DEBE envolver `tracker.ErrNotFound`. Sin token, DEBE fallar antes de llamar a la red.
- R23 [D] CUANDO la respuesta es 429, o 403 con `Retry-After` o `x-ratelimit-remaining: 0`, el sistema DEBE esperar (`Retry-After`, o hasta `x-ratelimit-reset`, entre 1 y 30 s) y reintentar hasta 3 veces.
- R24 [N] Ningún mensaje de error, texto de doctor ni archivo de caché DEBE contener el token. Los cuerpos de error DEBEN recortarse a 200 caracteres.

**CLI**
- R25 [D] `bflow tracker setup` DEBE mostrar lo creado, o lo que se crearía con `--dry-run`. Con `ManualSetupError`, DEBE responder `ok` con código `manual` y el texto "crea a mano en el project <owner/N>, campo Status:" seguido de la lista.
- R26 [D] `doctor`, con adapter github, DEBE comprobar el token y el acceso al repo. Si hay project, DEBE comprobar que el token lo ve (vía `Projects`) y que existe el campo Status. DEBE avisar (`warn`) de las fases sin etiqueta u opción. Sin project, NO DEBE marcar como fallo la ausencia de project.
- R27 [N] `bflow init` DEBE ofrecer `github` como tracker (solo si el host es github). Con `--project owner/N`, o eligiendo de `Projects` (con la opción "sin project"), escribe `tracker.adapter: github` y, si aplica, `project`. Sus siguientes pasos DEBEN incluir `bflow connect github` y `bflow tracker setup --dry-run`.

## Design

### Paquetes y nombres

**`internal/tracker/states.go`** (nuevo; la tabla sale de `plane`):
```go
type StateDef struct { Names []string; Group string; Color string } // Color "#RRGGBB"
var DefaultStates = map[flow.Phase]StateDef{ /* la tabla actual de plane, sin cambios */ }
var PhaseOrder = append(append([]flow.Phase{flow.Backlog}, flow.Order...), flow.Blocked)
type StateTable map[flow.Phase]StateDef
func NewStateTable(overrides map[string][]string) StateTable            // DefaultStates + tracker.states
func (t StateTable) Write(existing []string, p flow.Phase) (string, bool) // nombre existente donde se escribe p
func (t StateTable) PhaseOf(name string) flow.Phase                      // la misma regla de plane.phaseOf
```
`plane` pasa a usar `tracker.StateTable`: `Client.States` cambia de tipo y `plane.DefaultStates`/`plane.StateDef` desaparecen, porque nadie fuera del paquete los usa. Su comportamiento no cambia y sus pruebas siguen verdes sin tocarlas.

**`internal/tracker/tracker.go`** (se agrega):
```go
// PRLinker: capacidad opcional; la línea que cierra la tarea al mergear el PR ("" si no aplica).
type PRLinker interface{ CloseRef(id string) string }
// ManualSetupError: el tracker no puede crear estados sin riesgo; Missing lista qué crear a mano.
type ManualSetupError struct{ Where string; Missing []string; Reason string }
func (e *ManualSetupError) Error() string
```

**`internal/tracker/trackertest`**: `Harness.NoStartDate bool`. Cuando es true, el subtest "stamp start once" solo verifica que `Transition` con `StampStart` no falla.

**`internal/config`**: en `Tracker`, `Prefix string \`yaml:"prefix,omitempty"\`` y `StartField string \`yaml:"start_field,omitempty"\``. `Known.Trackers` suma `"github"`. Las validaciones de R2 van en `Validate`.

**`internal/adapters/tracker/github`** (paquete `github`; en `cmd/bflow` se importa como `ghtracker`):
```go
type Options struct {
    API, Repo, Prefix, Project, StartField, Token string
    States    map[string][]string
    CachePath string // .bflow/cache/github.json
}
func New(o Options) *Client
// Client implementa tracker.Tracker, Creator, StateProvisioner, StateLister, ProjectLister, PRLinker
// y SameState(a, b flow.Phase) bool (para la suite de contrato).
func (c *Client) CloseRef(id string) string // "Closes #42"; "" si id no es de este prefijo
```
Archivos: `github.go` (cliente, REST y GraphQL, errores), `issues.go` (Get, List, Transition, Comments, Create), `project.go` (resolución, caché, items, Status, fecha), `setup.go` (EnsureStates, StateMap, Projects).

### Protocolo

- REST (`Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28`, `Authorization: Bearer`):
  - `GET/PATCH /repos/{r}/issues/{n}`: los PR traen `pull_request`.
  - `GET /repos/{r}/issues?state=open|all&per_page=100`, paginado con `Link: rel="next"` y tope de 50 páginas.
  - `POST /repos/{r}/issues`.
  - `GET/POST /repos/{r}/issues/{n}/comments`.
  - `POST /repos/{r}/issues/{n}/labels`.
  - `DELETE /repos/{r}/issues/{n}/labels/{url.PathEscape(name)}`, donde 404 cuenta como hecho.
  - `GET/POST /repos/{r}/labels`.
- GraphQL (`POST {graphqlURL}`): `graphqlURL` es `…/api/graphql` cuando `API` termina en `/api/v3` (Enterprise) y `API + "/graphql"` en otro caso.
  - Resolución: `organization(login){projectV2(number){id title fields(first:50){…SingleSelect{id name options{id name color description}} …ProjectV2Field{id name dataType}}}}`. Con NOT_FOUND se repite con `user(login)`.
  - Item de un issue: `node(id: issue.node_id){…on Issue{projectItems(first:50){nodes{id project{id} status: fieldValueByName(name:"Status"){…SingleSelectValue{name optionId}} start: fieldValueByName(name:$start){…DateValue{date}}}}}}`.
  - List: `node(id: projectId){…on ProjectV2{items(first:100, after){nodes{id fieldValues… content{…on Issue{number title body state url updatedAt repository{nameWithOwner}}}} pageInfo}}}`, con tope de 50 páginas.
  - Mutaciones: `addProjectV2ItemById`, `updateProjectV2ItemFieldValue` (`singleSelectOptionId` o `date`), `updateProjectV2Field(fieldId, singleSelectOptions)`.
  - Introspección: `__type(name:"ProjectV2SingleSelectFieldOptionInput"){inputFields{name}}`.
- Los colores de las etiquetas son el hex de la tabla sin `#`. Los de las opciones van a un enum fijo por fase en `setup.go`: backlog GRAY; discovery, spec e implementing BLUE; contract y paused PURPLE; quality PINK; documenting GREEN; walkthrough e in_review ORANGE; done GREEN; blocked RED. La descripción de etiquetas y opciones es "bflow: <fase>".
- Caché: `{"project":"owner/7","owner_type":"org|user","project_id":"…","status_field":"…","options":{"Backlog":"id",…},"start_field":"…"}`, escrita con `store.WriteAtomic`. `tracker setup` y `tracker states` siempre leen de la red y reescriben la caché.

### Decisiones descartadas
- Usar `gh` CLI como transporte: dependería de un binario externo, y la suite de contrato necesita httptest.
- Guardar la fase en el cuerpo del issue: se pisa al editar y no se ve en tableros.
- Una etiqueta por fase con project: duplicaría la verdad que ya lleva Status.

### Superficie de seguridad
- **Authz:** el token solo viaja en el header `Authorization`, al host de `vcs.api_url` o `api.github.com`. Nunca se registra ni se guarda fuera del llavero o de las variables de entorno. La caché solo guarda ids públicos del project.
- **Validación:** el número del ID se parsea como entero antes de armar rutas. Los nombres de etiqueta se escapan en rutas. `project` y `prefix` se validan en config (R2). Las respuestas se leen con un límite de 8 MB y un timeout de 20 s.
- **Errores:** se muestran método, ruta y estado, más un mensaje de la API recortado a 200 caracteres. Nunca headers ni el token (R24).
- **Datos de terceros:** título, cuerpo y comentarios de un issue los escribe cualquiera con acceso al repo. Entran a `Task.Description` y `Comment.Body` como datos, y `bflow show` los sigue envolviendo en `<pasted_content>`. El adaptador no los interpreta.

## Tasks

- [x] **T1 · Contrato.** Tabla compartida y firmas públicas: `internal/tracker/states.go` (con `plane` migrado, sin cambiar su comportamiento), `PRLinker`, `ManualSetupError`, `Harness.NoStartDate`, `config.Tracker.Prefix/StartField`, y el esqueleto del paquete `internal/adapters/tracker/github` con `Options`, `New` y los métodos que devuelven "no implementado". Pruebas nuevas (fallan hasta su tarea):
  - `internal/tracker/states_test.go`: `TestStateTablePhaseOf` (incluye Todo, In Progress y Done) y `TestStateTableOverrides`.
  - `internal/adapters/tracker/github/github_test.go`, sobre el fake `fake_test.go` (httptest con REST y GraphQL):
    - contrato: `TestContractLabels`, `TestContractProject`
    - identificadores y lectura: `TestGetPullRequestIsNotFound`, `TestWrongPrefixIsNotFound`, `TestUnlabeledIssueIsBacklog`, `TestClosedUnlabeledIsDone`, `TestListSkipsPullRequests`, `TestListProjectOnlyThisRepo`
    - escritura: `TestTransitionKeepsOnlyTargetLabel`, `TestDoneClosesIssueOnce`, `TestStartAddsIssueToProject`, `TestStampStartMissingFieldIsNoop`, `TestCreateLeavesBacklog`, `TestCloseRef`
    - setup y projects: `TestSetupLabelsDryRun`, `TestSetupOptionsKeepIDs`, `TestSetupManualWhenIDUnsupported`, `TestOwnerResolvesOrgThenUser`, `TestStaleCacheRetriesOnce`
    - errores y red: `TestErrorMessages` (tabla: 401, 403 de scope org/usuario, 404, sin token, ninguno contiene el token), `TestRateLimitRetry`, `TestGraphQLURLEnterprise`
  - `internal/engine`: `TestPRBodyClosesIssue`.
  - `internal/config`: `TestValidateGithubTracker`.
  - `internal/cli`: `TestTrackerSetupManual`, `TestDoctorGithubTracker`.
  - Cubre R2, R3, R16, R18 y R19 (firmas).
- [x] **T2 · Cliente e identificadores.** `do` (REST), `graphql`, paginación por `Link`, mapa de errores y reintentos, `graphqlURL`, `parseKey` y `Get` sin project. R4, R9, R22, R23, R24.
- [x] **T3 · Modo etiquetas.** List, Transition (etiquetas y cierre en done), Comment, Comments, Create, CloseRef y SameState; `TestContractLabels` en verde. R5, R7, R10, R12, R14, R15.
- [x] **T4 · Modo project.** Resolución org/usuario, caché con reintento, item del issue, Status, add-to-project, fecha de inicio, List del project; `TestContractProject` en verde. R6, R8, R11, R13, R21.
- [x] **T5 · Setup, estados y projects.** EnsureStates (etiquetas; opciones con introspección, verificación de ids y `ManualSetupError`), StateMap y Projects. Antes de escribir `updateProjectV2Field`, confirma en la documentación de la API la forma de `singleSelectOptions` y anótalo en decisions. R17, R18, R19, R20.
- [x] **T6 · Wiring y núcleo.** `Known.Trackers`, `Validate` (R2), `buildTracker` case `github` (repo y token compartidos con `buildHost` mediante un helper), `connect`/`projects` para github, `PRBody` con `PRLinker`, y `tracker setup` con `ManualSetupError`. R1, R2, R3, R16, R25.
- [x] **T7 · doctor e init.** `doctorTracker` para github (token, repo, project, Status, faltantes) y `init` con tracker github y elección de project. R26, R27.
- [x] **T8 · Documentación y prueba manual.** README: sección del tracker github (config, tokens por tipo de owner, setup manual). Prueba manual en un repo de prueba contra GitHub real, sin project y con project (setup, start, transición, done, `Closes #N`), anotada en el walkthrough. R1-R27.

### Decisiones de implementación
- `updateProjectV2Field` recibe `singleSelectOptions` como `[ProjectV2SingleSelectFieldOptionInput!]` con `name`, `color` (enum), `description` y, si la introspección lo permite, `id`. Así está escrito por conocimiento de la API y contra el fake; no se pudo confirmar en la documentación desde este entorno: queda para la prueba manual de T8.
- `githubAccess` (wire.go) comparte repo, API y token entre `buildHost` y `buildTracker`. El token sale siempre de `Key("github", host de vcs.api_url)` (el mismo que escribe `connect`), no de la URL completa.
- `build` crea el cliente git antes que el tracker, porque el tracker github deduce el repo del remoto.
- `init` detecta el host antes de preguntar el tracker, para ofrecer `github` solo con host github.
- Con project, `Transition` no vuelve a escribir Status si el item ya tiene la opción.
- Cuando el Status de un item no traduce a ninguna fase, `Phase` queda vacía y `State` lleva el nombre; sin item o sin Status es backlog con `State` vacío.
