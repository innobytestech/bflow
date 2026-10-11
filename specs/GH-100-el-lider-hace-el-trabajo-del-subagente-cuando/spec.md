# GH-100 · El líder hace el trabajo del subagente cuando este falla

## Brief

**Objetivo:** cuando un subagente falla sin reportar, el líder no hace su trabajo: muestra el error al humano, explica la causa probable y lo relanza solo si el humano lo confirma.

**Entra**
- Un párrafo nuevo en el cuerpo común `adapters/leader/bflow.md`, justo después de la viñeta `spawn`. Llega igual a la skill de Claude (`adapters/claude/embed.go`) y al comando `/bflow` de OpenCode (`adapters/opencode/embed.go`), porque los dos usan `leader.Render`.
- La regla dice: no hagas su trabajo (ni leer el repo en su lugar, ni escribir código, ni revisar) y no corras su `report`. Muestra el error tal cual. Da la causa probable: el modelo (`models.opencode` en `bflow.yaml` + `bflow render`, o `bflow doctor`) o la cuenta o región del proveedor. Pregunta con {{ask_tool}} si se relanza y, si el humano dice que sí, vuelve a lanzar el mismo agente con los mismos `args`.
- Una prueba `TestSkillReglaSubagenteFalla` en `adapters/leader/leader_test.go` que renderiza con los valores de `ask_tool` de Claude y de OpenCode y comprueba las palabras clave de la regla.
- Una entrada en `CHANGELOG.md`.

**No entra**
- Un comando `bflow` nuevo para relanzar o para registrar la falla. Relanzar se hace con el mismo `spawn`.
- Que bflow detecte la falla por su cuenta (por ejemplo, con un timeout o con un hook que note que falta el `report`).
- Cambios en `docs/guia.md`. La regla es una instrucción para el líder, no para la persona.

**Decisiones nuevas**
- [N1] Relanzar con el mismo `spawn` (mismo `subagent`, mismos `args` y mismo `report`). Descartado: un `bflow retry <id>`, porque es superficie nueva sin estado nuevo que guardar.
- [N2] La prueba usa `Render` dentro del paquete `leader`, con las dos cadenas de `ask_tool`. Descartado: importar `adapters/claude` y `adapters/opencode` desde un paquete de prueba externo; daría lo mismo con más acoplamiento.
- [N3] La regla va en el cuerpo general (después de `spawn`) y no en una sección `## …` de skill. Descartado: una sección nueva, porque ningún `next` trae `skill` para esta situación.

**Riesgos**
- Es texto para el modelo: puede ignorarlo igual. La prueba solo asegura que el texto exista.
- Un subagente que falla a medias (escribió archivos pero no reportó) se relanza sobre ese trabajo parcial. Los agentes ya admiten `resume`, así que se acepta.

**Tamaño:** S. Son unas 3 líneas de texto, una prueba y una entrada de changelog.

## Discovery

Carril light: no hay discovery. El scout ubicó el cuerpo común (`adapters/leader/bflow.md`), el patrón de prueba (`TestSkillReglaClear`) y los dos usos de `Render` (Claude y OpenCode).

## Requirements

## Design

## Tasks

- [ ] **T1** Regla en el cuerpo común. En `adapters/leader/bflow.md`, inmediatamente después de la viñeta `- \`spawn\`` (antes de `- \`wait\``), agrega una subviñeta o un párrafo con este contenido. La redacción puede ajustarse, pero estas palabras deben aparecer literalmente:
  - "falla sin reportar" (la condición: el agente terminó con error o sin correr su `report`);
  - "no hagas su trabajo", y que eso incluye no leer el repo en su lugar, no escribir código, no revisar y no correr su `report`;
  - "tal cual" (mostrar el error sin resumirlo);
  - "`models.opencode`" y "`bflow render`" (modelo no disponible), "`bflow doctor`" y "cuenta o región del proveedor" como causas probables;
  - "{{ask_tool}}" para preguntar si se relanza, y que solo con un sí se relanza el mismo agente con sus mismos `args` y `report`; si dice que no, se detiene y no avanza.

  Criterios:
  - CUANDO se lee `bflow.md`, DEBE contener cada frase anterior.
  - CUANDO se corre `Render`, no DEBE quedar ningún `{{…}}` sin reemplazar (`TestLeaderRender` sigue pasando).
- [ ] **T2** Prueba `TestSkillReglaSubagenteFalla` en `adapters/leader/leader_test.go` (package `leader`). Para cada `ask_tool` de `{"AskUserQuestion", "la herramienta \`question\`"}`, llama a `Render([]byte("---\n---\n\n"), map[string]string{"ask_tool": v})`. Luego, por cada palabra de `{"falla sin reportar", "no hagas su trabajo", "tal cual", "models.opencode", "bflow render", "bflow doctor", "cuenta o región del proveedor"}` y por el propio valor de `v`, hace `t.Errorf` si la salida no la contiene.
  - Criterio: CUANDO se quita la regla de `bflow.md`, la prueba DEBE fallar para los dos adaptadores.
  - Criterio: `go test ./adapters/...` y `go vet ./...` pasan.
- [ ] **T3** En `CHANGELOG.md`, en `## v0.1.0`, agrega en la subsección que toque (`### Cambiado`, o créala después de `### Agregado`) una viñeta: si un subagente falla sin reportar, el líder ya no hace su trabajo; muestra el error y la causa probable y lo relanza cuando el humano lo confirma. Hace falta correr `bflow render` (o reinstalar el adaptador) para regenerar la skill y el comando.
