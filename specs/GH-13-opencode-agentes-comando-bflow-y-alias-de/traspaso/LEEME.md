# Traspaso GH-13 (temporal: borrar esta carpeta antes del PR)

Fecha: 2026-09-30. Fase: implementing, compuerta `decision` abierta.

## Dónde quedó
- T1 a T8 implementadas y commiteadas en esta rama (config, cuerpo común del leader, adaptador de OpenCode, render multi-herramienta, install/update, init/doctor, docs).
- `bflow check GH-13` no está en verde por una sola prueba congelada.

## Decisión pendiente
`TestUpdateRefreshesIntegrations/falla_el_refresco` (internal/cli/opencode_test.go, helper `updateRun`, línea ~311) lee `out["text"]` del JSON de `bflow update`. `Envelope.Text` lleva `json:"-"`, así que siempre recibe `""`: la prueba nunca puede pasar. El comportamiento es correcto: en modo texto, update dice "corre bflow install claude" y "corre bflow install opencode".

Opciones:
1. (Recomendada) Corregir la prueba: `updateRun` corre sin `--json` y devuelve el stdout como texto. Requiere `bflow freeze --allow internal/cli/opencode_test.go`.
2. Serializar un campo `text` en el JSON de update (cambia el contrato JSON y gasta tokens).

## Retomar en otra máquina
`.bflow/` está en .gitignore; `bflow-state/` es la copia del estado local (state.json con la compuerta abierta, frozen-tests.json, discovery, contract, reports/impl.md).

```sh
git fetch && git switch feature/GH-13-opencode-agentes-comando-bflow-y-alias-de
mkdir -p .bflow/tasks
cp -r specs/GH-13-opencode-agentes-comando-bflow-y-alias-de/traspaso/bflow-state .bflow/tasks/GH-13
bflow status GH-13
# opción 1:
bflow approve GH-13 --gate decision --choice 1
```

Después, borrar `specs/GH-13-opencode-agentes-comando-bflow-y-alias-de/traspaso/` en un commit antes de abrir el PR.
