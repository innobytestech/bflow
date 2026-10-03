# Consumer changelog v0.1.0

## Instaladores funcionan con solo prereleases publicadas

Antes: `install.sh` e `install.ps1` llamaban a `/releases/latest`, que GitHub no marca para prereleases (devuelve 404). Quien no tenía Go no podía instalar mientras solo hubiera `v0.1.0-rc.*` publicadas.

Ahora: con `/releases/latest` → 404, los scripts piden `/releases?per_page=10` y usan la primera release no borrador, prerelease incluida.

**Cambio:** ninguno para quien usa los scripts. Simplemente funcionan donde antes fallarían.

## Fijar versión con BFLOW_VERSION

Ahora `install.sh` e `install.ps1` aceptan `BFLOW_VERSION` para instalar una versión específica (con o sin la `v`):

```bash
curl -fsSL https://raw.githubusercontent.com/innobytestech/bflow/main/install.sh | BFLOW_VERSION=v0.1.0-rc.9 sh
```

```powershell
$env:BFLOW_VERSION = 'v0.1.0-rc.9'; irm https://raw.githubusercontent.com/innobytestech/bflow/main/install.ps1 | iex
```

Versión inexistente: falla con `bflow: no existe la versión <tag>` (ambos scripts).

La validación rechaza formato inválido antes de pedir nada: `bflow: BFLOW_VERSION no es una versión válida`.

**Consumidor:** quien automatiza la instalación puede fijar `BFLOW_VERSION` en su script. En local se sigue sin él.

## `BFLOW_INSTALL_NO_PATH` en Windows (nuevo)

`install.ps1` toca el PATH del usuario por defecto. Ahora `BFLOW_INSTALL_NO_PATH=1` lo evita:

```powershell
$env:BFLOW_INSTALL_NO_PATH = '1'; irm ... | iex
```

**Consumidor:** útil en CI o cuando bflow está solo en un lugar específico sin necesidad de PATH.

## `bflow update` elige el canal según lo instalado

Antes: `update` buscaba siempre en `/releases/latest` (sin prereleases). Quien tenía `v0.1.0-rc.9` instalada no recibía `v0.1.0-rc.10`.

Ahora: 
- **Con prerelease instalada:** busca entre todas las versiones (no borradores) y ofrece la mayor, aunque sea estable. Ejemplo: `v0.1.0-rc.9` → `v0.1.0-rc.10` o `v0.1.0`, lo que sea mayor.
- **Con versión estable instalada:** busca solo en `/releases/latest` (sin borradores ni prereleases) y nunca ofrece prerelease, aunque esté publicada. Quien tiene `v0.1.0` ve `up_to_date` si la lista solo trae `v0.1.0-rc.2`.
- **Sin versión publicada:** busca `/releases/latest` y, con 404, la lista. Nunca ofrece prerelease por defecto.

**Consumidor:** si lees `bflow update --check` o usas el servicio de update, el comportamiento es predecible: el canal surge de lo instalado, no de la política global.

## Orden de versiones: rc.10 > rc.9

Antes: la comparación de sufijos (`-rc.9` vs `-rc.10`) usaba comparación textual, así que `rc.9` > `rc.10` (texto). Nadie con `rc.9` recibía `rc.10` como "mayor".

Ahora: semver correcta. `rc.10 > rc.9 > rc.1 > alpha` (numéricos antes de texto, texto en orden alfabético, prefijo antes que el mismo con más identificadores).

**Cambio:** invisible si tienes estables. Si pruebas con solo prereleases, `rc.10` se ofrece correctamente como mayor.

## Resumen para cliente de instaladores

1. Fijar versión: `BFLOW_VERSION=v0.1.0-rc.9 sh` (bash) o `$env:BFLOW_VERSION = '...'` (ps1).
2. No tocar PATH en Windows: `$env:BFLOW_INSTALL_NO_PATH = '1'`.
3. Con solo prereleases publicadas, el script elige la más reciente automáticamente.
4. `bflow update` ahora respeta el canal: prerelease → busca entre todas; estable → solo estables.
