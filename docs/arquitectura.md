# Arquitectura del laboratorio WebRTC

Estado: pasos 4 y 7 del spec. Las capas existen y sus reglas están fijadas; el contrato HTTP (puertos de entrada, adaptador Fiber y OpenAPI) está implementado. Los casos de uso y los adaptadores de salida se implementan en los pasos 5, 6, 8 y 11.

## Correspondencia con la estructura del spec

El repositorio usa exactamente la raíz de referencia del paso 4, con estas precisiones:

| Ruta | Nota |
| --- | --- |
| `apps/api/go.mod` | Módulo `webrtc/api`. Sustituye al `go.mod` inicial de la raíz y a `src/app/main.go`. |
| `apps/api/cmd/server/config.go` | Carga y validación del contrato de variables; pertenece a la composición. |
| `apps/api/internal/adapters/in/http/` | Paquete `httpadapter` (el nombre `http` ocultaría `net/http`). Handlers con Fiber v3. |
| `apps/api/api/openapi.yaml` | Contrato OpenAPI 3.1 de la API (paso 7). |
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

- Go: `net/http`, `github.com/gofiber/...`, `github.com/jackc/pgx/...`, `github.com/livekit/...`, cualquier `internal/adapters/...` o `cmd/...`.
- TypeScript: `svelte`, `svelte/store`, `livekit-client`, `src/adapters/**`, `src/presentation/**`, `src/composition/**`, y el uso de `fetch`, `window`, `document` o `navigator`.
- En `src/presentation`: importar `livekit-client` o `src/adapters/**`, o llamar a `fetch`.

Cada paquete Go de capa documenta su regla en `doc.go`; cada carpeta del frontend, en su `README.md`.

## Puntos de composición

- **Backend:** `apps/api/cmd/server/main.go` carga la configuración (falla con código 2 y la lista completa de errores, sin imprimir secretos), construye la aplicación Fiber (`httpadapter.NewApp`), registra handlers y gestiona el apagado ordenado con `SIGINT`/`SIGTERM`. Hoy registra `GET /health/live` y `GET /health/ready`; `httpadapter.RegisterAPI` se ensambla cuando existan los casos de uso y el receptor de webhooks.
- **Frontend:** `apps/web/src/composition/bootstrap.ts` monta la aplicación Svelte. Ahí se ensamblarán gateways, adaptadores, casos de uso y presenters.

## Controles automáticos

| Control | Estado |
| --- | --- |
| `go vet ./...` y `go test ./...` | Activos; se ejecutan también dentro de la build de `apps/api/Containerfile`. |
| `TestOpenAPIInSync` | Activo; falla si las rutas Fiber o los códigos de error difieren de `api/openapi.yaml`. |
| `npm run check` (`svelte-check` + `tsc`) | Activo; se ejecuta dentro de la build de `apps/web/Containerfile`. |
| Comprobación automática de imports por capa (Go y TS) | Pendiente, paso 12. Las reglas a aplicar son las listadas arriba. |

## Decisiones del paso 4

- **Variables derivadas en Compose.** `infra/.env` define dominios, IP y secretos una sola vez; `compose.yaml` construye `APP_BASE_URL`, `LIVEKIT_URL`, `DATABASE_URL` y `LIVEKIT_API_URL` para la API. Así no pueden desalinearse los nombres que usa Caddy y los que recibe el backend.
- **Secretos.** Solo en `infra/.env` (excluido de git). Compose interrumpe la ejecución si falta alguno (`${VAR:?}`). LiveKit recibe claves por `LIVEKIT_KEYS`, no en `livekit.yaml`.
- **Imágenes.** Etiquetas de versión exactas; la base distroless de la API se fija por digest porque su etiqueta `nonroot` es móvil.
- **Redes.** `backend` (postgres, api, livekit, migrate) y `edge` (web, api, livekit). PostgreSQL no comparte red con Caddy y no publica puertos.
- **Sonda de salud de la API.** La imagen no tiene shell; el binario acepta `-healthcheck` y consulta su propio `/health/live`.

## Decisiones del paso 7

- **Fiber v3 en lugar de `net/http`.** Decisión del usuario sobre el stack del paso 2. Fiber solo aparece en `adapters/in/http` y en `cmd/server`; el núcleo no lo importa.
- **Puertos de entrada.** `application/ports/input.go` define `Auth`, `Users` y `Calls` con tipos propios; `ports/errors.go`, los errores de aplicación. Los handlers dependen solo de esas interfaces y traducen cada error a `{ code, message, requestId }` con mensajes fijos, sin copiar texto interno.
- **Dominio mínimo.** `domain/call.go` fija el vocabulario (`Role`, `CallStatus`, `Permissions`, identificadores). Las transiciones y la asignación de permisos por rol quedan para los pasos 5 y 6.
- **Cadena por ruta.** Cada ruta declara sus middlewares: mutaciones con `Origin` exacto, sesión y `X-CSRF-Token`; login con `Origin`, JSON obligatorio y límite por IP (10/min por defecto). El webhook solo se autentica por firma, a través de `httpadapter.WebhookReceiver`, que implementará `adapters/in/webhooks` en el paso 11.
- **Cookie de sesión.** `__Host-webrtc_session`: `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, sin `Domain`.
- **IP de cliente.** Fiber confía en `X-Forwarded-For` solo si la conexión llega desde una red privada o loopback (Caddy en la red `edge`).
- **Valores inmutables.** `fiber.Config.Immutable` está activo: los casos de uso pueden conservar cadenas recibidas sin leer búferes reutilizados por fasthttp.
- **Pendiente fuera del paso 7.** La idempotencia de `POST /calls` requiere persistir `Idempotency-Key` (no figura en el modelo del paso 6). El webhook queda accesible a través de Caddy en `/api/*` y lo protege la firma; restringirlo a la red interna corresponde al paso 10.
