# Por qué existe bflow y hacia dónde va

Soy Alfonso, de [Innobytes](https://innobytes.tech). Hice bflow porque el proceso con agentes de IA que usábamos en nuestros repos dependía de que el modelo se acordara de él.

## El problema

Trabajar con agentes de IA en proyectos reales suele terminar en un *harness*: prompts, subagentes y scripts que describen el proceso ("primero pregunta, luego escribe la spec, no edites pruebas, pide revisión…"). Yo tuve uno en dos repos reales, uno de backend y otro de frontend. Los problemas eran siempre los mismos:

- **Las reglas vivían en prosa.** "OBLIGATORIO", "NUNCA", "no saltes la compuerta". El modelo las respetaba casi siempre, y justo ese "casi" es donde se cuelan los errores caros.
- **El modelo movía el estado a mano.** Cambiaba estados en el tracker, creaba ramas y armaba URLs de PR, cada vez a su manera. En un repo el flujo estaba validado por scripts; en el otro, no.
- **Gastaba tokens en trabajo mecánico.** Leer el tracker, releer bitácoras, interpretar la salida de 4 comandos de pruebas, mantener archivos de estado. En uno de los repos, unos 7.000 tokens fijos por sesión antes de hacer nada útil.
- **Llenaba el repo de estado de trabajo.** Entre el 73% y el 80% de los archivos del harness versionados eran bitácoras, reportes y estado, no especificaciones.
- **Cada repo tenía su copia.** Mantenerlas sincronizadas era tedioso y se desviaban: estados distintos, prefijos de rama distintos, reglas que un repo tenía y el otro no.

## La meta

Que una persona decida lo importante (qué se construye, el contrato, qué se mergea) sin leer todo lo que genera el agente, y que el resto del proceso no dependa de la memoria del modelo.

## La idea

Separar lo que **requiere criterio** de lo que es **determinista**:

- El **agente** entiende el problema, diseña, programa y revisa.
- El **CLI** lleva el estado, las transiciones, git, el tracker, las pruebas y las reglas.

El agente no tiene que recordar el proceso: le pregunta a `bflow` qué sigue y lo hace. Un solo binario se instala una vez por máquina; cada repo solo trae un `bflow.yaml` corto. Los conceptos están en [conceptos.md](conceptos.md).

## Estado

bflow es una **prerelease: v0.1.0-rc.9**. Funciona de punta a punta con el tracker local, Plane y GitHub Issues, y con Claude Code y OpenCode. La API de comandos puede cambiar antes de la 1.0.

## Rumbo

Lo de abajo es el plan, no lo ya hecho. Cada fase enlaza su milestone en GitHub.

| Hito | Criterio de salida | Milestone |
|---|---|---|
| v0.1.0 | Primera release anunciada: intake, GitHub Issues, OpenCode, instalación en un paso, traspaso y cifras reales. | [v0.1.0](https://github.com/innobytestech/bflow/milestone/1) |
| Fase 1 · Legible para el mundo | Un dev angloparlante instala, corre `bflow init` y recorre una tarea con el tracker local sin ver una palabra en español. | [Fase 1](https://github.com/innobytestech/bflow/milestone/2) |
| Fase 2 · Que exista para alguien | Artículo publicado, bflow en al menos dos comparativas, repo en inglés. | [Fase 2](https://github.com/innobytestech/bflow/milestone/3) |
| Fase 3 · Adopción en equipo | Un equipo de tres personas con Linear y Claude Code lo usa de punta a punta (adaptador de Linear, Codex, métricas en el PR, política de estabilidad). | [Fase 3](https://github.com/innobytestech/bflow/milestone/4) |
| Fase 4 · Medir y decidir | Datos para decidir el índice de contexto y señales de adopción. | [Fase 4](https://github.com/innobytestech/bflow/milestone/5) |
| 1.0 | API de comandos estable. | Sin milestone todavía |

### Módulos del producto

1. Hecho: motor de flujo (estado, gates, tracker local, Plane y GitHub Issues y Projects, git y GitHub).
2. Hecho (versión inicial): guardas (git destructivo, `.env`, `.bflow/`, pruebas congeladas incluso sin hooks, ramas y PR a mano, tamaño de diff, agentes que terminan sin reportar).
3. En curso: distribuidor. `bflow render` genera los agentes para Claude Code y OpenCode; hay binarios por plataforma con checksums y atestación, instaladores y `bflow update`. Pendiente: Codex y la firma de código en Windows y macOS (falta el certificado).
4. Hecho: métricas (tiempo, tokens por fase, agente y modelo, calidad y fricción).
5. Previsto: contexto, un índice del código para el planner y el reviewer.
6. Previsto: planeación, ordenar el backlog y repartirlo en ciclos balanceados.

Siguiente: [instalación](instalacion.md) · [cómo probar y reportar](como-probar-y-reportar.md).
