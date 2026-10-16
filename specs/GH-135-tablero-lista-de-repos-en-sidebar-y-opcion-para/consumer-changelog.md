# Cambios para consumidores: GH-135, sidebar de repos en `bflow ui`

## Cambio que rompe compatibilidad

- **El marcado de la lista de repos cambió.** Quien lea el panel web con scripts, pruebas de interfaz o extensiones que dependan del DOM debe adaptarse:
  - Antes: `<nav class="repos" id="repos">` con una tira horizontal de botones (chips).
  - Ahora: `<aside class="side frame" id="repos">` con una columna vertical, `<ul id="repo-list">` y una `<li class="repo">` por repo. Cada fila tiene dos botones: uno para elegir el repo (clase `.pick`) y otro para "Quitar".
  - Si su script buscaba `nav#repos` o los chips de la tira, debe buscar `aside#repos` y `li.repo`.
  - Qué tiene que hacer el consumidor: actualizar los selectores. No hay cambio en la API ni en la salida de comandos.

## Cambios que no rompen compatibilidad

- **Ocultar repos en el panel.** Cada repo tiene "Quitar", que lo saca de la vista. No lo des-registra: bflow sigue registrando su flujo, y sus avisos (notificaciones y "Te toca en otro repo" en el título de la pestaña) siguen activos.
  - "Deshacer" aparece durante 6 segundos tras quitar un repo.
  - "Ocultos (n)" lista los repos quitados; "Mostrar" los regresa a su lugar según el orden de urgencia.
  - Si un repo oculto pasa a `TÚ`, el contador "Ocultos" se marca en ámbar.
- **Orden por urgencia.** Los repos en `TÚ` van primero, luego `AGENTE`, luego `vía libre`; dentro de cada grupo, por actividad más reciente.
- **Colapsar el sidebar.** Se puede reducir a solo el carril (F / L / H). Desde 1180px de ancho; por debajo, la lista es un panel que se abre con el botón "Repos".
- **Estado guardado en el navegador.** Se usan dos claves de `localStorage`:
  - `bflow.hidden-repos`: arreglo de ids de repo de 8 caracteres hexadecimales.
  - `bflow.sidebar-collapsed`: `"1"` cuando está colapsado.
  - Si el navegador no permite almacenamiento, todos los repos se ven y no hay error.
  - Esto es por navegador: ocultar en un navegador no lo oculta en otro.
- **Etiquetas de estado.** Cada fila muestra `TÚ`, `AGENTE` o `vía libre`. La etiqueta "TE TOCA" ya no aparece; el número de aprobaciones abiertas sale en el atributo `title` de la fila.
- **Con menos de 2 repos no hay sidebar**, igual que antes con la tira. Si todos los repos están ocultos, el tablero muestra "Todos tus repos están ocultos".

## Sin cambios

- Comandos, salida JSON, endpoints del servidor y configuración (`config.yaml`) no cambian.
