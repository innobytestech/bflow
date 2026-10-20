# GH-171 · Comparar el reviewer actual contra el de rc.5 con el eval

## Brief

**Objetivo:** correr `bflow eval reviewer` con el oficio actual y con el de `v0.1.0-rc.5` sobre los mismos 5 casos y el mismo modelo, versionar los dos resultados y dejar en el issue la cifra comparativa para el README (#160).

**Entra**
- Dos corridas completas sobre los 5 casos de `.bflow/evals/reviewer/` (gh-103, gh-15, gh-151, gh-159, gh-16): una sin `--craft`, otra con `--craft v0.1.0-rc.5`.
- Commit de los dos `.bflow/evals/<run>.json` en la rama de esta tarea (`.bflow/.gitignore` ya permite `!evals/`).
- Corrección en esta rama de cada resultado raro de las corridas, tocando `internal/eval`, `internal/cli/evalcmds.go` o los `case.yaml` según haga falta, con prueba que lo cubra; la corrida afectada se repite con el binario corregido.
- Comentario en GH-171 con atrapados, no atrapados, posibles falsos positivos, tokens y segundos de cada corrida, y la frase "el reviewer actual atrapa X de Y; el de rc.5, Z de Y".

**No entra**
- Cambios en `internal/eval`, `internal/cli/evalcmds.go` o los `case.yaml` que no vengan de corregir un resultado raro de estas corridas (ver N4).
- Quitar las gates de contrato y pausa (#153): esta medición es el "antes".

**Decisiones nuevas**
- [N] Binario: `go install ./cmd/bflow` desde `main` actual (incluye GH-170) antes de correr; las dos corridas usan ese mismo binario. Descartado: el binario ya instalado, que puede ser anterior a GH-170 y contar distinto.
- [N] Modelo: el que `bflow.yaml` asigna a `reviewer` (`agents.Models(...)["reviewer"]`), sin tocar la config entre corridas; el campo `model` de ambos JSON debe coincidir. Descartado: fijar un modelo nuevo para esta tarea.
- [N] Tokens y tiempo salen de `totals.tokens` y `totals.seconds` de cada JSON; si algún caso trae `tokens_available: false`, se dice en el comentario. Descartado: medirlos aparte.
- [N] Resultado raro (referencia no reconocida, caso sin reporte, modelo distinto): se corrige en esta misma rama (código en `internal/eval` / `internal/cli/evalcmds.go` o datos en `case.yaml`), con prueba, y se repiten ambas corridas con el binario corregido para que comparen lo mismo; el comentario lo resume en "Seguimiento". Si la causa es del oficio de rc.5 (no del eval), no se toca el oficio: se anota. Descartado: solo anotarlo y abrir issue aparte.
- [N] "Posible falso positivo" = cada entrada de `false_positives` del JSON, listada tal cual; no se juzga a mano si es real.

**Riesgos**
- El oficio de rc.5 no conoce `bflow eval reviewer report`: si no reporta, el caso queda sin veredicto. Si el eval puede recoger el veredicto de otra forma se corrige aquí; si no, la cifra se da sobre los casos válidos, diciéndolo.
- Las correcciones pueden crecer: si una pide más que un ajuste acotado del eval, se para y se reporta como decisión.
- Variación del modelo entre corridas: una sola corrida por oficio; la frase lo aclara como medición puntual.
- Costo en tokens: 10 ejecuciones de reviewer.

**Tamaño:** S-M (operativo; código solo si aparece un resultado raro).

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Criterios EARS numerados R1..Rn, cada uno [D] (del discovery) o [N] (nuevo). -->

## Design

<!-- Estructuras, firmas, decisiones tomadas y descartadas, superficie de seguridad. -->

## Tasks

- [ ] T1 Preparar el binario: en `main` actualizado, `go install ./cmd/bflow` y `bflow render` (que exista `.claude/agents/bflow-eval-reviewer.md`). Criterio: `bflow eval reviewer --abort` no deja corrida abierta y `bflow version` corresponde al commit de `main`.
- [ ] T2 Corrida con el oficio actual: `bflow eval reviewer` sobre los 5 casos hasta que escriba `.bflow/evals/<run>.json`. Criterio: el JSON tiene `craft.ref` vacío o del HEAD, 5 entradas en `cases`, y `model` igual al reviewer de `bflow.yaml`.
- [ ] T3 Corrida con rc.5: `bflow eval reviewer --craft v0.1.0-rc.5` sobre los mismos 5 casos, sin cambiar config ni binario. Criterio: `craft.ref` = `v0.1.0-rc.5`, 5 entradas en `cases`, `model` igual al de T2. Si difiere, se corrige según T4.
- [ ] T4 Corregir resultados raros (si los hay): por cada referencia no reconocida, caso sin reporte o modelo distinto, arreglar la causa en `internal/eval`, `internal/cli/evalcmds.go` o el `case.yaml` correspondiente, con prueba que falle antes y pase después; reinstalar el binario y repetir T2 y T3. Si la causa está en el oficio de rc.5, no se toca y se anota. Criterio: `bflow check` verde y las corridas finales sin el resultado raro (o anotado como causa del oficio rc.5).
- [ ] T5 Versionar: commit de los dos JSON finales en la rama de la tarea; en `.bflow/evals/` no queda otra corrida. Criterio: `git diff --stat main` muestra solo los dos `.json`, la spec y, si hubo T4, los cambios de código/casos con sus pruebas.
- [ ] T6 Comentar en GH-171: tabla por corrida (atrapados, no atrapados, `false_positives`, `totals.tokens`, `totals.seconds`), cuál atrapa más, la frase literal "el reviewer actual atrapa X de Y; el de rc.5, Z de Y" con Y = `totals.defects`, y la sección Seguimiento con cada resultado raro y su corrección (commit) o la razón de no corregirlo. Criterio: el comentario existe y la frase coincide con `totals.caught`/`totals.defects` de los JSON.
