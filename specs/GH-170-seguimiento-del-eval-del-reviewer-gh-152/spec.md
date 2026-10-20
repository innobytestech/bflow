# GH-170 · Seguimiento del eval del reviewer (GH-152): desempate de modelo, corrida huérfana, Makefile:N

## Brief

**Objetivo:** cerrar los cinco pendientes que dejó el review-map de GH-152 en el eval del reviewer, cada uno con su prueba.

**Entra**
- Empate de modelos en el resultado (`Runner.finish`, internal/eval/run.go:308-313): gana el más reciente, igual que `Tokens`.
- Corrida huérfana: `Runner.cleanup` borra `run.json` antes de `RemoveAll`, así una carpeta que no se pudo borrar entera ya no reaparece como abierta.
- Prueba de `Git.WorktreeRemove` cuando `remove` falla y el directorio ya no existe (git.go:340).
- `Makefile:3`, `Dockerfile:12` y rutas sin extensión con directorio (`cmd/tool:4`) cuentan como hallazgo en `refRe` (score.go:13).
- Herramientas de `bflow-eval-reviewer`: se alinea la spec de GH-152 con el render.

**No entra:** cambiar el mapeo general de herramientas del render de Claude ni el de OpenCode; reintentar el borrado de carpetas huérfanas que ya existan en la caché.

**Decisiones nuevas**
- [N] Empate de modelos: gana el del caso reportado más tarde. Descartado: dejar el orden alfabético y solo documentarlo, porque `Tokens` ya usa "el más reciente" y las dos cifras deben coincidir.
- [N] Corrida huérfana: `cleanup` quita `run.json` después de quitar los worktrees y antes de `RemoveAll`. Descartado: ignorar las corridas que ya tienen `.bflow/evals/<run>.json`, porque eso solo cubre el cierre normal y deja fuera `--abort` y el fallo al preparar, que también llaman a `cleanup`.
- [N] Una ruta sin extensión cuenta solo si tiene separador de directorio (`/` o `\`) o si su nombre es uno de los archivos conocidos sin extensión: Makefile, GNUmakefile, Dockerfile, Containerfile, Jenkinsfile, Gemfile, Rakefile, Procfile, Vagrantfile. Descartado: contar cualquier `palabra:N` como pide la tarea al pie de la letra, porque `localhost:8080` o `paso:2` en la nota se volverían falsos positivos y bajarían la calificación del reviewer.
- [N] Herramientas: el render de Claude traduce las herramientas abstractas igual para todos los agentes (`agents.Read` -> Read, Grep, Glob; `agents.Write` -> Edit, Write; render.go:24-28), así que es intencional. Se corrige la spec de GH-152 (línea 251) para decirlo y se fija con una prueba. Descartado: un caso especial en el render para quitar Grep, Glob y Edit al agente del eval: el reviewer real los tiene, y el eval debe medir al reviewer con las mismas herramientas.

**Riesgos**
- La lista de nombres conocidos sin extensión queda corta (p. ej. `Brewfile`): esa referencia no cuenta, igual que hoy.
- Si borrar `run.json` también falla (archivo bloqueado), la corrida sigue apareciendo abierta; `--abort` la limpia como hoy.

**Tamaño:** S. Cuatro archivos de código y sus pruebas, más una línea de la spec de GH-152.

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Carril light: los criterios van dentro de Tasks. -->

## Design

<!-- Carril light: el diseño va dentro de Tasks. -->

## Tasks

- [x] T1 Desempate de modelos por el más reciente.
  - En `Runner.finish` (internal/eval/run.go), guardar para cada modelo el índice del último caso en `st.Results` que lo usó; en empate de conteo gana el de índice mayor. Modelos vacíos se ignoran; sin modelos queda `r.Model`, como hoy.
  - Criterio: CUANDO dos modelos empatan en número de casos, el resultado DEBE reportar el del caso reportado más tarde. CUANDO uno tiene más casos, DEBE ganar ese.
  - Prueba: `TestFinishModelTieUsesLatest` en internal/eval/run_test.go (dos casos, el primero con `r.Model = "zeta"` y el segundo con `"alfa"`; espera `"alfa"`, que el orden alfabético no habría elegido).
- [x] T2 La corrida limpiada deja de estar abierta aunque `RemoveAll` falle.
  - En `Runner.cleanup`, después del bucle de worktrees y dentro del mismo chequeo de que `dir` cae en la caché, `os.Remove(filepath.Join(dir, "run.json"))` antes de `RemoveAll`. Para la prueba, `RemoveAll` pasa por una variable de paquete `removeAll = os.RemoveAll` que la prueba sustituye por una que no borra nada y devuelve error.
  - Criterio: CUANDO `cleanup` corre y `RemoveAll` falla, `Runner.Open()` DEBE devolver "" y el siguiente `Next` DEBE empezar una corrida nueva, no retomar la vieja.
  - Prueba: `TestOrphanRunDirIsNotOpen` en internal/eval/run_test.go (cierra una corrida con `removeAll` fallando; la carpeta sigue en la caché sin `run.json`; `Open()` da "").
- [x] T3 Prueba de `WorktreeRemove` sin directorio.
  - Sin cambio de código en git.go. Prueba `TestWorktreeRemoveMissingDir` en internal/adapters/vcs/git/git_test.go con un repo real (helpers `fixture`/`sh`): crear un worktree, borrar su carpeta a mano y llamar `WorktreeRemove`.
  - Criterio: CUANDO `git worktree remove` falla y el directorio ya no existe, `WorktreeRemove` DEBE devolver nil (el error de `prune`) y `git worktree list` ya no DEBE listar ese worktree.
- [x] T4 Rutas sin extensión cuentan como hallazgo.
  - En internal/eval/score.go, ampliar `refRe` (o agregar una segunda expresión) según la decisión [N]: ruta con extensión como hoy; o ruta sin extensión con `/` o `\`; o nombre base en `extensionless = []string{"Makefile", "GNUmakefile", "Dockerfile", "Containerfile", "Jenkinsfile", "Gemfile", "Rakefile", "Procfile", "Vagrantfile"}`. Actualizar el comentario de `refRe`.
  - Criterio: CUANDO la nota o un .md de out cita `Makefile:3`, `Dockerfile:12-14` o `cmd/tool:4`, DEBEN contar como hallazgos (`Makefile:3-3`, ...). CUANDO cita `localhost:8080`, `paso:2`, `a.go:` o `a.go:x`, NO DEBEN contar.
  - Prueba: cambiar la afirmación de score_test.go:93 (hoy espera 0 para `Makefile:3`) y agregar `TestFindingsExtensionlessPaths` con los casos del criterio, incluido el recorte del prefijo del worktree en `/wt/Makefile:3`.
- [x] T5 Herramientas del agente del eval alineadas con su spec.
  - Corregir specs/GH-152-04-evals-del-reviewer-medir-si-cada-cambio-de/spec.md línea 251: "herramientas Read, Write, Bash (abstractas; en Claude el render las vuelve Read, Grep, Glob, Edit, Write, Bash, como a todo agente)". Comentario de una línea en `EvalReviewerSpec` que diga lo mismo.
  - Criterio: el frontmatter de `.claude/agents/bflow-eval-reviewer.md` DEBE listar exactamente las herramientas que dice la spec.
  - Prueba: en internal/cli/eval_test.go, afirmar la línea `tools:` exacta del archivo de Claude renderizado (copiar el orden real que produce el render).
- [x] T6 Entrada en CHANGELOG.md bajo la versión en curso con los cinco puntos.
