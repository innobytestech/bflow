# Cambios para consumidores · GH-35

Las preguntas de producto ahora rechazan opciones que delaten cuál es la respuesta, y el orden de las opciones se mezcla de forma estable.

## Las opciones delatoras se rechazan

**Cambio.** Si una opción de `## Preguntas de producto` en tu review-map contiene marcas que delaten cuál hace el código ("(lo que hace el código)", "(actual)", "(implementado)", ✓…), `bflow report APPROVED` rechaza con `review_incomplete` y lista la opción para que la reescriba.

**Antes (opciones delatoras permitidas)**

```markdown
## Preguntas de producto

¿Qué pasa con tareas anteriores a GH-29?
- Muestra "sin detalle por llamada" (lo que hace el código)
- Reconstruir desde transcripts
```

Aunque la opción dejaba clara cuál era la respuesta, bflow las permitía. La comparación con el código no servía de nada.

**Ahora (opciones delatoras rechazadas)**

Si escribes lo mismo:
```bash
bflow report APPROVED
# Error: review_incomplete

# Estas opciones de `## Preguntas de producto` delatan cuál hace el código;
# reescríbelas neutras, sin marcas ni menciones al código:
#   - Muestra "sin detalle por llamada" (lo que hace el código)
```

El reviewer rechaza tu APPROVED y te pide que reescriba la opción sin la marca `(lo que hace el código)`.

**Qué tiene que hacer el consumidor**

Cuando escribas preguntas de producto, asegúrate de que ninguna opción menciona:
- Marcas de corrección: `✓`, `✔`, `✅`
- Referencias al código: `el código`, `lo que hace`, `implementad[oa]s?`, `implementación`
- Estado actual: `actual`, `hoy`, `correct[oa]s?`, `recomendad[oa]s?`, `esperad[oa]s?`

**Ejemplo correcto:**
```markdown
## Preguntas de producto

¿Qué pasa con tareas anteriores a GH-29?
- Muestra "sin detalle por llamada"
- Reconstruir desde transcripts
```

**Códigos de error nuevos**

- `review_incomplete`: El APPROVED fue rechazado porque hay opciones delatoras. Reescríbelas sin marcas y vuelve a reportar.

## El orden de las opciones se mezcla

**Cambio.** `bflow show <ID> questions` mezcla el orden de las opciones de cada pregunta, de forma que no sea predecible ni siempre la primera sea la del código.

**Antes (orden original)**

Las opciones se mostraban en el orden del review-map:
```bash
bflow show GH-29 questions

## Preguntas de producto

¿Qué pasa con tareas anteriores a GH-29?
- Muestra "sin detalle por llamada"         ← siempre primera
- Reconstruir desde transcripts
```

Como la opción del código siempre estaba primero, era predecible.

**Ahora (orden mezclado y estable)**

Las opciones se mezclan, pero el orden no cambia entre llamadas:
```bash
bflow show GH-29 questions

## Preguntas de producto

¿Qué pasa con tareas anteriores a GH-29?
- Reconstruir desde transcripts           ← orden 1 (mezclado)
- Muestra "sin detalle por llamada"       ← orden 2

bflow show GH-29 questions  # segunda llamada

## Preguntas de producto

¿Qué pasa con tareas anteriores a GH-29?
- Reconstruir desde transcripts           ← mismo orden
- Muestra "sin detalle por llamada"
```

La semilla de la mezcla es un hash estable del id de la tarea y el texto de la pregunta, así que el orden no cambia entre llamadas; pero tampoco es alfabético ni predecible.

**Qué tiene que hacer el consumidor**

Nada. El cambio es transparente: las opciones se muestran en un orden diferente, pero tu flujo no cambia. Si usas `bflow show <ID> questions` en documentación o scripts, el orden es ahora determinista pero distinto; actualiza tus ejemplos si es necesario.

## Checklista

Si escribes preguntas de producto en un review-map:

- [ ] Revisa que ninguna opción contiene marcas (`✓`, `✔`, `✅`) ni referencias al código o estado actual
- [ ] Si bflow rechaza con `review_incomplete`, reescribe la opción sin la marca y vuelve a reportar APPROVED
- [ ] Cuando uses `bflow show <ID> questions`, recuerda que el orden de las opciones ahora es mezclado pero estable

