# Plan — Paso 6: modelo de datos y ciclo de vida

Objetivo: implementar el paso 6 del spec (`docs/SPEC-tecnico-app-WebRTC.md:235-255`): esquema SQL
migratable, entidades y transiciones de dominio, persistencia transaccional en PostgreSQL, casos de
uso del ciclo de vida de la llamada y proceso periódico de cancelación/cierre/reconciliación.

Estado: fases 0–8 implementadas y verificadas con Go local; la verificación con Podman (fase 9–10)
queda pendiente de la instalación de Podman 6.0.2, que requiere elevación.

## Decisiones tomadas

| Tema | Decisión |
| --- | --- |
| Rama | Nueva rama `paso-seis-modelo-datos` desde `master` (HEAD `c9f611f`). |
| Alcance | Esquema + dominio + persistencia + casos de uso + worker (paso 6 completo). |
| Verificación | Go 1.26.3 local (pin del proyecto) + Podman 6.0.2 (máquina WSL rootful, `podman-compose` 1.6.0) para PostgreSQL y `migrate`. Docker Desktop no se usa. |
| HTTP | Se queda `net/http` de `master`. La rama `origin/paso-siete-contruir-contratos` usa Fiber v3 (desvía del spec) y no se toca aquí. |
| Compatibilidad con el paso 7 | `domain/call.go` y `ports/{contracts,input,errors}.go` se replican **verbatim** de `origin/paso-siete-contruir-contratos`, para que la fusión futura no colisione. |
| LiveKit | `server-sdk-go/v2 v2.18.0` con `livekit/protocol v1.49.0` fijados: v2.19.0 exige Go ≥ 1.26.8 y el proyecto está en 1.26.3. |
| `infra/.env.exmaple` | Se renombra a `infra/.env.example` en esta rama: el nombre mal escrito rompe la instrucción de `docs/laboratorio.md`. La rama del paso 7 hace el mismo rename, así que la fusión lo resolve de forma trivial. |

Contexto relevante del repo:

- `master` (HEAD `c9f611f`) tiene el andamiaje del paso 4 y el `compose.yaml` con el servicio `migrate`
  ya preparado (`infra/compose.yaml:28`), apuntando a `apps/api/migrations`, que hoy está vacío.
- `apps/api/go.mod` no tiene dependencias: `pgx/v5`, `uuid` y el SDK de LiveKit entran en este paso.

## Fase 0 — Entorno de trabajo

1. Go 1.26.3 instalado en `C:\Users\alurr\AppData\Local\Programs\go` (no elevated). En la sesión:
   `export PATH="/c/Users/alurr/AppData/Local/Programs/go/bin:$PATH"` y `GOTOOLCHAIN=local`.
2. Podman 6.0.2: instalador descargado en
   `C:\Users\alurr\AppData\Local\Temp\opencode\podman-installer-6.0.2.msi`
   (sha256 `c094059880f033656092f5fb4306457e42aa068ee32137162299817c5f79396f`). La instalación
   por usuario (`msiexec /i … ALLUSERS=2 MSIINSTALLPERUSER=1`) se queda colgada sin elevación: hay
   que ejecutarla desde una consola elevada. Hasta entonces, la verificación es solo con Go local.
3. Rama creada: `git switch -c paso-seis-modelo-datos` desde `master`.

## Fase 1 — Migraciones SQL

`apps/api/migrations/000001_init.up.sql` y `000001_init.down.sql` (formato `golang-migrate` v4.20.1,
el mismo que la imagen fijada en `infra/compose.yaml:29`).

Tablas (campos del spec:235-245, fechas en UTC con `timestamptz`):

- `users`: `id uuid PK default gen_random_uuid()`, `username text UNIQUE NOT NULL`,
  `display_name`, `password_hash` (lo necesita el paso 5), `active bool`, `created_at`.
- `auth_sessions`: `id`, `user_id FK→users`, `session_token_hash UNIQUE`, `csrf_secret`,
  `expires_at`, `revoked_at`, `created_at`. Índices por `user_id` y por `expires_at` donde no está revocada.
- `calls`: `id uuid PK`, `room_name text UNIQUE`, `owner_id FK→users`, `status text` con
  `CHECK (status IN ('CREATED','ACTIVE','ENDING','ENDED','CANCELLED'))`, `created_at`, `join_deadline`,
  `started_at`, `ended_at`, `max_duration_seconds INT CHECK (> 0)`, `version INT NOT NULL DEFAULT 0`
  (bloqueo optimista). Índices por `status`, `owner_id`, `join_deadline` (`CREATED`) y `started_at` (`ACTIVE`).
- `call_members`: PK `(call_id, user_id)`, `role` con `CHECK IN ('creator','participant','viewer')`,
  `allowed_sources text[]` con `CHECK (allowed_sources <@ ARRAY['camera','microphone','screen'])`,
  `revoked_at`, `created_at`. FK `call_id` con `ON DELETE CASCADE`.
- `call_sessions`: `id`, `call_id FK`, `user_id FK`, `participant_sid`, `room_sid`, `joined_at`,
  `left_at`, `disconnect_reason`. Índice parcial para las sesiones abiertas.
- `call_events`: `id`, `call_id FK`, `event_type`, `actor_id FK`, `occurred_at`, `provider_event_id`
  con índice único parcial `WHERE provider_event_id IS NOT NULL` (deduplicación de webhooks),
  `details jsonb DEFAULT '{}'`.
- `outbox_jobs`: `id`, `call_id FK`, `operation` con `CHECK IN ('delete_room')`, `attempts`,
  `next_attempt_at`, `completed_at`, `last_error`, `created_at`. Índice único parcial
  `(call_id, operation) WHERE completed_at IS NULL` → idempotencia del cierre.

`down.sql` borra las tablas en orden inverso. No se guardan tokens ni contenidos de medios
(spec:247); `password_hash` es el único secreto y vive fuera del núcleo.

## Fase 2 — Dominio (`apps/api/internal/domain/`)

Archivos nuevos, sin etiquetas JSON ni de persistencia:

- `call.go` — vocabulario replicado verbatim de la rama del paso 7: `UserID`, `CallID`, `Role`
  (`creator|participant|viewer`), `CallStatus` (`CREATED|ACTIVE|ENDING|ENDED|CANCELLED`), `Permissions`.
- `call_entity.go` — `Call` (id, room name, owner, estado, plazos, `version`, miembros), `CallMember`
  (rol, `allowed_sources`, `revoked_at`), `MediaSource` (`camera|microphone|screen`), `CallSession`,
  `User` y `CallEvent`.
- `transition.go` — tabla de transiciones válidas y `TransitionTo`:
  `CREATED→ACTIVE`, `CREATED→ENDING`, `CREATED→CANCELLED`, `ACTIVE→ENDING`, `ENDING→ENDED`.
  `ENDED` y `CANCELLED` son terminales: ninguna transición sale de ellos, de modo que un error
  transitorio de infraestructura no reabra una llamada terminada (spec:249).
  Interpretación documentada: `CREATED→ENDING` existe para que el creador pueda finalizar una
  llamada aún vacía (el contrato HTTP del paso 7 devuelve `ENDING`); `CANCELLED` queda para la
  caducidad del plazo de ingreso.
  Además `DeadlineExceeded`, `DurationExceeded` e `IsTerminal`.
- `permissions.go` — `PermissionsForRole`: creador y participante publican cámara, micrófono y
  pantalla y se suscriben; el espectador solo se suscribe (spec:225-229).
- `outbox.go` — `JobOperation`, `OutboxJob` y el cálculo del próximo intento (`Backoff` exponencial
  acotado entre 5 s y 5 min) sin dependencias externas.
- `errors.go` — `ErrInvalidTransition` y errores de dominio propios.

## Fase 3 — Puertos (`apps/api/internal/application/ports/`)

- `contracts.go`, `input.go`, `errors.go` — replicados verbatim de la rama del paso 7
  (`Auth`, `Users`, `Calls`, `CreateCallInput`, `CallAccess`, `EndCallResult`, `ErrCallStateConflict`, …).
- `output.go` (nuevo) — puertos de salida: `CallRepository`, `CallMemberRepository`,
  `CallSessionRepository`, `EventRepository`, `OutboxRepository`, `UserRepository`, `Repositories`
  (agregador), `UnitOfWork` (`WithinTransaction(ctx, func(ctx, Repositories) error) error`),
  `RoomService` (`GetRoom`, `DeleteRoom`) y `Clock` (`Now()`). `ErrRoomNotFound` vive aquí para no
  alterar el `errors.go` replicado.

Ninguna firma expone `pgx.Tx`, `http.Request`, tipos del SDK de LiveKit ni códigos HTTP
(spec:118).

## Fase 4 — Adaptador PostgreSQL (`apps/api/internal/adapters/out/postgres/`)

- `db.go` — `Open(ctx, dsn)` con `pgxpool` (`timezone=UTC`, `MaxConns=10`) y `WaitReady` con
  reintentos y backoff; `Close`.
- `store.go` — `Store` con `Repositories()` (pool) y `UnitOfWork()`: abre una transacción, entrega
  repositorios ligados a ella y hace `Commit`/`Rollback`.
- `call_repository.go`, `member_repository.go`, `session_repository.go`, `event_repository.go`,
  `outbox_repository.go`, `user_repository.go` — `Insert`, `GetByID`, `GetByIDForUpdate`
  (`SELECT … FOR UPDATE` sobre el registro de la llamada, para serializar emisión de credenciales y
  cierre, spec:250), `UpdateStatus` con comprobación de `version`, listados de caducadas y de
  duración excedida, y el reclamo de jobs del outbox con `FOR UPDATE SKIP LOCKED`.
  Las consultas usan `id::text` y parámetros `text` para que el núcleo nunca vea tipos de pgx.

## Fase 5 — Adaptadores de salida

- `out/livekit/room_service.go` — SDK oficial `github.com/livekit/server-sdk-go/v2 v2.18.0`:
  `GetRoom` (nombre, SID, número de participantes; `ListRooms` filtrado por nombre) y `DeleteRoom`
  (idempotente: un `not_found` de Twirp se traduce en éxito). Ningún tipo del SDK sale del paquete.
- `out/clock/clock.go` — `Clock` del sistema.

## Fase 6 — Casos de uso (`apps/api/internal/application/usecases/`)

| Caso de uso | Comportamiento |
| --- | --- |
| `CreateCall` | Valida invitado (existe, activo, no es el creador). En **una transacción**: crea la llamada (`CREATED`, `room_name = call_<uuid>`, `join_deadline = now + JOIN_DEADLINE_SECONDS`, `max_duration_seconds` de la config), los dos miembros con sus `allowed_sources`, y el evento `call.created`. |
| `EndCall` | Solo el owner. Bloquea la llamada, la pasa a `ENDING` (si ya estaba terminal o en `ENDING` no repite efectos), inserta el job `delete_room` en la **misma transacción** y el evento. Repetir la llamada no duplica efectos (índice único parcial del outbox). |
| `ActivateCall` | Marca `ACTIVE` al confirmar la conexión de medios del primer participante (spec:249) y registra la `call_session`. Deja el paso 8/11 conectar su evento. |
| `ReconcileCalls` | Periódico: (a) cancela `CREATED` cuyo `join_deadline` venció → `CANCELLED`; (b) pasa a `ENDING` las `ACTIVE` que excedieron `started_at + max_duration_seconds` y encola `delete_room`; (c) las `ENDING` cuya sala ya no existe pasan a `ENDED`. Una llamada `ACTIVE` sin sala se registra como aviso, sin cambiar de estado, hasta que el paso 11 pueble `call_sessions`; se documenta como limitación. |
| `ProcessOutbox` | Reclama jobs vencidos, llama a `RoomService.DeleteRoom` **fuera** de la transacción SQL (spec:120), y en una transacción marca `ENDED` + evento, o incrementa `attempts` con backoff y guarda `last_error`. Al superar `OUTBOX_MAX_ATTEMPTS` se cierra el job con error registrado. |

## Fase 7 — Worker periódico (`apps/api/internal/adapters/in/jobs/`)

`worker.go`: goroutine con `ticker` que invoca `ReconcileCalls` y `ProcessOutbox`; no toca tablas
directamente; se cancela por contexto y el apagado es ordenado (spec:376). La primera pasada es
inmediata.

## Fase 8 — Composición (`apps/api/cmd/server/`)

- `config.go` — nuevas variables validadas: `JOIN_DEADLINE_SECONDS` (600),
  `WORKER_INTERVAL_SECONDS` (15), `OUTBOX_MAX_ATTEMPTS` (10), `AUDIT_RETENTION_DAYS` (30),
  `DB_CONNECT_TIMEOUT_SECONDS` (30). Se mantienen el patrón actual de acumular todos los errores y
  salir con código 2. Las nuevas variables son opcionales: si faltan, se usa el valor por defecto.
- `main.go` — abre el pool con reintentos, construye repositorios → `UnitOfWork` → casos de uso →
  `RoomService`, arranca el worker y mantiene el apagado ordenado con `SIGINT`/`SIGTERM`.
- `infra/compose.yaml` e `infra/.env.example` — se pasan las variables nuevas al contenedor `api`.

## Fase 9 — Pruebas

- Dominio (puras, sin base de datos): tabla de transiciones (válidas y bloqueadas), estados
  terminales que no se reabren, permisos por rol, backoff del outbox.
- Aplicación con dobles (`usecases/fakes_test.go`, almacén en memoria con reversión):
  `CreateCall` deja llamada, miembros y evento en la misma transacción y revierte ante error;
  `EndCall` es idempotente y solo para el owner; `ActivateCall` activa y registra la sesión;
  `ReconcileCalls` cancela, finaliza por duración y reconcilia `ENDING→ENDED`;
  `ProcessOutbox` borra la sala, reintenta con backoff y abandona al agotar los intentos.
- Integración (base de datos real): van **junto al adaptador**, en
  `apps/api/internal/adapters/out/postgres/store_integration_test.go` con etiqueta `integration`,
  porque un paquete `internal/` no puede importarse desde `tests/` (regla de visibilidad de Go).
  Cada prueba crea un esquema aislado, aplica la migración inicial y lo borra al terminar.
  Cubren lectura de usuarios, ciclo de vida de la llamada con `version` optimista, listados,
  miembros, sesiones, deduplicación de eventos del proveedor, unicidad parcial del outbox y
  reversión de la transacción. `tests/integration/` queda para guion de laboratorio
  (scripts PowerShell y escenarios sobre la API HTTP de los pasos 7+).

## Fase 10 — Documentación

- `docs/arquitectura.md` — estado a paso 6, modelo de datos, tabla de transiciones, proceso
  periódico, puertos nuevos, `UnitOfWork`, controles automáticos y decisiones del paso.
- `docs/laboratorio.md` — comando real de migración, respaldo y restauración con
  `pg_dump`/`pg_restore`, purga de auditoría por retención, pruebas de integración y nota del
  límite de reconciliación descrito arriba.

## Verificación

```powershell
# 1. Compilación y pruebas unitarias (Windows, Go local)
cd apps/api
$env:PATH = "C:\Users\alurr\AppData\Local\Programs\go\bin;$env:PATH"
$env:GOTOOLCHAIN = "local"
go mod tidy
go vet ./...
go test ./...
go build ./cmd/server
go vet -tags integration ./...   # las pruebas de integración compilan

# 2. PostgreSQL real con Podman (la red interna de Compose)
podman compose --env-file infra/.env -f infra/compose.yaml up -d postgres
podman compose --env-file infra/.env -f infra/compose.yaml run --rm migrate

# 3. Integración: dentro de la red de Podman (los puertos publicados no llegan a Windows)
podman run --rm --network webrtc-lab_backend -v ${PWD}/apps/api:/app -w /app `
  -e TEST_DATABASE_URL=postgres://webrtc:CONTRASEÑA@postgres:5432/webrtc?sslmode=disable `
  docker.io/library/golang:1.26.3-alpine go test -tags integration ./...

# 4. La build del contenedor ejecuta vet y test: es el criterio del spec
podman compose --env-file infra/.env -f infra/compose.yaml build api
```

Criterio de terminado del paso 6 (spec:255): las migraciones aplican y revierten sin residuo, las
restricciones están activas, las transiciones están cubiertas por pruebas y el proceso periódico
cancela, cierra y reconcilia llamadas sin duplicar efectos.

## Riesgos y decisiones pendientes

1. **Fusión con el paso 7**: los archivos replicados verbatim evitan conflictos, pero `go.mod`,
   `go.sum`, `cmd/server/main.go` y `internal/adapters/in/http/doc.go` se tocarán en ambas ramas;
   habrá que resolverlos a mano (`go mod tidy` y rehacer el `main.go` con Fiber o con `net/http`
   según se decida mantener el paso 7 tal cual).
2. **Podman no está instalado**: el instalador descargado necesita una consola elevada. Mientras
   tanto no se puede aplicar la migración en un PostgreSQL real ni ejecutar las pruebas de
   integración; la verificación se limita a Go local.
3. **`infra/.env.example`** (antes `.env.exmaple`): renombrado en esta rama; la rama del paso 7
   hace el mismo rename, así que la fusión debe tomar una sola versión con las variables nuevas.
4. **`infra/.env.local` y `infra/.env.lan` están versionados** con secretos de relleno. No los toco;
   la rama del paso 7 ya los elimina.
5. **Alcance de la reconciliación**: `ACTIVE` sin sala no se cierra hasta que exista el evento de
   medios del paso 11; es una decisión de seguridad (no cerrar llamadas vivas) documentada.
