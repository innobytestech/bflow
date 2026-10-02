# Changelog: Medición de lecturas del reviewer

## Resumen

El reviewer ahora se rechaza con `review_incomplete` si no abrió los archivos marcados con 🔴 en el review-map antes de aprobar. El documenter se rechaza con `docs_pending` si una documentación listada en `## Docs` no cambió en la rama ni se justificó en `reports/docs.md`. El hook `bflow guard --reads` mide qué archivos del diff abrió el reviewer en quality.

## Cambios que rompen compatibilidad

### Nuevos códigos de rechazo en `bflow report --agent reviewer`

- **`review_incomplete`** (código 2): El reviewer reportó APPROVED pero:
  - Falta abrir un archivo de una viñeta 🔴 que está en el diff (solo si la cobertura se mide con `--reads`).
  - Una viñeta 🔴 no empieza con la ruta en backticks (ej. `` - `src/main.go`: … ``).
  - El review-map no tiene la sección `## Docs`.

  Ejemplo de rechazo:
  ```
  review_incomplete: 3 viñetas sin ruta inicial; falta leer src/main.go, internal/review/review.go
  ```

  **Qué hacer:** El reviewer debe editar el review-map para:
  1. Agregar rutas en backticks al inicio de cada viñeta 🔴 (ej. `` - `src/main.go`: cambio… ``).
  2. Agregar la sección `## Docs` (puede estar vacía: `- ninguna`).
  3. Si hace falta, abrir los archivos con `git diff`, `cat`, `Read` u otra herramienta de bflow.

### Nuevo código de rechazo en `bflow report --agent documenter`

- **`docs_pending`** (código 2): El documenter reportó DONE pero una ruta de `## Docs` no cambió en la rama ni aparece en `reports/docs.md`.

  Ejemplo de rechazo:
  ```
  docs_pending: falta actualizar o justificar: docs/api.md, README.md
  ```

  Formato esperado en `reports/docs.md`:
  ```markdown
  - `docs/api.md`: sin cambio: se documentó en spec.md
  - `README.md`: sin cambio: no aplica al cambio
  ```

  **Qué hacer:** El documenter debe:
  1. Actualizar los archivos listados en `## Docs`, o
  2. Agregar una línea en `reports/docs.md` con el formato `` - `ruta`: sin cambio: <motivo> ``.

## Cambios sin ruptura de compatibilidad

### Hook `bflow guard --reads`

El hook de guardia ahora acepta el flag `--reads` e registra en `.bflow/tasks/<ID>/reads.jsonl` qué archivos abrió el reviewer en quality (vía `Read`, `git diff`, `cat`, etc.). Sin el flag, la cobertura queda "no medida" y no se exige en APPROVED.

**Instalación automática:** Corre `bflow install claude` (en Claude) o `bflow render` (en OpenCode) dentro del repo.

### Nuevo output en el walkthrough (`bflow report --agent gate walkthrough`)

Al inicio del walkthrough, aparece:
```
**Cobertura del reviewer:** el reviewer leyó N de M archivos del diff
- archivos no leídos: src/main.go, internal/review/review.go
- rutas 🔴 fuera del diff: docs/old.md
```

O si no hay cobertura:
```
**Cobertura del reviewer:** no medida
```

### Nuevo output en métricas (`bflow stats <ID>`)

Aparece una nueva línea:
```
  revisión: el reviewer leyó 5 de 8 archivos del diff
```

Con `--json`, en `data.stats.review`:
```json
{
  "stats": {
    "review": {
      "measured": true,
      "total": 8,
      "read": 5,
      "unread": ["src/main.go", "internal/review/review.go"],
      "unread_other": 2
    }
  }
}
```

Sin cobertura, no aparece la línea ni la entrada en JSON.

### Formato del review-map

Ahora se require:
- Cada viñeta 🔴 debe empezar con rutas en backticks separadas por comas (ej. `` - `src/main.go`, `internal/review/review.go`: cambio… ``).
- El review-map debe tener una sección `## Docs` (puede estar vacía: `- ninguna`).

Ejemplo:
```markdown
## 🔴 Cambios críticos
- `src/main.go`: nueva lógica de medición

## 🟡 Cambios normales
- `internal/review/review.go`, `internal/review/types.go`: tipos y funciones públicas

## Docs
- `docs/guia.md`: actualizar guía de instalación
- `README.md`: tabla de garantías
```

## Migración

1. **En `bflow install claude`:** El hook ya viene con `--reads`. Si actualizas una instalación anterior, corre nuevamente.
2. **En `bflow render`:** El plugin de OpenCode se actualiza automáticamente con `--reads`.
3. **En review-maps existentes:** Si ya tienes rutas en backticks, no hay cambio. Si no, agrégalas antes de que el reviewer apruebe.
4. **En documenter:** Asegúrate de que `## Docs` exista en el review-map. Si una ruta no aplica al cambio, justifícala en `reports/docs.md`.
