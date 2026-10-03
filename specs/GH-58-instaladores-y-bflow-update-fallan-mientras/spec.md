# GH-58 · Instaladores y bflow update fallan mientras solo hay prereleases

## Brief

**Objetivo:** que `install.sh`, `install.ps1` y `bflow update` encuentren la versión correcta aunque solo haya prereleases (`v0.1.0-rc.*`) publicadas.

**Entra**
- Instaladores: si `/releases/latest` responde 404, toman la release más reciente no borrador de `/releases?per_page=10`, prerelease incluida.
- `BFLOW_VERSION=v0.1.0-rc.9` fija la versión en los dos instaladores (`/releases/tags/<tag>`).
- `bflow update`: con una prerelease instalada busca entre todas las releases, prereleases incluidas; con una estable solo usa `/releases/latest`; con un binario que no viene de una versión publicada, `/releases/latest` y, si responde 404, la lista.
- `release.Compare` ordena los sufijos por identificador, como pide semver: hoy compara texto y `rc.10` queda por debajo de `rc.9`, así que la rc.10 nunca le llegaría a quien tiene la rc.9.
- Pruebas con `httptest` y `BFLOW_RELEASES_URL`: `install.sh` (Linux/macOS), `install.ps1` (Windows), `Latest` y `bflow update --check`.
- README y CHANGELOG.

**No entra**
- Un canal elegible (`--pre`, configuración): el canal sale de la versión instalada.
- Paginar más allá de 10 releases.
- Autenticación contra la API de GitHub.

**Decisiones nuevas**
- [N1] El canal sale de la versión instalada. Se descarta un flag `--pre`: suma una opción que la tarea no pide.
- [N2] Con prerelease instalada, `update` elige la mayor según `Compare` entre las no borrador. Se descarta tomar la primera de la lista porque GitHub las ordena por fecha de creación, no por versión. Los instaladores sí toman la primera (en sh no hay forma razonable de ordenar semver).
- [N3] En `install.sh`, la lista solo aporta los tags en orden; cada uno se pide a `/releases/tags/<tag>` hasta que uno responde 200. Así se reutiliza el parseo actual de un solo objeto y un borrador se descarta solo (ese endpoint no devuelve borradores). Se descarta separar con `sed` los objetos de la lista.
- [N4] Solo un 404 activa la lista. Cualquier otro error (403 por límite de peticiones, red) se informa tal cual. Se descarta caer a la lista ante cualquier fallo porque esconde la causa real.
- [N5] `BFLOW_INSTALL_NO_PATH=1` evita que `install.ps1` toque el `PATH` del usuario. Sin él, la prueba en Windows cambiaría el registro de quien la corre. Se descarta probar solo en CI porque la prueba también corre en local.

**Riesgos**
- El parseo con `sed` en `install.sh` depende del formato JSON de GitHub. Se mitiga partiendo por comas, como ya hace `url_of`, para que funcione con JSON compacto o con sangría.
- PowerShell 5.1 y 7 exponen distinto el código de error de `Invoke-RestMethod`. Se lee `[int]$_.Exception.Response.StatusCode`, que funciona en los dos.

**Tamaño:** S-M. Cuatro archivos de código (`install.sh`, `install.ps1`, `update.go`, `updatecmds.go`), una corrección en `release.go`, pruebas y documentación.

## Discovery

Carril light: sin discovery. El scout confirmó que `Latest()` en `internal/release/update.go` es la única consulta a la API y que `releaseServer` (en `cmd/bflow/e2e_update_test.go` y en `internal/cli`) solo sirve `/releases/latest`.

## Requirements

<!-- Carril light: los criterios van en Tasks. -->

## Design

<!-- Carril light: el diseño va en Tasks. -->

## Tasks

- [ ] **T1 · `Compare` según semver en los sufijos** (`internal/release/release.go`).
  - R1 [N]: CUANDO dos versiones tienen el mismo núcleo y los dos sufijos existen, `Compare` DEBE compararlos identificador por identificador (separados por `.`): los numéricos como números, los demás como texto, un numérico antes que uno de texto, y si un sufijo es prefijo del otro, el más corto va antes. `Compare("v0.1.0-rc.9","v0.1.0-rc.10") == -1`.
  - R2 [N]: Los casos que ya cubre `TestCompareAndPublished` DEBEN seguir dando lo mismo. Se agregan `rc.9`/`rc.10`, `rc.1`/`rc.1.1` y `alpha`/`rc.1`.

- [ ] **T2 · `Latest` con canal** (`internal/release/update.go`).
  - Firma nueva: `func (c *Client) Latest(ctx context.Context, pre bool) (Release, error)`. Se agrega `Prerelease bool` a `Release` (del campo `prerelease` del JSON). El decodificado de un objeto release pasa a una función interna `decodeRelease` que comparten las dos rutas.
  - El 404 se distingue con un error tipado: `get` devuelve `*HTTPError{URL string; Status int}` cuando el código no es 200, con el mismo texto que hoy (`GET <url>: <status>`).
  - R3 [D]: CUANDO `pre` es false y `/releases/latest` responde 200, `Latest` DEBE devolver esa release, igual que hoy.
  - R4 [D]: CUANDO `pre` es false y `/releases/latest` responde 404, `Latest` DEBE pedir `/releases?per_page=10` y devolver la primera con `draft: false`.
  - R5 [N]: CUANDO `pre` es true, `Latest` DEBE pedir `/releases?per_page=10` sin pasar por `/releases/latest` y devolver la mayor según `Compare` entre las que no son borrador; los tags que `Compare` no entiende se ignoran.
  - R6 [N]: CUANDO la lista no tiene ninguna release válida, `Latest` DEBE fallar con `no hay versiones publicadas`. CUANDO `/releases/latest` responde algo que no es ni 200 ni 404, DEBE devolver ese error sin consultar la lista.
  - Pruebas en `internal/release/update_test.go` (nuevo), con `httptest`: `TestLatestStable`, `TestLatestFallbackOnlyPrereleases`, `TestLatestPreChoosesHighest` (lista en desorden, con un borrador y `rc.10` > `rc.9`), `TestLatestOtherErrorNoFallback`.

- [ ] **T3 · `bflow update` elige el canal** (`internal/cli/updatecmds.go`). Cita R7-R9.
  - R7 [D]: CUANDO la versión instalada es publicada y tiene sufijo (prerelease), `update` DEBE llamar `Latest(ctx, true)`: quien tiene `v0.1.0-rc.9` recibe `v0.1.0-rc.10` o `v0.1.0`, lo que sea mayor.
  - R8 [D]: CUANDO la versión instalada es estable, `update` DEBE llamar `Latest(ctx, false)`, y DEBE descartar una prerelease que le llegue por la lista: contesta `up_to_date` y nunca ofrece ni instala una prerelease.
  - R9 [D]: CUANDO el binario no viene de una versión publicada, `update` DEBE llamar `Latest(ctx, false)`, que con solo prereleases cae a la lista (R4). Los mensajes `update_unknown` y `--force` no cambian.
  - Pruebas: `releaseServer` de `cmd/bflow/e2e_update_test.go` acepta las releases a servir (tag, prerelease, draft) y sirve `/releases/latest` (404 si todas son prerelease), `/releases`, `/releases/tags/<tag>` y las descargas. Casos nuevos en `TestUpdate`, o en `TestUpdatePrerelease`: solo prereleases y binario `v0.1.0-rc.8`, entonces `--check` ofrece `v0.1.0-rc.9`; binario `v0.1.0-rc.9` con `rc.10` publicada, entonces la ofrece; binario `v0.1.0` con `v0.1.0` estable y `v0.2.0-rc.1`, entonces `up_to_date`. Se ajusta `releaseServer` de `internal/cli/opencode_test.go` si la firma lo pide.

- [ ] **T4 · `install.sh`**. Cita R10-R12.
  - R10 [D]: CUANDO `$api/releases/latest` responde 404, el script DEBE leer `$api/releases?per_page=10`, sacar los `tag_name` en orden (partiendo por comas) y pedir `$api/releases/tags/<tag>` uno por uno hasta que uno responda 200. Esa respuesta pasa a ser `json`. El código HTTP se lee con `curl -sSL -o <archivo> -w '%{http_code}'`. Con otro código que no sea 200 ni 404, sale con `bflow: no se pudo consultar <url> (HTTP <código>)`.
  - R11 [D]: CUANDO `BFLOW_VERSION` está definido, el script DEBE pedir solo `$api/releases/tags/$BFLOW_VERSION`, con una `v` delante si no la trae. Si responde 404, sale con `bflow: no existe la versión <tag>`.
  - R12 [N] (seguridad): `BFLOW_VERSION` DEBE cumplir `^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$` antes de usarse en una URL. Si no, el script sale con un error. La verificación de SHA-256 no cambia.
  - Prueba `TestInstallSh` en `cmd/bflow/e2e_install_test.go` (nuevo). Se salta en Windows o si no hay `sh`, `curl` o `tar`. Corre `sh ../../install.sh` con `BFLOW_RELEASES_URL` y `BFLOW_INSTALL_DIR` temporales contra el `releaseServer` de T3, en tres casos: solo prereleases (instala la primera de la lista), `BFLOW_VERSION=0.1.0-rc.8` (instala esa) y `BFLOW_VERSION='x;rm'` (falla sin pedir nada).

- [ ] **T5 · `install.ps1`**. Cita R13-R15.
  - R13 [D]: CUANDO `Invoke-RestMethod "$api/releases/latest"` falla con 404 (`[int]$_.Exception.Response.StatusCode -eq 404`), el script DEBE pedir `"$api/releases?per_page=10"` y usar el primer objeto con `-not $_.draft`. Si no hay ninguno, lanza `bflow: no se encontró una versión publicada`. Cualquier otro error se relanza.
  - R14 [D]: CUANDO `$env:BFLOW_VERSION` está definido, el script DEBE validarlo con la misma expresión de R12, agregarle la `v` si no la trae y pedir `"$api/releases/tags/$tag"`.
  - R15 [N]: CUANDO `$env:BFLOW_INSTALL_NO_PATH` es `1`, el script NO DEBE tocar el `PATH` del usuario. Se documenta en la cabecera del script.
  - Prueba `TestInstallPs1` en `cmd/bflow/e2e_install_test.go`. Corre solo en Windows y usa `powershell` o `pwsh`, el que esté (si no hay ninguno, se salta). Ejecuta `-NoProfile -ExecutionPolicy Bypass -File install.ps1` con `BFLOW_INSTALL_NO_PATH=1`, con los mismos casos que T4. El `releaseServer` sirve el `.zip` de Windows.

- [ ] **T6 · Documentación**. README (sección Instalación): fijar versión con `BFLOW_VERSION`, comportamiento con solo prereleases y canal de `bflow update`. CHANGELOG (`v0.1.0`, Corregido): una entrada para consumidores sobre los instaladores y `update` con prereleases, y sobre `rc.10` > `rc.9`. Cabeceras de `install.sh` e `install.ps1`: `BFLOW_VERSION` y, en ps1, `BFLOW_INSTALL_NO_PATH`.
