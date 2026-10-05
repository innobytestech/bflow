# Byteflow · `bflow`

**Motor de flujo Spec-Driven Development para trabajar con agentes de IA.** Un solo binario en Go que lleva cada tarea de la idea al PR mergeado (discovery, spec, contrato, implementación, revisión, walkthrough), decide las transiciones, habla con tu tracker y con git, y le dice al agente exactamente qué hacer después, en pocas líneas de JSON.

> Estado: **prerelease v0.1.0-rc.9**. Funciona de punta a punta con el tracker local, Plane y GitHub, y con Claude Code y OpenCode como agente. La API de comandos puede cambiar antes de la 1.0. Si lo vas a probar, empieza por [cómo probar y reportar](docs/como-probar-y-reportar.md).

---

## Por qué existe

Un harness de prompts y scripts en dos repos reales (backend y frontend) tenía siempre los mismos problemas: reglas en prosa que el modelo respetaba "casi" siempre, estado movido a mano por el modelo, unos 7.000 tokens fijos por sesión en trabajo mecánico, entre 73% y 80% del harness versionado era estado y no especificaciones, y cada repo tenía su copia, que se desviaba.

Byteflow separa lo que **requiere criterio** (entender el problema, diseñar, programar, revisar), que hace el agente, de lo que es **determinista** (estado, transiciones, git, tracker, pruebas, reglas), que hace el CLI. El agente no tiene que recordar el proceso: pregunta a `bflow` qué sigue y lo hace. La historia completa y el rumbo: [Por qué y rumbo](docs/por-que-y-rumbo.md).

## Para quién es (y para quién no)

bflow es opinado a propósito: tiene un flujo definido y gates humanas fijas. Muestra una forma concreta de trabajar con agentes.

**Te sirve si:**

- Desarrollas features con agentes de IA y quieres que las decisiones importantes (qué se construye, el contrato, qué se mergea) las tome una persona, sin leer todo lo que el agente genera.
- Trabajas con git y PRs, y con Claude Code u OpenCode como agente.
- Quieres saber cuánto cuesta cada feature (tiempo, tokens, iteraciones) y dónde se atora.

**No te sirve si:**

- Buscas que el agente trabaje solo de punta a punta, sin aprobaciones.
- Tu flujo no pasa por ramas y PRs.
- Usas otra herramienta de agente y necesitas todas las garantías: hoy solo Claude Code y OpenCode tienen adaptador (ver [qué garantiza](docs/conceptos.md#qué-garantiza-y-qué-no)).

## Instalar

Linux y macOS (en `~/.local/bin`):

```bash
curl -fsSL https://raw.githubusercontent.com/innobytestech/bflow/main/install.sh | sh
```

Windows (en `%LOCALAPPDATA%\Programs\bflow`):

```powershell
irm https://raw.githubusercontent.com/innobytestech/bflow/main/install.ps1 | iex
```

Los scripts verifican el SHA-256 del archivo antes de instalar. Versión fija, Go, actualizar y desinstalar: [Instalación](docs/instalacion.md).

## Inicio rápido

```bash
cd mi-repo
bflow init                 # detecta stack, remoto, rama base y propone los pasos de check
bflow doctor               # valida config, herramientas, conexiones y hooks
bflow install claude       # o: bflow install opencode (skill o comando, y hooks)
```

Sin `bflow.yaml`, bflow funciona con el tracker local y solo con git. Para recorrer una tarea:

```bash
bflow new --lane light --title "Validación de RFC" --file idea.md   # crea la tarea y la arranca
bflow report LOCAL-1 --agent spec-author --verdict READY    # lo corre el agente
bflow show LOCAL-1 brief
bflow approve LOCAL-1                                        # crea la rama
# ... el implementer programa y corre `bflow check` ...
bflow report LOCAL-1 --agent implementer --verdict DONE     # con pasos de check, exige un check verde
bflow status
```

Cada comando imprime el siguiente paso; con `--json`, ese paso llega en `next`. En la práctica le dices a tu agente "qué sigue" y él lo corre. Para conectar Plane o GitHub: [Configuración](docs/configuracion.md).

## Qué garantiza

Lo que vive en el CLI se cumple con cualquier herramienta (el estado y las gates los maneja bflow, `DONE` exige un check verde, las pruebas del contrato no cambian). Lo que depende de hooks, solo donde hay hooks (Claude Code y OpenCode): bloquear `git reset --hard`, push forzado, `.env` y `.bflow/`. bflow no garantiza la calidad del código ni contiene a un agente malintencionado. La tabla completa: [Conceptos](docs/conceptos.md#qué-garantiza-y-qué-no).

## Rumbo

Hoy: v0.1.0-rc.9. Previsto: v0.1.0 (primera release anunciada); Fase 1, legible para el mundo (inglés); Fase 2, que exista para alguien; Fase 3, adopción en equipo (Linear, Codex); Fase 4, medir y decidir; y la 1.0 con la API de comandos estable. Cada fase y su criterio de salida: [Por qué y rumbo](docs/por-que-y-rumbo.md#rumbo).

## Documentación

| Documento | Para qué |
|---|---|
| [Por qué y rumbo](docs/por-que-y-rumbo.md) | La motivación, la meta y las fases. |
| [Instalación](docs/instalacion.md) | Requisitos, instalar, actualizar, verificar, desinstalar. |
| [Conceptos](docs/conceptos.md) | Núcleo y adaptadores, fases y carriles, gates, `next`, qué queda en el repo, garantías. |
| [Cómo trabajar con bflow](docs/guia.md) | El día a día: qué haces en cada gate, ajustar agentes, qué hacer cuando algo se atora. |
| [Configuración](docs/configuracion.md) | `bflow.yaml`, perfiles, trackers, agentes, check, Claude Code y OpenCode. |
| [Comandos](docs/comandos.md) | Referencia de todos los comandos. |
| [Marcas de sesión](docs/marcas-sesion.md) | Cómo se reparten los tokens entre tareas. |
| [Cómo probar y reportar](docs/como-probar-y-reportar.md) | Qué esperamos de quien prueba, cómo reportar y problemas conocidos. |

## Desarrollo

```bash
go vet ./...
go test ./...        # incluye E2E que compilan el binario y usan git real
```

- Dependencias: biblioteca estándar, `gopkg.in/yaml.v3` y `github.com/zalando/go-keyring`.
- El núcleo (`internal/...`) nunca importa `internal/adapters/...`; lo verifica `internal/archtest`. El único lugar que conecta ambos es `cmd/bflow`.
- Los adaptadores externos se prueban con `httptest` y respuestas grabadas, nunca contra servicios reales.
- Guía para contribuir: [`CONTRIBUTING.md`](CONTRIBUTING.md). Vulnerabilidades: [`SECURITY.md`](SECURITY.md).

## Licencia

[Apache License 2.0](LICENSE). Copyright 2026 Innobytes ([innobytes.tech](https://innobytes.tech)).
