# Cambios para consumidores · GH-32

`bflow panel` ahora cierra automáticamente las tareas locales que se terminaron fuera de esta copia: si el tracker las marca como done/cerradas, o si su PR de rama se mergeó. Esto aplica a cualquier fase, no solo `in_review`.

## Panel cierra tareas terminadas en el tracker

**Cambio de comportamiento.** Panel ahora reconcilia el estado local con el tracker antes de listar compromisos. Si una tarea está en cualquier fase local (ej. `implementing` con gate abierto) pero el tracker la da por terminada (done o cerrada), panel la cierra localmente y la lista en `closed_outside`.

**Antes (quedan abiertas)**

```bash
bflow panel
# Salida:
# GH-13 se terminó en otra máquina, quedó cerrada en GitHub pero local sigue en implementing/decision
# Aún aparecería en compromisos pendientes si había un gate abierto
```

**Ahora (se cierran)**

```bash
bflow panel
# Salida:
# GH-13 cerrada: terminada fuera de esta copia (tracker)
# Ya no aparece en compromisos pendientes
```

El cierre registra un evento `closed_outside` en el log con motivo `tracker`, descarta los efectos pendientes (para no revertir cambios en el tracker) y no reintenta ningún comentario o transición.

## Panel cierra tareas cuyo PR se mergeó (en cualquier fase)

**Cambio de comportamiento.** Si una tarea no está en `in_review` pero su rama tiene un PR cerrado y mergeado, panel la cierra con motivo `pr` y mueve el tracker a `done`.

**Antes (no se cerraban)**

```bash
# En otra máquina:
bflow panel  # vio que el PR #30 se mergeó y cerró GH-13 en in_review

# En esta máquina:
bflow panel
# GH-13 estaba en implementing/decision, el PR #30 ya estaba mergeado
# Sin cambios: seguía en implementing pendiente de aprobación
```

**Ahora (se cierran)**

```bash
# En esta máquina:
bflow panel
# GH-13 cerrada: terminada fuera de esta copia (PR #30 mergeado)
# Se mueve el tracker a done igual que un merge normal en in_review
```

El cierre registra un evento `closed_outside` con motivo `pr`, guarda el número y URL del PR, y emite `FxTrackerState` para llevar el tracker a `done`.

## Cambios en el JSON de Panel

**Cambio de API.** La respuesta de `Panel()` incluye un nuevo campo:

**Antes**

```json
{
  "items": [...],
  "closed": ["GH-12"],
  "reminded": [],
  "warnings": []
}
```

**Ahora**

```json
{
  "items": [...],
  "closed": ["GH-12"],
  "closed_outside": [
    {
      "id": "GH-13",
      "from": "implementing",
      "reason": "tracker"
    },
    {
      "id": "GH-11",
      "from": "paused",
      "reason": "pr",
      "pr": 30
    }
  ],
  "reminded": [],
  "warnings": []
}
```

**Qué tiene que hacer el consumidor**

Si tu código consume la salida de `bflow panel`:

1. **Actualiza tu parser JSON** para ignorar o procesar el nuevo campo `closed_outside`:
   - Si ignoras: no hay cambio (es un array opcional).
   - Si lo procesas: itera `closed_outside` igual que `closed`, pero nota que tiene campos adicionales (`from`, `reason`, `pr`).

2. **Combina las listas en tu UI** si quieres mostrar todas las tareas cerradas:
   ```javascript
   const allClosed = [...panelReport.closed, ...panelReport.closed_outside.map(c => c.id)];
   ```

3. **El motivo importa**: Si registras por qué se cierran las tareas, diferencia:
   - `reason: "tracker"` — el tracker ya las había terminado.
   - `reason: "pr"` — el PR se mergeó fuera.

## Cambios en el log y métricas

**Cambio de formato.** El archivo `.bflow/tasks/<ID>/log.jsonl` ahora incluye un evento `closed_outside`:

```json
{
  "ts": "2026-10-01T14:30:00Z",
  "id": "GH-13",
  "event": "closed_outside",
  "from": "implementing",
  "to": "done",
  "by": "system",
  "data": {
    "reason": "tracker",
    "tracker_state": "done"
  }
}
```

Con motivo `pr`:

```json
{
  "event": "closed_outside",
  "from": "paused",
  "to": "done",
  "data": {
    "reason": "pr",
    "pr": 30
  }
}
```

**En `bflow stats`**, las tareas que se cierran desde afuera añaden un campo `closed_outside` (motivo) y no cuentan el tiempo previo como trabajo de agente ni espera humana:

```bash
bflow stats GH-13
# Antes:
# Fase: implementing (5h12m), espera humana (2d3h)
# Cerrada: No (gate decision abierto)

# Ahora:
# Fase: implementing (no contabilizado)
# Cerrada: tracker (GH-13 terminada fuera de esta copia)
```

## Nuevas operaciones en VCS Host

**Cambio de interfaz.** El interfaz `vcs.Host` (adaptadores de GitHub, GitLab, etc.) ahora debe implementar:

```go
FindMergedPR(ctx context.Context, head string) (PR, error)
```

Devuelve el primer PR cerrado y mergeado cuya rama origen sea `head`, o `ErrNoPR` si no hay.

**Qué tiene que hacer el consumidor**

Si implementas un adaptador de VCS:

1. Agrega `FindMergedPR` a tu implementación de `Host`.
2. Busca PRs cerrados (no solo abiertos) cuya rama sea `head`.
3. Filtra por `merged_at != nil` (o equivalente en tu API) para validar que está mergeado.
4. Devuelve el primero o `ErrNoPR`.

Ejemplo para GitHub:

```go
func (c *Client) FindMergedPR(ctx context.Context, head string) (vcs.PR, error) {
  owner := strings.SplitN(c.Repo, "/", 2)[0]
  q := url.Values{"head": {owner + ":" + head}, "state": {"closed"}}
  var ps []apiPR
  if err := c.do(ctx, "GET", c.pulls()+"?"+q.Encode(), nil, &ps); err != nil {
    return vcs.PR{}, c.noAccess(err)
  }
  for _, p := range ps {
    if pr := p.pr(); pr.Merged {
      return pr, nil
    }
  }
  return vcs.PR{}, vcs.ErrNoPR
}
```

## Limitaciones y casos de red

**Errores de red solo avisan.** Si el tracker o el host no responden, panel avisa pero no cierra:

- `ErrNoCredentials`: sin token → aviso, sin cierre.
- `ErrNoPR`: sin PR mergeado → aviso, sin cierre.
- Timeout o error de conexión → aviso, sin cierre.

El cierre de `in_review` por merge sigue igual: una tarea en `in_review` con PR mergeado se cierra como antes (evento `merged`, no `closed_outside`).

## Checklista para empezar

- [ ] Si consumes `Panel()` en JSON: actualiza tu parser para el nuevo campo `closed_outside`.
- [ ] Si registras cierres: diferencia por motivo (`tracker` vs `pr`).
- [ ] Si implementas un VCS Host: agrega `FindMergedPR`.
- [ ] Si lees métricas: ignora o procesa el campo `closed_outside` en `TaskStats`.

