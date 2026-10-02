# Changelog · Consumidores GH-15

## Contrato `output.Next`

### Nuevo campo `clear`

**Antes:** ningún campo que indique si descartar la sesión.

**Después:** 
```json
{
  "action": "ask",
  "gate": "spec",
  "clear": true
}
```

**Qué cambió:** tras un `bflow approve` que avanza la fase (p. ej. spec → contract), `next.clear` es `true` si hay siguiente paso (no es `done`). En otros comandos (`reject`, `report`, `status`, `hook session-start`) o si no hay cambio de fase (gates `decision`, `questions`, `rounds`), el campo se omite (equivale a `false`).

**Por qué:** la sesión principal (skill bflow) lo interpreta como señal de que la conversación puede descartarse (via `/clear` en OpenCode o sesión nueva en Claude Code) y la sesión limpia retoma el trabajo sin perder el hilo (el hook `session-start` devuelve estado compacto).

**Qué hacer:** si tu herramienta interpreta `next`, ignora `clear` si no existe (está en `omitempty`). Si quieres aprovechar la economía de tokens, tu skill o plugin puede:
- detectar `clear: true`
- sugerir o ejecutar `/clear` (si la plataforma lo soporta)
- reanudar con el siguiente paso sin perder contexto (confiando en que el hook de inicio lo devuelve)

**Retrocompatibilidad:** el campo es opcional; consumidores existentes no ven cambio.

---

## Hook `bflow hook session-start`

### Líneas nuevas cuando hay tarea activa

**Antes:**
```
API-12 · spec · 42m · pendiente: spec
[breve actual]
```

**Después:**
```
API-12 · spec · 42m · pendiente: spec
[breve actual]
next: {"action":"ask","gate":"spec",...}
Sigue con /bflow.
```

**Qué cambió:** se añaden dos líneas:
- `next: <JSON compacto de una línea>` con el siguiente paso sin campo `display` (ya está en la línea anterior o disponible en `bflow show <id> brief`).
- `Sigue con /bflow.` como recordatorio.

**Cuándo:** solo cuando hay exactamente una tarea activa. Si no hay ninguna, hay varias en curso o hay error, se omiten las líneas de `next`.

**Por qué:** el plugin OpenCode inyecta esta salida al crear sesión raíz, sin pedirle respuesta al modelo; así la sesión limpia (tras `/clear`) sabe dónde estaba sin re-leer el tracker ni re-ejecutar comandos.

**Qué hacer:** si parseabas la salida del hook, busca la línea `next: ` y deserializa el JSON. Si no la esperas, ignórala (la salida es compatible con versiones anteriores).

---

## Plugin OpenCode

### Inyección de contexto en `session.created`

**Antes:** la sesión nueva no recibía contexto (el usuario tenía que escribir `/bflow` manualmente).

**Después:** al crear sesión raíz (sin `parentID`), el plugin corre `bflow hook session-start` e inyecta su salida como contexto con `noReply: true` (no interrumpe al modelo, no genera respuesta).

**Qué cambió:** el `client.session.prompt` se llama con `{ noReply: true, parts: [{ type: "text", text: <hook output> }] }`. Si falla (bflow no en PATH, `.bflow` no existe, timeout 10s), se omite sin error ni advertencia (ambos casos son normales, p.ej. al iniciar repo nuevo).

**Por qué:** el flujo se retoma automáticamente tras `/clear`, sin que el usuario copie el estado a mano.

**Qué hacer:** nada, es transparente. Si tu plugin personalizado está basado en este, actualiza la inyección para incluir las nuevas líneas.

---

## Notas de aprobación en `decisions.md`

### Nuevas notas en gate `spec`

**Antes:** `bflow approve <id> --gate spec --note "..."` registraba la nota solo en `log.jsonl`.

**Después:** la nota también se escribe en `decisions.md`, con esta línea:
```
- 2026-10-02 · gate spec: tu nota acá (usuario@email.com)
```

**Qué cambió:** archivo `decisions.md` aparece en `.bflow/tasks/<id>/` con encabezado `# Decisiones en vuelo` y una línea por nota. Se crea si no existe.

**Cuándo:** solo en gate `spec`; otros gates siguen registrando notas solo en `log.jsonl` (excepto `decision` y `questions` que tienen mecanismos propios).

**Por qué:** el PR incluirá las decisiones tomadas en vuelo en la descripción (listo para revisar sin releer el transcript).

**Qué hacer:** si lees `decisions.md`, espera líneas con formato `- FECHA · gate GATE: NOTA (USUARIO)`. Si no existe el archivo, la tarea no tiene notas de vuelo aún.

---

## Resumen

| Cambio | Consumidor | Acción |
|--------|------------|--------|
| `clear: true` en `next` | Cualquiera que interprete next | Ignorar si no existe (omitempty) |
| Líneas `next:` y `Sigue con /bflow.` en `hook session-start` | Scripts que parsean el hook | Parsear JSON si la línea existe |
| Inyección en `session.created` | Plugin personalizado OpenCode | Actualizar si está basado en el nuestro |
| Notas en `decisions.md` | Consumidor de decisiones en vuelo | Leer archivo si existe |
