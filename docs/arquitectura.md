# Arquitectura del laboratorio WebRTC

Estado: paso 6 del spec (modelo de datos y ciclo de vida). Las capas existen y sus reglas están fijadas; el esquema SQL, el dominio de llamadas, los casos de uso del ciclo de vida y el proceso periódico están implementados. El login, los contratos HTTP y el resto de adaptadores llegan en los pasos 5, 7–11.

## Correspondencia con la estructura del spec

El repositorio usa exactamente la raíz de referencia del paso 4, con estas precisiones:

| Ruta | Nota |
| --- | --- |
| `apps/api/go.mod` | Módulo `webrtc/api`. Sustituye al `go.mod` inicial de la raíz y a `src/app/main.go`. |
| `apps/api/cmd/server/config.go` | Carga y validación del contrato de variables; pertenece a la composición. |
| `apps/api/internal/adapters/in/http/` | Paquete `httpadapter` (el nombre `http` ocultaría `net/http`). |
| `apps/api/internal/adapters/in/jobs/` | Worker periódico: reconciliación y trabajos pendientes del outbox. |
| `apps/api/migrations/` | Migraciones SQL (`000001_init.up.sql` / `.down.sql`). |
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

## Modelo de datos (paso 6)

El esquema vive en `apps/api/migrations/000001_init.up.sql` y se aplica con la tarea `migrate` de Compose. Tablas:

| Tabla | Contenido |
| --- | --- |
| `users` | Directorio local: nombre de usuario único, hash de contraseña, estado. |
| `auth_sessions` | Sesiones: solo el hash del identificador opaco, caducidad y revocación. |
| `calls` | Llamada: sala única, propietario, estado, plazo de ingreso, duración máxima y versión para bloqueo optimista. |
| `call_members` | Pertenencia: rol (`creator`, `participant`, `viewer`) y fuentes que puede publicar. |
| `call_sessions` | Conexiones de participantes: `participant_sid`, `room_sid`, entrada y salida. |
| `call_events` | Auditoría: tipo de evento, actor, instante y detalles en `jsonb`. |
| `outbox_jobs` | Efectos externos pendientes (por ahora `delete_room`) con intentos, próximo intento y último error. |

Convenciones: fechas en `timestamptz` (la conexión fija `timezone=UTC`), identificadores `uuid` generados por PostgreSQL, estados de llamada restringidos por `CHECK` y un único trabajo pendiente por llamada y operación mediante índice único parcial.

### Ciclo de vida

```text
CREATED ──▶ ACTIVE ──▶ ENDING ──▶ ENDED
   │                     ▲
   ├──▶ CANCELLED        │
   └──▶ ENDING ──────────┘
```

- `CREATED → ACTIVE`: el primer participante conecta con los medios.
- `ACTIVE → ENDING`: el creador termina la llamada o vence la duración máxima.
- `ENDING → ENDED`: el trabajo pendiente elimina la sala en el servidor de medios.
- `CREATED → CANCELLED`: vence el plazo de ingreso y nadie se une.
- `ENDED` y `CANCELLED` son terminales: una transición inválida es un error, no un reinicio.

### Proceso periódico

`internal/adapters/in/jobs` ejecuta cada `WORKER_INTERVAL_SECONDS` dos tareas: `reconcile-calls` (compara el estado persistido con el del servidor de medios y aplica las transiciones pendientes) y `process-outbox` (reclama los trabajos vencidos con `FOR UPDATE SKIP LOCKED`, llama al servidor de medios **fuera** de la transacción y actualiza el estado dentro de una transacción). Los reintentos usan un backoff exponencial acotado (`domain.Backoff`) y, al agotar `OUTBOX_MAX_ATTEMPTS`, el trabajo se abandona conservando el error.

## Puntos de composición

- **Backend:** `apps/api/cmd/server/main.go` carga la configuración (falla con código 2 y la lista completa de errores, sin imprimir secretos), construye el pool de PostgreSQL (esperando a que la base responda), el store, la unidad de trabajo, el reloj, el adaptador de salas de LiveKit, los casos de uso del ciclo de vida y el worker periódico; registra los handlers y gestiona el apagado ordenado con `SIGINT`/`SIGTERM`. Hoy registra solo `GET /health/live`.
- **Frontend:** `apps/web/src/composition/bootstrap.ts` monta la aplicación Svelte. Ahí se ensamblarán gateways, adaptadores, casos de uso y presenters.

## Controles automáticos

| Control | Estado |
| --- | --- |
| `go vet ./...` y `go test ./...` | Activos; se ejecutan también dentro de la build de `apps/api/Containerfile`. |
| Pruebas de integración de PostgreSQL | `go test -tags integration ./...`; requieren `TEST_DATABASE_URL` (o `DATABASE_URL`). Cada prueba crea un esquema aislado con la migración y lo borra al terminar. |
| `npm run check` (`svelte-check` + `tsc`) | Activo; se ejecuta dentro de la build de `apps/web/Containerfile`. |
| Comprobación automática de imports por capa (Go y TS) | Pendiente, paso 12. Las reglas a aplicar son las listadas arriba. |

## Decisiones del paso 6

- **Estado y reglas en el dominio.** Las transiciones, el plazo de ingreso, la duración máxima y los permisos por rol viven en `internal/domain`; los casos de uso solo coordinan. El núcleo no importa nada de `application` ni de `adapters`.
- **Bloqueo optimista con versión.** `calls.version` se incrementa en cada cambio de estado: dos procesos que compiten por la misma llamada no se pisan, el segundo recibe `ports.ErrCallStateConflict`.
- **Outbox transaccional.** El trabajo de eliminar la sala se guarda en la misma transacción que el paso a `ENDING`. Si el proceso se reinicia, el trabajo sigue pendiente y se reintenta.
- **Efecto externo fuera de la transacción.** La llamada al servidor de medios no puede deshacerse: se hace fuera de la transacción y solo se actualiza el estado persistido dentro de ella. El borrado de la sala es idempotente, así que un reintento no daña nada.
- **Reconciliación sin adivinar.** Una llamada `ACTIVE` cuya sala no aparece se cuenta como huérfana y se registra, pero no cambia de estado: hasta que el paso 11 registre las conexiones de los participantes no hay forma de saber si la sala se recreó.
- **Sin secretos ni medios en la base.** No se guardan tokens de medios ni contenidos de audio o video; solo metadatos y auditoría técnica.

## Decisiones del paso 4

- **Variables derivadas en Compose.** `infra/.env` define dominios, IP y secretos una sola vez; `compose.yaml` construye `APP_BASE_URL`, `LIVEKIT_URL`, `DATABASE_URL` y `LIVEKIT_API_URL` para la API. Así no pueden desalinearse los nombres que usa Caddy y los que recibe el backend.
- **Secretos.** Solo en `infra/.env` (excluido de git). Compose interrumpe la ejecución si falta alguno (`${VAR:?}`). LiveKit recibe claves por `LIVEKIT_KEYS`, no en `livekit.yaml`.
- **Imágenes.** Etiquetas de versión exactas; la base distroless de la API se fija por digest porque su etiqueta `nonroot` es móvil.
- **Redes.** `backend` (postgres, api, livekit, migrate) y `edge` (web, api, livekit). PostgreSQL no comparte red con Caddy y no publica puertos.
- **Sonda de salud de la API.** La imagen no tiene shell; el binario acepta `-healthcheck` y consulta su propio `/health/live`.
