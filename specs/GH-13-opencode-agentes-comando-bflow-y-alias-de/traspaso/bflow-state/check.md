# Check
sha: ba7036361131cb58b81100df89681794739e182e
date: 2026-09-30T17:56:35-05:00
result: FAIL (DEGRADADO)

| paso | estado | s |
|:--|:--|:--|
| vet | pass | 0.9 |
| test | fail | 53.6 |
| build | pass | 1.0 |
| lint | pass | 1.6 |
| vulns | pass | 2.8 |
| secrets | skip | 0.0 |

## ⚠️ Degradado (no verificado en local)
- sin cgo: -race omitido (CGO_ENABLED=0)
- gitleaks no instalado: secrets omitido

## ❌ test
`go test -count=1 {race} ./...`
```
--- FAIL: TestUpdateRefreshesIntegrations (3.81s)
    --- FAIL: TestUpdateRefreshesIntegrations/falla_el_refresco:_update_sigue_OK_y_dice_qué_correr (0.03s)
        opencode_test.go:408: avisa cómo refrescar cada una: ""
FAIL
FAIL	innobytes.tech/bflow/internal/cli	18.224s
FAIL
…
ok  	innobytes.tech/bflow/internal/archtest	1.702s
ok  	innobytes.tech/bflow/internal/check	4.956s
--- FAIL: TestUpdateRefreshesIntegrations (3.81s)
    --- FAIL: TestUpdateRefreshesIntegrations/falla_el_refresco:_update_sigue_OK_y_dice_qué_correr (0.03s)
        opencode_test.go:408: avisa cómo refrescar cada una: ""
FAIL
FAIL	innobytes.tech/bflow/internal/cli	18.224s
ok  	innobytes.tech/bflow/internal/config	0.955s
ok  	innobytes.tech/bflow/internal/engine	1.697s
ok  	innobytes.tech/bflow/internal/envcheck	0.459s
ok  	innobytes.tech/bflow/internal/flow	0.892s
ok  	innobytes.tech/bflow/internal/guard	0.383s
ok  	innobytes.tech/bflow/internal/markdown	0.367s
ok  	innobytes.tech/bflow/internal/metrics	0.417s
ok  	innobytes.tech/bflow/internal/output	0.416s
ok  	innobytes.tech/bflow/internal/release	0.932s
ok  	innobytes.tech/bflow/internal/secrets	0.341s
ok  	innobytes.tech/bflow/internal/setup	0.373s
ok  	innobytes.tech/bflow/internal/store	3.329s
?   	innobytes.tech/bflow/internal/testutil	[no test files]
ok  	innobytes.tech/bflow/internal/tracker	0.356s
?   	innobytes.tech/bflow/internal/tracker/trackertest	[no test files]
?   	innobytes.tech/bflow/internal/vcs	[no test files]
?   	innobytes.tech/bflow/scripts/dist	[no test files]
FAIL
```
