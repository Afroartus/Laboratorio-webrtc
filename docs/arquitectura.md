# Arquitectura del laboratorio WebRTC

Estado: el backend sigue en el paso 4 (composición, configuración y `GET /health/live`). El frontend ya implementa el núcleo, los adaptadores y la interfaz de los pasos 3.3 y 9, contra el contrato HTTP del paso 7. La API Go, la persistencia y el nodo LiveKit todavía no cubren ese contrato.

## Correspondencia con la estructura del spec

El repositorio usa exactamente la raíz de referencia del paso 4, con estas precisiones:

| Ruta | Nota |
| --- | --- |
| `apps/api/go.mod` | Módulo `webrtc/api`. Sustituye al `go.mod` inicial de la raíz y a `src/app/main.go`. |
| `apps/api/cmd/server/config.go` | Carga y validación del contrato de variables; pertenece a la composición. |
| `apps/api/internal/adapters/in/http/` | Paquete `httpadapter` (el nombre `http` ocultaría `net/http`). |
| `apps/web/src/composition/bootstrap.ts` | Ensamblado del frontend; `main.ts` solo lo invoca. |

## Capas y dependencias permitidas

```text
              composición (cmd/server, src/composition)
                 │ construye e inyecta
    ┌────────────┼──────────────────────────┐
    ▼            ▼                          ▼
adaptadores de entrada              adaptadores de salida
(http, webhooks, jobs, presentation) (postgres, livekit, security, clock,
    │                                 adapters/http, adapters/livekit, adapters/browser)
    │ invocan                          │ implementan
    ▼                                  ▼
          aplicación: usecases ──► ports
                    │
                    ▼
                 dominio
```

| Capa | Backend Go | Frontend Svelte | Puede importar |
| --- | --- | --- | --- |
| Dominio | `internal/domain` | `src/domain` | Solo biblioteca estándar / TypeScript puro. |
| Puertos | `internal/application/ports` | `src/application/ports` | Dominio. |
| Casos de uso | `internal/application/usecases` | `src/application/usecases` | Dominio y puertos. |
| Entrada | `internal/adapters/in/{http,webhooks,jobs}` | `src/presentation` | Aplicación y tecnología de entrada. |
| Salida | `internal/adapters/out/{postgres,livekit,security,clock}` | `src/adapters/{http,livekit,browser}` | Puertos y tecnología concreta. |
| Composición | `cmd/server` | `src/composition`, `src/main.ts` | Todas. |

Prohibido en el núcleo (dominio + aplicación):

- Go: `net/http`, `github.com/jackc/pgx/...`, `github.com/livekit/...`, cualquier `internal/adapters/...` o `cmd/...`.
- TypeScript: `svelte`, `svelte/store`, `livekit-client`, `src/adapters/**`, `src/presentation/**`, `src/composition/**`, y el uso de `fetch`, `window`, `document` o `navigator`.
- En `src/presentation`: importar `livekit-client` o `src/adapters/**`, o llamar a `fetch`.

Cada paquete Go de capa documenta su regla en `doc.go`; cada carpeta del frontend, en su `README.md`.

## Puntos de composición

- **Backend:** `apps/api/cmd/server/main.go` carga la configuración (falla con código 2 y la lista completa de errores, sin imprimir secretos), construye el `ServeMux`, registra handlers y gestiona el apagado ordenado con `SIGINT`/`SIGTERM`. Hoy registra solo `GET /health/live`.
- **Frontend:** `apps/web/src/composition/bootstrap.ts` es el único ensamblado y monta siempre el perfil simulado, en `vite dev` y en la build. Así arrancar la aplicación no llama a la API ni abre LiveKit. Los adaptadores `HttpAuthGateway`, `HttpCallGateway` y `LiveKitMediaSession` (`livekit-client` 2.23.0, compatible con el servidor v1.13.10 del laboratorio) siguen en el repositorio, sin que la composición los importe, para enchufarlos cuando existan la API y el nodo.

## Controles automáticos

| Control | Estado |
| --- | --- |
| `go vet ./...` y `go test ./...` | Activos; se ejecutan también dentro de la build de `apps/api/Containerfile`. |
| `npm run check` (`svelte-check` + `tsc`) y `npm test` | Activos en `apps/web`. `check` se ejecuta dentro de la build de `apps/web/Containerfile`. |
| Comprobación automática de imports por capa (TS) | Activa en `apps/web/scripts/check-imports.mjs`, incluida en `npm run check`. |
| Comprobación automática de imports por capa (Go) | Pendiente. Las reglas son las de la tabla de arriba. |

## Decisiones del paso 4

- **Variables derivadas en Compose.** `infra/.env` define dominios, IP y secretos una sola vez; `compose.yaml` construye `APP_BASE_URL`, `LIVEKIT_URL`, `DATABASE_URL` y `LIVEKIT_API_URL` para la API. Así no pueden desalinearse los nombres que usa Caddy y los que recibe el backend.
- **Secretos.** Solo en `infra/.env` (excluido de git). Compose interrumpe la ejecución si falta alguno (`${VAR:?}`). LiveKit recibe claves por `LIVEKIT_KEYS`, no en `livekit.yaml`.
- **Imágenes.** Etiquetas de versión exactas; la base distroless de la API se fija por digest porque su etiqueta `nonroot` es móvil.
- **Redes.** `backend` (postgres, api, livekit, migrate) y `edge` (web, api, livekit). PostgreSQL no comparte red con Caddy y no publica puertos.
- **Sonda de salud de la API.** La imagen no tiene shell; el binario acepta `-healthcheck` y consulta su propio `/health/live`.
- **Frontend.** Los casos de uso viven en `LabSession` y son la única máquina de estados; el presenter de Svelte refleja su snapshot. Los puertos son `AuthGateway`, `CallGateway`, `MediaSession` y `DeviceAccess`. El JWT de medios y el CSRF no salen de la memoria del adaptador. Apagar la cámara despublica la pista y detiene la captura. Compartir pantalla llama a `getDisplayMedia` en la misma vuelta del clic. `?relay=1` pide candidatos de relé al conectar.
