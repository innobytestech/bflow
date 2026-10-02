# Cambios para consumidores · GH-8

## Nuevo comando y fase

### `bflow drop` · retira una tarea sin terminarla

**Nuevo comando.** Retira una tarea que ya empezó (en cualquier fase de trabajo) sin terminarla, la cierra en el tracker y la saca de `bflow status` sin reportar DONE. Requiere una nota que explique el motivo.

**Uso**

```bash
bflow drop <ID> --note "dividida en otras tareas"
bflow drop <ID> --note "cambio de alcance"
```

**Comportamiento**

- Solo funciona desde fases de trabajo (`discovery`, `spec`, `contract`, `implementing`, `paused`, `quality`, `documenting`, `walkthrough`, `in_review`). Una tarea ya `done` o `dropped` no se puede retirar nuevamente.
- No toca la rama ni el PR abierto; avisa si existen y los deja para cerrar a mano.
- Agrega un comentario en el tracker con la nota y marca la tarea como `dropped` (terminal, como `done`).
- Ejemplo de respuesta:

```json
{
  "ok": true,
  "code": "advanced",
  "data": {
    "id": "API-12",
    "phase": "dropped",
    "warnings": [
      "la rama feature/API-12-validar-rfc sigue existiendo; bórrala a mano si ya no sirve",
      "el PR https://github.com/org/api/pull/42 sigue abierto; ciérralo a mano si ya no sirve"
    ]
  }
}
```

**Permisos**

- Solo una persona puede correr este comando (no subagentes).

---

## Split: créación automática de tareas hijas

### Brief con `### División` crea las hijas al aprobar

**Nuevo en el flujo.** Cuando un gate `split` se aprueba, bflow crea automáticamente las tareas hijas listadas en el Brief bajo `### División`, las enlaza a la madre y marca la madre como `dropped`.

**Qué escribir en el Brief**

Agregá una sección `### División` (o `### Division`) con las tareas hijas en formato de lista:

```markdown
## Brief

Objetivo: validar... blah blah

### División

- **Validar RFC**: verificar que el RFC sea válido en la API del SAT
- **Guardar en BD**: persistir el RFC validado en la tabla clientes
- **Histórico**: registrar cambios en RFC para auditoría
```

Cada hija necesita:
- Título único de hasta 120 caracteres
- Alcance en dos a seis líneas

**Validación**

- Mínimo 2 tareas hijas, máximo 6.
- Cada título debe ser único dentro de la división.
- Cada título de hasta 120 caracteres.
- Cada alcance debe tener al menos una línea.
- Si falta o está mal escrita, `bflow report` rechaza con código 2.

**Proceso**

1. El agente spec-author escribe el Brief con la sección `### División`.
2. Reporta `READY` o `DONE`.
3. Una persona aprueba el gate `split`.
4. bflow crea las hijas en el tracker (en backlog, sin carril) usando el título como nombre y el alcance + referencia a la madre como descripción.
5. La madre pasa a `dropped` (retirada, terminal como `done`).
6. Las hijas aparecen en `bflow status` como tareas nuevas sin empezar.

**Ejemplo de response al aprobar**

```json
{
  "ok": true,
  "code": "advanced",
  "data": {
    "id": "API-8",
    "phase": "dropped"
  }
}
```

El comentario en el tracker en la madre lista las hijas:

```
**Dividida en:**
- API-9 · Validar RFC
- API-10 · Guardar en BD
- API-11 · Histórico
```

**Si alguna hija ya existe**

Si reintentás la aprobación (ejemplo: falló por un problema de red), bflow solo crea las que falten. No duplica tareas.

**Si el tracker no puede crear**

Si el tracker no tiene capacidad de crear tareas (ejemplo: adaptador local, que no es Creator), bflow rechaza sin cambiar nada:

```
error: el tracker local no permite crear tareas desde bflow; créalas a mano y retira API-8 con bflow drop
```

---

## Cambios internos (no afectan a consumidores)

- Nueva fase terminal `dropped` con los mismos permisos de `done`: no se puede cambiar de fase desde aquí.
- El evento `drop` en el flujo interno toma la nota como motivo del retiro.
- Event.Children contiene la lista de hijas creadas como `<ID> · <título>`.

---

## Fases en el tablero

Las tareas `dropped` no aparecen en `bflow status` y cierran el issue en GitHub/Plane (con `not_planned` en GitHub). Cuentan como cerradas en `bflow panel` y `bflow ui`.

La máquina de estados sigue igual; `dropped` es otro terminal como `done`.
