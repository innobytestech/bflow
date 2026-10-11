# GH-99 · doctor no avisa cuando OpenCode no carga el plugin ni cuando el proveedor deja al agente quieto

## Brief

**Objetivo**: que `bflow doctor` diga qué versiones de OpenCode hay, qué plugin le toca a cada una y si OpenCode no pudo cargar el plugin del repo o se trabó por un error del proveedor, leyendo el log de OpenCode.

**Entra**
- `doctor` (con `opencode` en `agent:`) reporta la versión del CLI (`opencode --version`, como hoy) y la del servidor 2.x (Desktop), que saca del log, y a cada una le asigna su plugin: 1.x usa `bflow.js` y 2.x usa `bflow2/`.
- Lee el log de OpenCode, en modo solo lectura y solo la cola de cada archivo, y da `warn` si un plugin de `<repo>/.opencode/plugins/` aparece en `failed to load plugin`, con el motivo recortado.
- Da `warn` si en los últimos 7 días el log muestra `requires Global regions` o `self signed certificate`, con lo que hay que hacer.
- Una sección "Problemas comunes" en `adapters/opencode/README.md` y una fila en la tabla "Cuando algo se atora" de `docs/guia.md` que remite a esa sección.

**No entra**: arreglar los errores del proveedor desde bflow, el líder cuando falla un subagente (#100), el plugin en Desktop 2.x (#98 ya hecho), detectar Desktop por su ruta de instalación ni tocar los dos plugins.

**Lo que se leyó en esta máquina** (Windows, OpenCode CLI 1.1.53 y Desktop 2.0.22):
- El log está en `~/.local/share/opencode/log/` también en Windows. 2.x escribe `opencode.log` en logfmt (`timestamp=… level=WARN message="failed to load plugin" target="C:\\…\\bflow.js" cause="…"`) y en cada arranque deja `message="cli starting" version=2.0.22`. 1.x escribe un archivo por corrida (`2026-10-11T022342.log`) con líneas `ERROR 2026-10-01T18:31:11 … error=self signed certificate…`.
- 2.x siempre intenta cargar `bflow.js` y falla con `must export a default definition`. Es normal (para eso existe `bflow2/`) y no debe dar aviso.

**Decisiones nuevas**
- [N] La versión de Desktop se toma del último `cli starting version=` de `opencode.log`. Se descarta buscar el ejecutable de Desktop en cada sistema operativo: la ruta cambia según el SO y el instalador, y el log ya trae la versión.
- [N] Un fallo de carga solo cuenta si es posterior al mtime (fecha de modificación del archivo) más reciente de los archivos del plugin en el repo. Así, después de `bflow render` los avisos viejos dejan de salir. Se descarta una ventana fija de días, que seguiría avisando de algo ya arreglado.
- [N] Los errores del proveedor se detectan (ventana de 7 días) además de documentarse. Se descarta solo documentarlos, porque el humano no sabría que tiene que ir a buscar la doc.
- [N] Todo es `warn` y nunca `fail`: el log es una pista, no prueba de que el repo esté mal. Áreas nuevas: `log` y `proveedor`.
- [N] La lectura del log va en el adaptador, detrás de una interfaz opcional (`LogDoctor`). Así doctor sigue sin importar el adaptador, que es lo que dejó GH-98.

**Riesgos**: el formato del log no es estable entre versiones de OpenCode. Mitigación: si una línea no se reconoce se ignora sin error, y las pruebas usan fixtures copiadas de logs reales con fechas relativas. El log puede traer texto de prompts (`args=`), así que doctor nunca imprime una línea completa: solo `target` y `cause`, recortados a 160 caracteres.

**Tamaño**: S-M. Un archivo nuevo en el adaptador (~150 líneas), unas 25 líneas en doctor, pruebas y docs.

## Discovery

Carril light, sin discovery. Lo que no aclaraba el scout (ruta del log en Windows, formatos 1.x y 2.x, cómo ver la versión de Desktop y el fallo normal de `bflow.js` en 2.x) se sacó de los logs reales de esta máquina.

## Requirements

Carril light: los criterios van en Tasks.

## Design

Carril light: el detalle va en Tasks.

## Tasks

- [ ] T1 Contrato. En `internal/cli/cli.go`, la interfaz opcional:
  ```go
  // LogDoctor la implementan los adaptadores que saben leer el log de su herramienta.
  type LogDoctor interface {
      DoctorLog(root string, now time.Time) []LogFinding
  }
  type LogFinding struct{ Area, Level, Msg string } // Area: agente|log|proveedor; Level: ok|warn
  ```
  En `internal/adapters/agent/opencode/logscan.go`: `func LogDir() string` y `func (Agent) DoctorLog(root string, now time.Time) []cli.LogFinding`, o un tipo espejo si importar `cli` genera un ciclo. En ese caso la conversión va en `internal/cli` y el contrato lo deja escrito. Pruebas, con skip hasta T2 y T3:
  - `TestLogDirXDG`, `TestLogDirDefault` en `logscan_test.go`
  - `TestDoctorLogVersion2`, `TestDoctorLogPluginFallo`, `TestDoctorLogBflowJSEn2EsNormal`, `TestDoctorLogFalloViejo`, `TestDoctorLogProveedor`, `TestDoctorLogSinLog`, `TestDoctorLogNoFiltraPrompt`
  - `TestDoctorOpenCodeLog` en `internal/cli/opencode_hooks_test.go`, que usa `doctorChecks` con `XDG_DATA_HOME` apuntando a una carpeta temporal.

  Fixtures en `testdata/log_v2.txt` y `testdata/log_v1.txt`, copiadas de las líneas reales de arriba, con `{{TS-1h}}` o algo parecido para que las fechas sean relativas, como en `v2_units.txt`.

- [ ] T2 Lectura del log en el adaptador (R1-R7).
  - R1: `LogDir()` DEBE devolver `$XDG_DATA_HOME/opencode/log` si la variable está definida y, si no, `~/.local/share/opencode/log` en todos los sistemas operativos.
  - R2: DEBE leer como máximo el último 1 MiB de `opencode.log` (2.x) y del `*.log` con fecha más reciente (1.x). Sin carpeta o sin archivos, DEBE devolver un solo `log ok` que diga que no hay log en esa ruta, sin error.
  - R3: CUANDO `opencode.log` tiene `message="cli starting" version=X`, DEBE devolver `agente ok "opencode 2.x (opencode.log): versión X, usa .opencode/plugins/bflow2/"` con el último X. CUANDO el major (primer número de la versión) es mayor que 2, DEBE dar `warn` "formato de plugin no probado en esa versión".
  - R4: CUANDO una línea `failed to load plugin` tiene un `target` dentro de `<root>/.opencode/plugins/` y es posterior al mtime más reciente de `opencode.PluginFiles` en el repo, DEBE dar `log warn` con el target relativo al repo, el `cause` recortado y "corre bflow render; si sigue, revisa el log en <ruta>". Las rutas se comparan normalizadas: se quitan los escapes `\\`, se usa `filepath.Clean` y no se distinguen mayúsculas en Windows.
  - R5: Un fallo de `bflow.js` cuyo `cause` contiene `must export a default definition` NO DEBE avisar, porque es lo normal en 2.x.
  - R6: CUANDO en los últimos 7 días (respecto de `now`) aparece `requires Global regions`, DEBE dar `proveedor warn` "el modelo pide regiones Global: actívalo en Privacy de tu workspace de OpenCode". CUANDO aparece `self signed certificate`, DEBE dar `proveedor warn` "certificado autofirmado (antivirus o proxy con inspección HTTPS): define NODE_EXTRA_CA_CERTS con su CA". Un aviso por tipo como máximo. Las fechas de 2.x vienen en UTC (`Z`) y las de 1.x, sin zona, se leen como hora local.
  - R7: Ningún mensaje DEBE incluir texto de la línea fuera de `target`, `cause` y `version`, y cada campo se recorta a 160 caracteres. Una línea que no se puede leer se ignora. La función no escribe nada.
  - Quitar los skips de las pruebas de `logscan_test.go`.

- [ ] T3 Doctor (R8-R9).
  - R8: `doctorOpenCode`, cuando el adaptador implementa `LogDoctor`, DEBE agregar cada `LogFinding` con `add(area, level, msg)` después del check `plugin` y antes de `cobertura`.
  - R9: El check de versión del CLI DEBE decir qué plugin le toca: `opencode: versión 1.1.53, usa .opencode/plugins/bflow.js` si el major es 1, y `bflow2/` si es 2 o más.
  - Quitar el skip de `TestDoctorOpenCodeLog`, que cubre: fallo reciente de `bflow2` → `log warn`, solo el fallo normal de `bflow.js` → sin `warn` en `log`, certificado reciente → `proveedor warn`.

- [ ] T4 Docs (R10).
  - R10: `adapters/opencode/README.md` DEBE tener `## Problemas comunes` con cuatro casos de 1 a 3 líneas cada uno: plugin que no carga (qué dice doctor, `bflow render`, y que el fallo de `bflow.js` en 2.x es normal), modelo de Go que pide regiones Global, certificado autofirmado (`NODE_EXTRA_CA_CERTS=<ruta a la CA del antivirus o proxy>`) y cómo confirmar en el log (ruta de R1, `opencode.log` para 2.x y un archivo por corrida para 1.x, qué buscar). `docs/guia.md`, en "Cuando algo se atora", DEBE sumar la fila "OpenCode no hace nada o el guard no actúa" que remite a `bflow doctor` y a esa sección. También DEBE quedar una entrada en el changelog.
