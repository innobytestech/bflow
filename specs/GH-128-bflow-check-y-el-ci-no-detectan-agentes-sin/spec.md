# GH-128 · bflow check y el CI no detectan agentes sin renderizar; GH-119 se mergeó con agentes desactualizados

## Brief

**Objetivo:** que `bflow check` y el CI fallen cuando los agentes renderizados (`.claude/agents/`, `.opencode/agents/`, plugin) no coinciden con las plantillas del árbol de trabajo.

**Entra**
- Un paso `render` en `check.steps` de `bflow.yaml` que corre `go run ./cmd/bflow render --check`.
- El mismo comando como paso del job `test` en `.github/workflows/ci.yml` (ubuntu y windows).
- Una prueba que protege que el paso siga en `bflow.yaml`.

**No entra**
- Commitear los agentes que faltaban: ya lo hizo c0d31e0 (GH-55). En main (d8f59f1) `go run ./cmd/bflow render --check` responde "13 archivo(s) al día".
- Un paso de render dentro del código de `runCheck` para todos los repos que usan bflow (otra tarea si se quiere).
- Condiciones por ruta en `check.steps` (no existen hoy).

**Decisiones nuevas**
- [N] El paso usa `go run ./cmd/bflow`, no el binario instalado. Las plantillas (`internal/agents/craft/*.md`, plugin) van embebidas con `go:embed`: el binario de `~/go/bin` compara contra las plantillas con que se compiló, no contra las de la rama, y daría falso "al día". Descartado: llamar `planRender` dentro de `runCheck` (mismo problema: usa las plantillas del binario que corre).
- [N] El paso corre siempre, no solo cuando el diff toca `internal/agents/`. Cuesta una compilación que comparte caché con `go build`, y también detecta cambios en `bflow.yaml` (`agents:`, modelos) que cambian el render. Descartado: agregar un campo de rutas a `config.Step` (superficie pública nueva para un solo uso).
- [N] En CI corre en ambos sistemas de la matriz: `planRender` ya normaliza CRLF, y así se prueba la ruta de Windows.

**Riesgos**
- Si en Windows el checkout convierte saltos de línea en el plugin `.js`, el paso fallaría en CI; se verifica en el PR y, si pasa, se acota a Linux.
- Quien corra `bflow check` sin Go en el PATH: no aplica, este repo ya exige Go para `vet`/`test`/`build`.

**Tamaño:** S (dos archivos de config y una prueba).

## Discovery

Carril light: sin discovery. Lo que se leyó del repo está en el scout.

## Requirements

Van dentro de Tasks (carril light).

## Design

Van dentro de Tasks (carril light).

## Tasks

- [x] T1 Paso `render` en `bflow.yaml`, después de `build`: `- name: render` / `run: go run ./cmd/bflow render --check`.
  - R1: CUANDO una plantilla de `internal/agents/` o del plugin cambia y no se corrió `bflow render`, `bflow check` DEBE fallar en el paso `render` y el extracto DEBE incluir `render_outdated` y los archivos desactualizados.
  - R2: CUANDO los agentes están al día, el paso `render` DEBE pasar.
- [x] T2 Prueba `TestRepoCheckRunsRender` en `cmd/bflow/` que carga el `bflow.yaml` del repo con `config.Load` y exige un paso cuyo `run` contenga `cmd/bflow render --check`.
  - R3: CUANDO alguien quite el paso de `bflow.yaml`, `go test ./...` DEBE fallar.
- [x] T3 En `.github/workflows/ci.yml`, job `test`, después de `go vet`: `- run: go run ./cmd/bflow render --check` (sin `if`, ambos sistemas).
  - R4: CUANDO un PR trae agentes sin renderizar, el CI DEBE fallar en ese paso.
- [x] T4 Verificación manual (se anota en el PR): editar una línea de `internal/agents/craft/implementer.md` sin renderizar, correr `bflow check` y ver el fallo de R1; revertir y ver que pasa (R2).
