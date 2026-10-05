# Configuración

## `bflow.yaml` y sus capas

Hay tres capas; la última gana:

1. **Global** (`~/.config/bflow/config.yaml`, en Windows `%AppData%\bflow\config.yaml`), con perfiles compartidos entre repos.
2. **Perfil**, el que cada repo referencia.
3. **Repo** (`bflow.yaml`), solo con lo propio de ese repo.

`bflow init` crea el `bflow.yaml`: detecta stack, remoto, rama base y propone los pasos de check. Pregunta solo lo que no pudo detectar; con `--yes` no pregunta nada. Sin `bflow.yaml`, bflow funciona con el tracker local y solo con git.

```bash
bflow init [--yes] [--profile p] [--tracker local|plane|github] [--project P]
bflow doctor       # valida config, herramientas, conexiones y hooks
```

### Perfiles

```bash
bflow profile add acme --tracker plane --url https://plane.example.com --host github --base dev
bflow profile list
bflow profile use acme       # en este repo
```

```yaml
# global
profiles:
  acme:
    tracker: { adapter: plane, url: https://plane.example.com, workspace: acme }
    vcs: { host: github, base_branch: dev }
    agent: claude
```

```yaml
# bflow.yaml del repo (lo genera bflow init)
profile: acme
stack: go
tracker: { project: API }        # identificador legible, nunca UUID
check:
  steps:
    - { name: vet,   run: "go vet ./..." }
    - { name: test,  run: "go test -count=1 {race} ./..." }
    - { name: lint,  run: "golangci-lint run --new-from-rev={base}", needs: golangci-lint }
    - { name: vulns, run: "govulncheck ./...", needs: govulncheck, optional: true }
```

### Validación

La validación junta todos los problemas en un solo mensaje, indica la línea de los campos desconocidos y rechaza cualquier clave que parezca un secreto.

## Check

`bflow check [ID]` corre los pasos de `check.steps`, resume los fallos en pocas líneas y liga el resultado a un commit; el detalle completo queda en un archivo (`bflow show <ID> check`).

- `--quick <paquete>`: solo los pasos rápidos sobre un paquete, para iterar. No registra resultado, así que no cuenta para reportar DONE.
- `--verify`: no corre los pasos; comprueba que el último check pasó y corresponde al código actual (y que las pruebas congeladas no cambiaron).
- Un paso con `needs` depende de una herramienta: si falta y el paso es `optional: true`, se omite con aviso; si no es opcional, falla.
- Un paso de vulnerabilidades marca como preexistentes las que la tarea no tocó (ver la [guía](guia.md#cuando-algo-se-atora)).

## Conectar un tracker

Los tokens se validan antes de guardarse en el llavero del sistema (Credential Manager, Keychain o Secret Service) y nunca se escriben en YAML. En CI se usan variables de entorno: `BFLOW_PLANE_TOKEN`, `GH_TOKEN`.

```bash
bflow connect plane --url https://plane.example.com --workspace mi-workspace
bflow connect github --from-gh      # o --token
bflow tracker states                # cómo se traducen las fases a tus estados (solo lectura)
bflow tracker setup --dry-run       # qué estados faltarían crear
bflow tracker setup                 # crea los que falten
```

### Tracker local

Es el predeterminado: las tareas son archivos en el repo, sin cuenta ni token. `bflow task add "título"` crea `LOCAL-1`, y `bflow task list` lista las abiertas.

### Plane

`tracker.adapter: plane` con la URL y el workspace (del perfil o de `bflow.yaml`). Las fases se traducen a los estados del proyecto con nombres literales; `bflow tracker states` los muestra y `bflow tracker setup` crea los que falten.

### GitHub Issues y Projects

Las tareas pueden vivir en los issues del repo (`GH-42` es el issue #42), con o sin un Project v2. Exige `vcs.host: github`; de ahí salen el repo, la API (`vcs.api_url`) y el token de `bflow connect github`.

```yaml
vcs: { host: github }
tracker:
  adapter: github
  project: mi-org/7        # opcional; sin él, la fase es una etiqueta bflow:<fase>
  # prefix: GH             # prefijo de los IDs (GH por defecto)
  # start_field: Start date  # campo de fecha del project donde se sella el inicio
```

- **Sin project:** la fase es la etiqueta `bflow:<fase>`. `bflow tracker setup` crea las que falten. Un issue abierto sin etiqueta es backlog; uno cerrado, done. No hay fecha de inicio.
- **Con project:** la fase es una opción del campo de selección única Status, con los mismos nombres que Plane (`tracker.states` los cambia). `bflow tracker setup` agrega las opciones que falten si la API conserva los ids de las existentes; si no, no escribe nada y lista qué crear a mano (`crea a mano en el project mi-org/7, campo Status:`). Al empezar, el issue entra al project y se sella la fecha de inicio si existe el campo.
- Al llegar a done se cierra el issue (bflow nunca reabre uno cerrado) y el PR lleva `Closes #N`.
- **Token:** el mismo de `bflow connect github`. Para Projects de una organización hace falta un token fine-grained con el permiso de organización Projects; para Projects de un usuario, un token clásico con el scope `project`. `bflow doctor` lo revisa.
- `bflow init` ofrece `github` cuando el remoto es de GitHub y deja elegir el project (o ninguno): `bflow init --tracker github --project owner/N`.

## Agentes

`agent:` acepta una herramienta (`agent: claude`) o una lista (`agent: [claude, opencode]`); vacío equivale a `claude`. `bflow init --agent claude,opencode` acepta varias, y detecta `.opencode/`, `opencode.json` y `opencode.jsonc`.

```bash
bflow render           # escribe .claude/agents/bflow-<agente>.md; commitéalos
bflow render --check   # en CI: falla si no coinciden con la configuración
```

Cada agente tiene dos capas. El **contrato** con bflow (qué recibe, qué archivos escribe, qué veredictos puede reportar y cómo) sale del flujo y no se configura. El **oficio** (cómo hace su trabajo) trae un default y cada repo lo complementa:

```yaml
# bflow.yaml
agents:
  implementer:
    model: sonnet
    effort: medium
    read: [docs/architecture/]           # el agente los lee antes de empezar
    extra: docs/bflow/implementer.md     # instrucciones propias, se copian al agente
  documenter: { model: haiku, effort: low }
```

Con `read`, el agente no carga CLAUDE.md (`omit_claude_md: false` lo cambia): las reglas del repo le llegan por esas rutas. Un agente propio se agrega a una fase en `flow.agents` y define su oficio en `extra`; bflow le antepone el contrato. Los agentes generados llevan el prefijo `bflow-` para no chocar con los tuyos; `render` nunca pisa un archivo que no generó. Si el repo tiene `AGENTS.md` (lo leen Codex y OpenCode), `render` mantiene en él un bloque que dice cómo retomar una tarea con bflow. `bflow doctor` avisa si los agentes están desactualizados y si alguna skill instalada parece de proceso (ramas, PR, tracker, specs), porque puede chocar con el flujo; `doctor.ignore_skills` silencia las que no chocan. Más ejemplos de oficio en la [guía](guia.md#ajustar-a-los-agentes).

## Claude Code y OpenCode

### Claude Code

`bflow install claude` instala la skill y los hooks (ver [instalación](instalacion.md#instalar-en-tu-herramienta-de-agente)). La skill tiene unas 30 líneas: no contiene reglas del flujo, solo cómo interpretar `next`. Los hooks corren:

- `bflow hook session-start` al abrir la sesión.
- `bflow guard --reads` antes de cada edición, comando o `Read` (de los `Read` solo registra los del reviewer en quality, para medir cuánto del diff abrió).
- `bflow hook tokens` al terminar cada turno y cada subagente (cada uno cuenta solo su transcript).
- `bflow hook subagent-stop` cuando termina un agente de bflow: si no reportó, lo hace seguir hasta 2 veces con lo que le falta y después bloquea la tarea para que decida una persona.
- `bflow statusline`, la barra de estado:

```
API-12 · implementing · 1h42m · ronda 1 · 145.1k nuevos · 2.9M caché
```

Los archivos fuente están en [`adapters/claude/`](../adapters/claude/).

### OpenCode

Con `opencode` en `agent:`, `bflow render` escribe `.opencode/agents/bflow-<agente>.md` (subagentes con permisos `edit` y `bash` según lo que hace cada agente) y `bflow render --check` cubre los dos conjuntos. También escribe `.opencode/plugins/bflow.js` (se commitea y no se edita): el guard antes de bash, edit, write y patch, y los tokens por fase y agente. OpenCode no tiene nudge de subagente; el plugin inyecta `bflow hook session-start` al crear una sesión raíz (sin pedir respuesta), así que un `/clear` no pierde el hilo.

OpenCode pide el modelo como `proveedor/modelo`, así que los alias de los agentes (`sonnet`, `haiku`) se traducen con `models.opencode`, en `bflow.yaml`, en un perfil o en la config global (gana el repo, por alias):

```yaml
models:
  opencode:
    sonnet: anthropic/claude-sonnet-4
    haiku: anthropic/claude-haiku-4
```

Un valor con `/` se usa tal cual. Un alias sin equivalente deja al agente sin `model:` (usa el de tu OpenCode) y `render` y `bflow doctor` lo avisan. Los archivos fuente están en [`adapters/opencode/`](../adapters/opencode/).

## Paneles y notificaciones

- `bflow watch` es un panel en vivo en otra terminal: quién trabaja y desde cuándo, o qué gate espera tu decisión; lo que sigue; tiempos por fase; tokens por agente; fricción, y los últimos eventos. `bflow watch --open` lo abre en otra pestaña o ventana si no hay uno abierto. Con `ui: { watch: true }`, `bflow start` lo abre solo; nunca en CI ni sin escritorio.
- `bflow ui` muestra el panel en el navegador, en una página local de solo lectura: tablero, línea del carril, métricas y todos tus repos agrupados por perfil. Con `ui: { web: true }`, la ventana también se abre al empezar una tarea; si ya hay una abierta, no abre otra.
- Cuando la tarea espera tu decisión (una gate o un bloqueo) y Claude termina su turno, bflow manda una notificación del sistema, una vez por espera. `ui: { notify: false }` la apaga.

Como son preferencias personales, `ui:` va en la config global, no en `bflow.yaml`. El primer `bflow init` en cada máquina pregunta qué abrir (panel y navegador, solo el panel o nada) y lo guarda ahí; no vuelve a preguntar, pero puedes cambiarlo editando `ui:`.
