# GH-165 · Escáner de comentarios narrativos: línea '++ ' perdida, // en strings y #include

## Brief

**Objetivo**: que el escáner de comentarios narrativos (GH-151) no pierda líneas del diff ni tome por comentario un `//` dentro de un string o una directiva `#include`/`#define`.

**Entra**
- `DiffAdded` (`internal/adapters/vcs/git/git.go`): `+++ ` solo es cabecera entre `diff --git` y el primer `@@` del archivo. Una línea agregada `++ x` (llega como `+++ x`) se reporta y no vacía `path`.
- `IsComment` (`internal/review/comments.go`): el `//` final se busca fuera de comillas dobles, simples y backticks de la misma línea (con `\` como escape dentro de `"` y `'`).
- `IsComment`: `#` seguido sin espacio de una directiva del preprocesador (`include`, `include_next`, `import`, `define`, `undef`, `if`, `ifdef`, `ifndef`, `elif`, `elifdef`, `elifndef`, `else`, `endif`, `pragma`, `error`, `warning`, `line`, `region`, `endregion`) no es comentario.
- Una prueba por defecto que falla sin el arreglo, y la frase de `docs/configuracion.md` (sección «Comentarios que narran la tarea») que describe la regla.

**No entra**
- Strings multilínea (raw strings de varias líneas, heredocs, `"""`): el escáner sigue siendo línea a línea.
- Atributos de Rust (`#[cfg]`, `#![allow]`) y shebang `#!`: siguen contando como comentario `#`.
- Rutas citadas por git (`+++ "b/con\ttab"`): se siguen saltando como hoy.
- Cambiar los patrones, la regla de `.md` o el fake de `internal/engine/vcs_test.go`.

**Decisiones nuevas**
- [N] Cabecera por estado del hunk: `diff --git` abre la cabecera, `@@` la cierra; `+++ ` fuera de cabecera es una línea agregada. Descartado: exigir `+++ b/` (una línea agregada `++ b/x.go` seguiría cambiando `path`).
- [N] La directiva se reconoce solo con la palabra pegada al `#` (`#include`, `#define`). Descartado: aceptar espacios (`#  define`), porque `# define el límite T3` o `# if T1...` en Python/YAML/shell dejarían de verse. Costo: `#  define X // T3` en C indentado con espacio sigue siendo comentario (falso positivo, el de hoy).
- [N] Comillas simples cuentan como string. Descartado: solo dobles. Costo: una comilla simple sin pareja (lifetime de Rust `&'a str`, `it's` en shell) esconde el `//` final de esa línea (falso negativo, preferible al falso positivo que rechaza el DONE).
- [N] Se conserva la regla del espacio antes de `//` fuera de strings (`x := 1 // a` sigue siendo comentario; `http://x` sin comillas no).

**Riesgos**: falsos negativos acotados en los dos casos descritos; ninguno rechaza un DONE que hoy pasa.

**Tamaño**: S. Dos funciones, tres pruebas, una frase de docs. Sin cambios de firmas ni de config.

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Criterios EARS numerados R1..Rn, cada uno [D] (del discovery) o [N] (nuevo). -->

## Design

<!-- Estructuras, firmas, decisiones tomadas y descartadas, superficie de seguridad. -->

## Tasks

- [x] T1 `DiffAdded` con estado de cabecera.
  - CUANDO llega `diff --git `, el escáner DEBE vaciar `path` y entrar en cabecera; CUANDO llega `@@`, DEBE salir de cabecera y fijar `line`.
  - CUANDO una línea empieza con `+++ ` y está en cabecera, DEBE tomarse como ruta (`+++ b/<ruta>`; otra cosa, como `/dev/null`, deja `path` vacío). CUANDO no está en cabecera, DEBE tratarse como línea agregada con texto `l[1:]` (`++ x`) y avanzar `line`.
  - Prueba nueva `TestDiffAddedPlusPlusLine` en `internal/adapters/vcs/git/git_test.go`, repo real como `TestDiffAdded` (fixture, `write`, `sh`): un archivo con `"package a\n++ x\nvar C = 1\n"` y otro archivo después en orden de ruta; espera las tres líneas del primero (`++ x` en su línea) y las del segundo con su ruta. Sin el arreglo pierde `++ x` y `var C = 1`. `TestDiffAdded` no cambia.
- [x] T2 `IsComment`: `//` final fuera de strings.
  - CUANDO la línea no empieza con un prefijo de comentario, el sistema DEBE recorrerla con estado de comillas (`"`, `'`, `` ` ``; `\` escapa el siguiente carácter dentro de `"` y `'`, no dentro de backticks) y devolver desde el primer `//` fuera de comillas que tenga un espacio o tab justo antes. Sin ese `//`, no es comentario.
  - Casos nuevos en `TestIsCommentPrefixes`: `s := "a // T3"` → no; `c := 'a // b'` → no; ``r := `a // b` `` → no; `s := "x\" // y"` → no; `s := "a" // b` → `// b`; `s := "a\\" // b` → `// b`. Caso en `TestNarrativeCommentsIgnoresCode`: `s := "a // T3"`. Los casos existentes no cambian.
- [x] T3 `IsComment`: directivas del preprocesador.
  - CUANDO la línea recortada empieza con `#` seguido sin espacio de una de las directivas del Brief y luego fin de línea o un carácter que no es letra, dígito ni `_`, el sistema NO DEBE tratarla como comentario `#`; DEBE seguir evaluando el `//` final de T2 (`#include "x.h" // T3` → `// T3`).
  - CUANDO es `# include`, `#includes`, `#definir` o `# T1`, DEBE seguir siendo comentario.
  - Casos nuevos en `TestIsCommentPrefixes`: `#include "spec.h"`, `#define SPEC 1`, `#pragma once`, `#if T1`, `#endif` → no; `#include <x.h> // T3` → `// T3`; `# include x` y `#includes` → comentario. Caso en `TestNarrativeCommentsIgnoresCode`: `#include "spec.h"`.
  - Actualizar el doc de `IsComment` y la frase de `docs/configuracion.md` (sección «Comentarios que narran la tarea»): `//` final fuera de comillas; `#include`, `#define` y demás directivas pegadas al `#` no cuentan.
