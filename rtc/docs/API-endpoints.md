# API de la aplicación WebRTC: endpoints

Versión: 1.0 · Fecha: 22 de octubre de 2026 · Basado en: `SPEC-tecnico-app-WebRTC.md` v1.2 (paso 7).

Los valores marcados como *(propuesta)* no están fijados en el spec y se confirmarán durante la implementación. El contrato definitivo se publicará en OpenAPI, sincronizado con los handlers Go.

## Convenciones generales

| Aspecto | Valor |
| --- | --- |
| URL base (navegador) | `https://webrtc.test:8443` (mismo origen que la web; Caddy enruta `/api` al backend). |
| URL interna (webhook) | `http://api:8080` (solo red de contenedores). |
| Formato | JSON UTF-8 (`Content-Type: application/json`). Fechas en ISO-8601 UTC. |
| Autenticación de usuario | Cookie de sesión opaca `HttpOnly`, `Secure`, `SameSite=Lax`. Nombre: `session` *(propuesta)*. |
| CSRF | Toda mutación autenticada envía el encabezado `X-CSRF-Token` *(nombre propuesto)* con el valor obtenido en `GET /api/v1/auth/me`. Además se valida `Origin` contra `ALLOWED_WEB_ORIGINS`. |
| Trazabilidad | Cada respuesta incluye `X-Request-Id`; el mismo valor aparece como `requestId` en los errores. |

### Formato de error

```json
{
  "code": "CALL_ENDED",
  "message": "La llamada terminó",
  "requestId": "01J...."
}
```

| HTTP | Significado | Códigos `code` de ejemplo *(propuesta)* |
| --- | --- | --- |
| `400` | Cuerpo o parámetros con formato inválido | `VALIDATION_ERROR` |
| `401` | Sesión inválida, vencida o credenciales incorrectas | `UNAUTHENTICATED`, `INVALID_CREDENTIALS` |
| `403` | Operación no permitida (rol, CSRF u origen) | `FORBIDDEN`, `CSRF_INVALID`, `ORIGIN_NOT_ALLOWED` |
| `404` | Llamada inexistente o no visible para el usuario | `CALL_NOT_FOUND` |
| `409` | Conflicto de estado o de idempotencia | `CALL_STATE_CONFLICT`, `IDEMPOTENCY_CONFLICT` |
| `410` | Llamada terminada, para un miembro autorizado | `CALL_ENDED` |
| `429` | Límite de solicitudes superado | `RATE_LIMITED` |
| `503` | Dependencia no disponible (PostgreSQL, LiveKit) | `DEPENDENCY_UNAVAILABLE` |

### Tipos compartidos

| Tipo | Valores |
| --- | --- |
| `CallStatus` | `CREATED`, `ACTIVE`, `ENDING`, `ENDED`, `CANCELLED` |
| `Role` | `OWNER` (creador), `PARTICIPANT`, `VIEWER` (espectador) |
| `Permissions` | `{ publishCamera: boolean, publishMicrophone: boolean, publishScreen: boolean, subscribe: boolean }` |

Permisos por rol:

| Rol | `publishCamera` | `publishMicrophone` | `publishScreen` | `subscribe` | Puede finalizar |
| --- | --- | --- | --- | --- | --- |
| `OWNER` | `true` | `true` | `true` | `true` | Sí |
| `PARTICIPANT` | `true` | `true` | `true` | `true` | No |
| `VIEWER` | `false` | `false` | `false` | `true` | No |

## Resumen

| Método | Ruta | Autenticación | CSRF |
| --- | --- | --- | --- |
| `POST` | `/api/v1/auth/login` | Credenciales | Validación de `Origin` |
| `POST` | `/api/v1/auth/logout` | Cookie | Sí |
| `GET` | `/api/v1/auth/me` | Cookie | No |
| `GET` | `/api/v1/users` | Cookie | No |
| `POST` | `/api/v1/calls` | Cookie | Sí |
| `GET` | `/api/v1/calls/{id}` | Cookie | No |
| `POST` | `/api/v1/calls/{id}/join-token` | Cookie | Sí |
| `POST` | `/api/v1/calls/{id}/end` | Cookie | Sí |
| `POST` | `/api/v1/webhooks/livekit` | Firma LiveKit | No aplica |
| `GET` | `/health/live` | Ninguna | No aplica |
| `GET` | `/health/ready` | Ninguna | No aplica |

---

## Autenticación

### `POST /api/v1/auth/login`

Inicia sesión con un usuario local de laboratorio.

**Cuerpo**

```json
{
  "username": "usuario1",
  "password": "********"
}
```

**Respuesta `204 No Content`**

- `Set-Cookie: session=<token-opaco>; HttpOnly; Secure; SameSite=Lax; Path=/; Expires=<fecha>`
- El cliente obtiene el usuario y el token CSRF con `GET /api/v1/auth/me`.

**Reglas**

- Valida credenciales contra el hash adaptativo (bcrypt) almacenado.
- Limita intentos por usuario e IP.
- Protección contra login CSRF: rechaza solicitudes cuyo `Origin` no esté en `ALLOWED_WEB_ORIGINS`.
- Si existía una sesión previa en la cookie, se reemplaza.
- El mensaje de error no distingue entre usuario inexistente, inactivo o contraseña incorrecta.

**Errores:** `400`, `401 INVALID_CREDENTIALS`, `403 ORIGIN_NOT_ALLOWED`, `429`, `503`.

### `POST /api/v1/auth/logout`

Cierra la sesión actual.

**Encabezados:** cookie de sesión, `X-CSRF-Token`.

**Cuerpo:** vacío.

**Respuesta `204 No Content`**

- Marca la sesión como revocada (`revoked_at`) en PostgreSQL.
- `Set-Cookie: session=; Max-Age=0; HttpOnly; Secure; SameSite=Lax; Path=/`

**Errores:** `401`, `403 CSRF_INVALID`.

### `GET /api/v1/auth/me`

Devuelve el usuario autenticado y el token CSRF de la sesión.

**Encabezados:** cookie de sesión.

**Respuesta `200 OK`**

```json
{
  "user": {
    "id": "<uuid>",
    "username": "usuario1",
    "displayName": "Usuario Uno"
  },
  "csrfToken": "<token-csrf>",
  "sessionExpiresAt": "2026-10-22T18:00:00Z"
}
```

**Errores:** `401`.

---

## Usuarios

### `GET /api/v1/users`

Lista los usuarios locales activos para elegir a quién invitar.

**Encabezados:** cookie de sesión.

**Respuesta `200 OK`**

```json
{
  "users": [
    { "id": "<uuid>", "displayName": "Usuario Uno" },
    { "id": "<uuid>", "displayName": "Usuario Dos" }
  ]
}
```

**Reglas**

- Solo usuarios con `active = true`.
- No expone hashes, sesiones ni datos adicionales.

**Errores:** `401`.

---

## Llamadas

### `POST /api/v1/calls`

Crea una llamada 1:1. El usuario autenticado queda como `OWNER` y el invitado con el rol indicado.

**Encabezados**

| Encabezado | Obligatorio | Descripción |
| --- | --- | --- |
| Cookie de sesión | Sí | |
| `X-CSRF-Token` | Sí | |
| `Idempotency-Key` | No | Identificador único del intento (por ejemplo UUID). Repetir la solicitud con la misma clave y el mismo cuerpo devuelve la misma llamada sin crear otra. |

**Cuerpo**

```json
{
  "inviteeUserId": "<uuid>",
  "inviteeRole": "PARTICIPANT"
}
```

| Campo | Tipo | Reglas |
| --- | --- | --- |
| `inviteeUserId` | UUID | Usuario activo y distinto del creador. |
| `inviteeRole` | `PARTICIPANT` \| `VIEWER` | No se admite `OWNER`. |

**Respuesta `201 Created`**

```json
{
  "callId": "<uuid>",
  "joinUrl": "https://webrtc.test:8443/calls/<uuid>",
  "status": "CREATED"
}
```

**Reglas**

- Crea `calls` y los dos `call_members` en una sola transacción.
- Fija `join_deadline` (10 minutos) y `max_duration_seconds` (`CALL_MAX_DURATION_SECONDS`).
- `joinUrl` no contiene tokens de LiveKit.

**Errores:** `400`, `401`, `403 CSRF_INVALID`, `409 IDEMPOTENCY_CONFLICT` (misma clave con otro cuerpo), `503`.

### `GET /api/v1/calls/{id}`

Consulta el estado de una llamada y los permisos del usuario actual. El cliente la usa al cargar la antesala y como respaldo cada 10 segundos mientras la llamada está visible.

**Parámetros de ruta:** `id` (UUID de la llamada).

**Encabezados:** cookie de sesión.

**Respuesta `200 OK`**

```json
{
  "callId": "<uuid>",
  "status": "ACTIVE",
  "role": "PARTICIPANT",
  "permissions": {
    "publishCamera": true,
    "publishMicrophone": true,
    "publishScreen": true,
    "subscribe": true
  },
  "joinDeadline": "2026-10-22T17:10:00Z"
}
```

**Reglas**

- Solo miembros no revocados de la llamada. Para cualquier otro usuario responde `404` (no revela la existencia de la llamada).
- Una llamada terminada se devuelve con `200` y su `status` final, para que el cliente explique el motivo.

**Errores:** `400`, `401`, `404`.

### `POST /api/v1/calls/{id}/join-token`

Emite las credenciales temporales para conectarse a la sala LiveKit.

**Parámetros de ruta:** `id` (UUID de la llamada).

**Encabezados:** cookie de sesión, `X-CSRF-Token`.

**Cuerpo:** vacío. Identidad, sala y permisos se determinan en el servidor a partir de la sesión y los datos persistidos; cualquier campo enviado se ignora.

**Respuesta `200 OK`** · `Cache-Control: no-store`

```json
{
  "callId": "<uuid>",
  "roomName": "call_<uuid>",
  "serverUrl": "wss://rtc.webrtc.test:8443",
  "participantToken": "<jwt-temporal>",
  "expiresAt": "2026-10-22T17:02:00Z",
  "permissions": {
    "publishCamera": true,
    "publishMicrophone": true,
    "publishScreen": true,
    "subscribe": true
  }
}
```

**Reglas**

- Revalida en cada solicitud: sesión, pertenencia, rol, estado de la llamada y plazo de ingreso. Se serializa con el cierre sobre el registro de la llamada.
- Antes de firmar, crea o asegura la sala con nombre único y límite de dos participantes.
- El JWT restringe `roomJoin` a esa sala, permite suscripción, limita la publicación a las fuentes del rol, deshabilita publicación de datos y no concede administración de salas.
- TTL de la credencial inicial: `JOIN_TOKEN_TTL_SECONDS` (propuesta: 120 s).
- `serverUrl` es la URL WSS alcanzable desde el navegador, nunca la dirección interna del contenedor.
- El cliente guarda el token solo en memoria. Ante una reconexión que requiera nueva autorización, vuelve a llamar a este endpoint.

**Errores:** `400`, `401`, `403 CSRF_INVALID`, `404`, `409 CALL_STATE_CONFLICT`, `410 CALL_ENDED` (`ENDING`, `ENDED`, `CANCELLED` o plazo de ingreso vencido), `429`, `503 DEPENDENCY_UNAVAILABLE` (LiveKit no disponible).

### `POST /api/v1/calls/{id}/end`

Finaliza la llamada para todos los participantes.

**Parámetros de ruta:** `id` (UUID de la llamada).

**Encabezados:** cookie de sesión, `X-CSRF-Token`.

**Cuerpo:** vacío.

**Respuesta `202 Accepted`**

```json
{
  "status": "ENDING"
}
```

**Reglas**

- Solo el creador (`OWNER`).
- Guarda `ENDING` y la tarea de cierre en `outbox_jobs` en la misma transacción; a partir de ese momento se deniegan nuevos tokens.
- El cierre efectivo (`DeleteRoom` en LiveKit) se ejecuta de forma asíncrona con reintentos; la llamada pasa a `ENDED` al confirmar la eliminación o ausencia de la sala.
- Idempotente: si la llamada ya está en `ENDING` o `ENDED`, responde `202` con el estado actual sin duplicar efectos ni auditoría.

**Errores:** `400`, `401`, `403 FORBIDDEN` (miembro sin rol `OWNER`), `403 CSRF_INVALID`, `404`, `409 CALL_STATE_CONFLICT` (por ejemplo, llamada `CANCELLED`).

---

## Webhooks

### `POST /api/v1/webhooks/livekit`

Recibe los eventos de LiveKit. Solo es accesible por la red interna de contenedores (`http://api:8080/api/v1/webhooks/livekit`); no se publica a través de Caddy.

**Encabezados**

| Encabezado | Descripción |
| --- | --- |
| `Authorization` | JWT firmado por LiveKit con `LIVEKIT_API_KEY`/`LIVEKIT_API_SECRET`, que incluye el hash del cuerpo. |
| `Content-Type` | `application/webhook+json` |

**Cuerpo:** evento de LiveKit (`room_started`, `room_finished`, `participant_joined`, `participant_left`, `track_published`, `track_unpublished`, etc.), con su `id` único.

**Respuesta `204 No Content`**, después de persistir el evento.

**Reglas**

- Valida la firma con el SDK Go sobre el cuerpo original, sin reserializarlo.
- No usa cookie de usuario ni CSRF.
- Deduplica por identificador del evento (`provider_event_id`): un evento repetido responde `204` sin efectos.
- Traduce el evento e invoca `HandleMediaEvent` (por ejemplo, `ACTIVE` al primer participante conectado; registro en `call_sessions` con `participant_sid` y `room_sid`).
- Un evento tardío no reabre una llamada terminada.

**Errores:** `400` (cuerpo ilegible), `401` (firma inválida o ausente), `503` (no se pudo persistir; LiveKit reintentará).

---

## Salud

### `GET /health/live`

Indica que el proceso está en ejecución. No comprueba dependencias.

**Respuesta `200 OK`**

```json
{ "status": "ok" }
```

### `GET /health/ready`

Indica si la API puede atender solicitudes. Comprueba PostgreSQL y la API administrativa de LiveKit.

**Respuesta `200 OK`**

```json
{
  "status": "ready",
  "checks": {
    "database": "ok",
    "livekit": "ok"
  }
}
```

**Respuesta `503 Service Unavailable`**

```json
{
  "status": "not_ready",
  "checks": {
    "database": "ok",
    "livekit": "unavailable"
  }
}
```

**Reglas:** sin autenticación; no revela versiones, direcciones internas, cadenas de conexión ni secretos.
