# Cambios para consumidores · GH-26

## Comportamiento

### Subagentes no pueden responder compuertas

**Cambio de comportamiento.** Los subagentes (cualquier herramienta que se ejecute con `agent_type` en el hook de Claude Code, como `bflow-documenter`, `bflow-implementer` o tus agentes generados) ya no pueden ejecutar los comandos que responden una decisión humana: `bflow approve`, `bflow reject`, `bflow unblock`, `bflow start` y `bflow new`.

**Antes (permitido)**

Un subagente podía correr:
```bash
bflow approve <ID> --gate walkthrough
bflow reject <ID> --gate contract --note "Falta completar..."
bflow start <ID>
bflow new --lane full --title "Nueva tarea"
```

y el comando se ejecutaba normalmente.

**Ahora (negado)**

Cuando un subagente intenta ejecutar uno de esos comandos, el guard lo bloquea con exit code 2:

```
error: guard bloqueó: human_only
bflow approve responde una decisión de una persona: lo corre la sesión principal después de preguntarle.
Termina tu parte con `bflow report` (o NEEDS_DECISION).
```

Este bloqueo aplica incluso con rutas (`./bin/bflow`), `env`, `go run`, o comandos encadenados (`&&`, `||`, `;`).

**Qué tiene que hacer el consumidor**

Los subagentes ahora deben terminar su trabajo con `bflow report` en lugar de intentar aprobar compuertas:

```bash
# En lugar de:
bflow approve <ID> --gate walkthrough

# Usa:
bflow report <ID> --verdict DONE --gate walkthrough
```

La sesión principal (sin `agent_type`, es decir, la que pregunta al humano y lanza los agentes) sigue pudiendo ejecutar estos comandos sin cambios.

**Excepciones**

- Los demás comandos de bflow que los subagentes usan (`report`, `show`, `block`, `status`, `check`, `task add`, etc.) siguen funcionando normalmente.
- Los subagentes pueden usar `bflow new` solo si no hay tarea activa y lo redirigen a través de la sesión principal (si es que es aplicable a tu flujo).

**Logging**

Si la tarea se está rastreando en `.bflow/`, el evento `guard` en el JSONL incluye qué subagente intentó el comando bloqueado, y `bflow watch` lo muestra:

```
guard bloqueó a bflow-documenter: human_only
```

