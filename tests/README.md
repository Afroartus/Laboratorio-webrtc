# tests

Pruebas que necesitan servicios reales o un navegador. Las pruebas aisladas de dominio y aplicación viven junto al código (`apps/api/**/*_test.go`, `apps/web/src/**`).

| Carpeta prevista | Contenido | Paso |
| --- | --- | --- |
| `integration/` | Adaptadores reales con PostgreSQL y LiveKit locales. | 6–8, 11 |
| `e2e/` | Navegación de la SPA con herramientas compatibles con Svelte. | 9, 12 |
| `lab/` | Guiones e informes de ejecución en uno y dos equipos (versiones, red, recursos). | 12 |
