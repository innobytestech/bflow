# Changelog para consumidores — GH-16

## Configuración: presupuesto de tokens por fase

### Agregado

- **`flow.budget`** en `bflow.yaml`: presupuesto de tokens nuevos por fase.
  
  Sintaxis: mapa de fase a cifra (entero o con sufijo `k`/`K` para ×1e3, `m`/`M` para ×1e6, decimal opcional):
  
  ```yaml
  flow:
    budget:
      implementing: 200k          # 200,000 tokens nuevos
      quality: 1.5M               # 1,500,000 tokens nuevos
      spec: 100000                # Entero sin sufijo
  ```
  
  Fases válidas: Discovery, Spec, Contract, Implementing, Paused, Quality, Documenting, Walkthrough.
  
  Sin entrada para una fase: sin presupuesto, sin gate de pregunta.

- **`flow.loop_repeats`** en `bflow.yaml`: frena la N-ésima repetición idéntica de un subagente.
  
  ```yaml
  flow:
    loop_repeats: 2              # Frena a la segunda repetición (0 = apagado, default)
  ```
  
  El guard rechaza cuando un subagente repite idénticamente Bash, Read, Edit o Write en la misma corrida (huella: sha256 de herramienta + entrada cruda). Un Edit o Write permitido reinicia la cuenta de Bash y Read (no frena relanzamiento de pruebas tras un cambio).

## Comandos: gate `budget`

### Cambios

- **`bflow approve [ID] --gate budget`** y **`bflow reject [ID] --gate budget --note <motivo>`**:
  
  Nuevo gate `budget` que pregunta al persona cuando una fase supera su presupuesto (1×, 2×, 3×... en `flow.budget`).
  
  - `approve`: "Sí, seguir"; continúa la fase sin bloqueo.
  - `reject --note <motivo>`: "No, parar"; bloquea la tarea con la nota. La fase no avanza hasta que alguien corra `bflow unblock`. El motivo queda en el log y el implementer lo verá.
  
  El gate solo aparece en fases presupuestadas (`flow.budget` tiene la fase) que no son terminales (Done, Dropped, InReview, Blocked, Backlog). Múltiplos respondidos no vuelven a preguntar hasta el siguiente múltiplo.

## Panel: línea de presupuesto y evento de ciclo

### Cambios

- **`bflow watch` muestra "presupuesto" por fase configurada.**
  
  Una línea `presupuesto: <usado> / <presupuestado> nuevos` por cada fase en `flow.budget`. Si el gasto supera el presupuesto, la línea se marca en rojo brillante (`1;31`).
  
  Ejemplo:
  ```
  presupuesto  implementing 142.1k / 200k nuevos
  presupuesto  quality 1.8M / 1.5M nuevos    (← en rojo)
  ```

- **`bflow watch` avisa de ciclos detectados.**
  
  En el log de eventos, ciclos rechazados por el guard aparecen como:
  ```
  ciclo: <subagente> <herramienta> repetida N veces
  ```
  
  Ejemplo: `ciclo: GH-16-implementer Bash repetida 2 veces`

## Qué haces en tu cliente

1. Actualiza `bflow.yaml` con `flow.budget` y `flow.loop_repeats` si los necesitas.
2. Si tu cliente llama `bflow approve/reject`, aguanta el nuevo parámetro `--gate budget`.
3. En el panel (`bflow watch`), aguanta las líneas nuevas de "presupuesto" y los eventos de "ciclo" en el log.
4. Al implementer: cuando `bflow next` lleve `gate: budget`, la pregunta es "¿Seguir?" tras haber superado el presupuesto de esa fase. `bflow approve --gate budget` o `bflow reject --gate budget --note <motivo>`. No es un rechazo: solo confirma que quiere continuar, o pide parar.

## Sin cambios

- Fases, carriles, gates existentes, comandos `start`, `report`, `status`, `pr`.
- Pruebas congeladas, check, validación de tipos.
- Tokens de sesión principal (solo suma nueva por fase en métrica; el presupuesto no corta trabajo).
