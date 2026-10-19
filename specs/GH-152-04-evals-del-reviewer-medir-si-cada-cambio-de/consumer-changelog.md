# Cambios para consumidores: GH-152 (eval del reviewer)

No rompe la salida existente. Hay un comando nuevo, un agente nuevo que se genera solo con `bflow render` y un cambio de comportamiento en `report DONE` (ver abajo). Tras actualizar, corre `bflow render` y reinicia la sesión de la herramienta.

## Cambio de comportamiento: `freeze --allow`

Antes, un `report --agent implementer --verdict DONE` que se rechazaba (por ejemplo `tasks_open`, `no_new_commits` o `repro_test_missing`) ya consumía el permiso de `freeze --allow`. Ahora el permiso se consume solo cuando el DONE se acepta.

- Antes: DONE rechazado, luego `freeze --allow` gastado; el siguiente intento de editar la prueba congelada se bloqueaba.
- Ahora: DONE rechazado, el permiso sigue disponible para el siguiente DONE.

Qué tiene que hacer el consumidor: nada, salvo si su flujo asumía que el permiso se perdía tras un rechazo.

## Comando nuevo: `bflow eval reviewer`

Mide qué defectos conocidos atrapa el reviewer. Los casos viven en `.bflow/evals/reviewer/<caso>/case.yaml`; el resultado queda en `.bflow/evals/<AAAA-MM-DD-HHMMSS>.json`.

Flags: `--craft <ref>`, `--case <nombre>` (repetible), `--abort`, `--from-fixes`.

Salida (JSON compacto; `ok`, `code`, `data`, `next`). Cada llamada devuelve uno de estos códigos:

- `eval_spawn` (ok): hay un caso por lanzar. `next.action` es `spawn`, `next.agents[0].subagent` es `bflow-eval-reviewer` y `args` trae `id`, `base`, `worktree`, `out`, `craft` y `spec` si el caso lo declara. El consumidor lanza ese agente y vuelve a correr `bflow eval reviewer`.
- `eval_done` (ok): corrida cerrada. `data.result.path` apunta al JSON y `data.result.totals` trae `defects`, `caught`, `missed`, `false_positives`, `tokens` y `seconds`.
- `eval_aborted` (ok): `--abort`; no escribe resultado. Sin corrida abierta responde ok sin hacer nada.
- `eval_from_fixes` (ok): `data.created` lista los esqueletos nuevos; vacío con aviso si no hay hotfix con `--fixes`.
- `eval_reported` (ok): `bflow eval reviewer report <caso> --verdict APPROVED|REJECTED [--note t]` califica el caso en curso; `data.score` trae `caught`, `missed` y `false_positives`.

Rechazos (código de salida 2, `ok: false`, `code` y motivo en `reason`):

| `code` | Cuándo |
|---|---|
| `case_invalid` | Un caso sin `base`/`head`, con un commit que no resuelve, sin defectos, con una ruta vacía, absoluta o con `..`, o con un rango que no es `N` ni `N-M`. Una línea por problema en `data.reason`. Nada se crea. |
| `craft_ref_unknown` | `--craft` no resuelve a un commit (un ref que empieza con `-` cuenta). |
| `craft_missing` | El ref no contiene `internal/agents/craft/reviewer.md`. |
| `no_cases` | No queda ningún caso para correr, o `--case` nombra uno que no existe. |
| `eval_agent_missing` | No existe el agente `bflow-eval-reviewer` generado. Motivo: correr `bflow render` y reiniciar la sesión. |
| `eval_open` | Hay corrida abierta y se pasó `--craft`, `--case` o `--from-fixes`. Nombra la corrida. Termínala con `bflow eval reviewer` o cancélala con `--abort`. |
| `review_map_missing` | `report` sin `<out>/review-map.md`. |
| `case_not_pending` | `report` de un caso que no es el que está en curso. |
| `eval_not_running` | `report` sin corrida abierta. |
| `bad_verdict` | `--verdict` distinto de `APPROVED` o `REJECTED`. |

Los casos con `draft: true` se omiten y aparecen en `data.skipped`. Los avisos van en `data.warnings`.

### Forma del JSON de resultado

`.bflow/evals/<fecha>.json`:

- `kind` (`"reviewer"`), `run`, `version` de bflow, `model`.
- `craft`: `ref` (`""` si fue el árbol) y `sha256` del oficio.
- `cases[]`: `case`, `verdict`, `note`, `caught[]`, `missed[]`, `false_positives[]`, `spawned`, `reported`, `seconds`, `model`, `tokens`, `tokens_available`.
- `totals`: `defects`, `caught`, `missed`, `false_positives`, `tokens`, `seconds`.

Para comparar oficios se leen los `totals` de cada archivo. No hay comando que compare dos resultados.

## Agente nuevo: `bflow-eval-reviewer`

`bflow render` lo genera (Claude Code y OpenCode) solo si existe `.bflow/evals/reviewer/`. Usa el modelo y el esfuerzo de `agents.reviewer`. Si el repo no tiene casos, `bflow render` no lo crea y no lo marca como conflicto.

## Configuración de Git

- `.bflow/.gitignore` agrega `!evals/` y `!evals/**`. Lo hace bflow solo.
- Si el `.gitignore` de la raíz ignora `.bflow/` entero, `bflow eval reviewer` lo avisa en `data.warnings`. Para versionar los casos, cámbialo a `.bflow/*` con `!.bflow/evals/`.
- El guard deja a los subagentes escribir `.md` directos en `.bflow/cache/evals/<corrida>/<caso>/out/` y hacer `git add` (sin `-f`) de `.bflow/evals/`. Lo demás de `.bflow/` sigue negado.
- El chequeo de `report DONE` que rechaza archivos de `.bflow/` en el diff deja pasar `.bflow/evals/`: los casos del eval se versionan a propósito.
