# Contribuir a bflow

Gracias por el interés. bflow está en `v0.x` y el diseño todavía cambia; antes de un cambio grande abre un issue para acordarlo.

## Preparar el entorno

Requisitos: Go 1.25 o superior y git.

```bash
git clone https://github.com/innobytestech/bflow
cd bflow
go vet ./...
go test ./...        # incluye E2E que compilan el binario y usan git real
```

## Reglas del código

- **Pruebas primero.** Cada cambio llega con su prueba; un bug, con la prueba que lo reproduce.
- **Núcleo neutral.** `internal/...` nunca importa `internal/adapters/...` (lo verifica `internal/archtest`). Solo `cmd/bflow` conecta ambos.
- **Sin dependencias nuevas** fuera de la biblioteca estándar, `gopkg.in/yaml.v3` y `github.com/zalando/go-keyring`, salvo acuerdo previo en un issue.
- **Adaptadores externos** se prueban con `httptest` y respuestas grabadas, nunca contra servicios reales. Un tracker nuevo debe pasar la suite de conformidad.
- **Salida para agentes mínima.** El JSON de `--json` es compacto y sin campos redundantes; cada byte cuesta tokens.
- `gofmt` y `go vet` limpios. CI corre en Linux (con `-race`) y Windows.

## Commits y pull requests

- Mensajes estilo Conventional Commits: `feat(tracker): …`, `fix(guard): …`, `docs: …`.
- Un PR por tema, con descripción de qué cambia y cómo se probó.
- No incluyas datos internos de tu organización (URLs, IDs de proyectos, correos) en código, pruebas ni fixtures.

## Licencia

Al contribuir aceptas que tu aporte se publique bajo la [licencia Apache 2.0](LICENSE).

## Seguridad

Las vulnerabilidades se reportan en privado; ver [SECURITY.md](SECURITY.md).
