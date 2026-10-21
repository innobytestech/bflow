# GH-186 · Archivos de comunidad: código de conducta y plantillas de issue y PR

## Brief

**Objetivo:** que Community standards de GitHub quede en verde para código de conducta, plantillas de issue y plantilla de PR, y que quien prueba la rc.10 reporte con los datos que pide `docs/como-probar-y-reportar.md`.

**Entra**
- `CODE_OF_CONDUCT.md` en la raíz: Contributor Covenant 2.1, traducción oficial al español (`https://www.contributor-covenant.org/es/version/2/1/code_of_conduct/`), con la sección de atribución del original. Contacto: info@innobytes.tech.
- `.github/ISSUE_TEMPLATE/bug.md`, `.github/ISSUE_TEMPLATE/propuesta.md` y `.github/ISSUE_TEMPLATE/config.yml`.
- `.github/pull_request_template.md`.
- Enlace al código de conducta desde `CONTRIBUTING.md` y `README.md:116`.
- Una línea en `## v0.1.0` → `### Documentación` del CHANGELOG.

**No entra:** ACCESSIBILITY.md; formularios YAML (issue forms) con campos validados; cambios a `docs/como-probar-y-reportar.md`; código Go.

**Decisiones nuevas**
- [N] Contacto del CoC: info@innobytes.tech, como pide la tarea. Es el contacto público de la organización dueña del repo (el mismo dominio que README y NOTICE ya publican), no un dato interno de un contribuidor; la regla de `CONTRIBUTING.md:29` apunta a eso. Descartado: usar solo el reporte privado de GitHub, que sirve para vulnerabilidades pero no para conducta.
- [N] Plantillas Markdown con front matter (`name`, `about`, `title`, `labels`) en vez de issue forms YAML. Descartado: issue forms, fuera de alcance según la tarea.
- [N] `config.yml` con `blank_issues_enabled: true` (no cerrar la puerta a quien no encaje en las dos plantillas) y dos `contact_links`: vulnerabilidades → `https://github.com/innobytestech/bflow/security/advisories/new` (la ruta de SECURITY.md) y guía de reporte → `docs/como-probar-y-reportar.md` en GitHub. Descartado: `false`, que obliga a elegir plantilla.
- [N] La plantilla de bug pide `bflow status --json` además de `bflow doctor` (verificado: cualquier comando acepta `--json`, `docs/comandos.md:3`), con aviso de no pegar tokens ni `.env`.

**Riesgos**
- Texto del CoC: debe copiarse de la fuente oficial sin parafrasear; si se edita, deja de ser el Covenant y GitHub puede no reconocerlo. Solo se reemplaza el marcador de contacto.
- Verificación final solo posible tras el merge (Insights → Community standards). Sin pruebas automáticas: son Markdown.

**Tamaño:** pequeño (6 archivos nuevos o tocados, sin código).

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Carril light: los criterios van dentro de Tasks. -->

## Design

<!-- Carril light: ver Brief y Tasks. -->

## Tasks

- [x] **T1 · Código de conducta.** Crear `CODE_OF_CONDUCT.md` con el texto oficial en español del Contributor Covenant 2.1, sección de aplicación con info@innobytes.tech como contacto y sección de atribución con enlace a la versión 2.1 y a la licencia CC BY 4.0 del Covenant.
  - Criterio: CUANDO GitHub analiza el repo, DEBE reconocer el archivo como Contributor Covenant (Community standards en verde tras el merge).
  - Criterio: el texto no difiere del oficial salvo el marcador de contacto.
- [x] **T2 · Plantillas de issue.** Crear en `.github/ISSUE_TEMPLATE/`:
  - `bug.md` (`name: Bug`, `labels: bug`): qué pasó y qué esperabas; pasos en orden; salida de `bflow version`; sistema operativo; agente y su versión (Claude Code u OpenCode); tracker (local, Plane, GitHub); salida completa del comando que falló (con `--json` si salió con código 2); salida de `bflow status --json` o `bflow doctor`. Aviso visible: no pegar tokens, `.env` ni datos internos.
  - `propuesta.md` (`name: Propuesta`, `labels: enhancement`): problema que resuelve, propuesta, alternativas consideradas, qué no entra.
  - `config.yml`: `blank_issues_enabled: true` y `contact_links` a vulnerabilidades (advisory privado, como dice SECURITY.md) y a la guía `docs/como-probar-y-reportar.md`.
  - Criterio: CUANDO alguien abre "New issue", DEBE ver Bug, Propuesta y los dos enlaces; todo en español.
  - Criterio: cada dato que pide la guía de reporte aparece en `bug.md`.
- [x] **T3 · Plantilla de PR.** Crear `.github/pull_request_template.md` con: qué cambia, cómo se probó, `Closes #` y checklist: cada cambio trae su prueba; CHANGELOG actualizado si cambia la salida pública; sin datos internos (URLs, IDs, correos); `gofmt` y `go vet` limpios.
  - Criterio: CUANDO alguien abre un PR, el cuerpo DEBE prellenarse con esa plantilla; los puntos del checklist coinciden con reglas de CONTRIBUTING.md.
- [x] **T4 · Enlaces y CHANGELOG.** En `CONTRIBUTING.md` agregar una línea que enlace `CODE_OF_CONDUCT.md`; en `README.md:116` agregar "Código de conducta: [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md)." junto a CONTRIBUTING y SECURITY; en `CHANGELOG.md`, bajo `## v0.1.0` → `### Documentación`, una viñeta: código de conducta (Contributor Covenant 2.1), plantillas de bug y propuesta, plantilla de PR.
  - Criterio: los enlaces relativos resuelven; no se toca el encabezado de versión del CHANGELOG.
  - Criterio: `bflow check` (si existe para el repo) queda verde.
