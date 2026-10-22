# Diagramas de flujo de la aplicación WebRTC

Versión: 1.0 · Fecha: 22 de octubre de 2026 · Basado en: `SPEC-tecnico-app-WebRTC.md` v1.2 y `API-endpoints.md` v1.0.

Los diagramas usan Mermaid. Participantes comunes:

| Nombre | Componente |
| --- | --- |
| `UI` | Componentes y presenter/store Svelte. |
| `UC` | Casos de uso del frontend (`SignIn`, `JoinCall`, `EndCall`, etc.). |
| `MS` | Adaptador `MediaSession` sobre `livekit-client`. |
| `Caddy` | Proxy HTTPS/WSS y servidor de la SPA. |
| `API` | Backend Go (handlers + casos de uso). |
| `DB` | PostgreSQL. |
| `LK` | LiveKit (SFU + TURN integrado). |
| `Worker` | Goroutine de cierres y reconciliación dentro del servicio Go. |

## Índice

1. [Vista general de rutas](#1-vista-general-de-rutas)
2. [Inicio de sesión](#2-inicio-de-sesión)
3. [Cierre de sesión](#3-cierre-de-sesión)
4. [Crear llamada e invitar](#4-crear-llamada-e-invitar)
5. [Abrir el enlace y antesala](#5-abrir-el-enlace-y-antesala)
6. [Unirse a la llamada](#6-unirse-a-la-llamada)
7. [Emisión del join-token (decisión en el backend)](#7-emisión-del-join-token-decisión-en-el-backend)
8. [Controles de cámara, micrófono y pantalla](#8-controles-de-cámara-micrófono-y-pantalla)
9. [Reconexión](#9-reconexión)
10. [Salir de la llamada](#10-salir-de-la-llamada)
11. [Finalizar para todos](#11-finalizar-para-todos)
12. [Procesamiento de webhooks](#12-procesamiento-de-webhooks)
13. [Trabajador de cierres y reconciliación](#13-trabajador-de-cierres-y-reconciliación)
14. [Ciclo de vida de la llamada](#14-ciclo-de-vida-de-la-llamada)
15. [Estados de la interfaz](#15-estados-de-la-interfaz)

---

## 1. Vista general de rutas

Separa las tres rutas: negocio (HTTPS `/api`), señalización (WSS) y medios (WebRTC UDP/TCP o TURN).

```mermaid
flowchart LR
    subgraph Cliente[Navegador]
        UI[UI Svelte] --> UC[Casos de uso]
        UC --> GW[AuthGateway / CallGateway]
        UC --> MS[MediaSession]
    end

    subgraph Host[Host de laboratorio - Podman]
        Caddy[Caddy :8443]
        API[API Go :8080]
        DB[(PostgreSQL :5432)]
        LK[LiveKit :7880]
        W[Worker]
    end

    GW -->|"HTTPS /api/v1/*"| Caddy
    Caddy -->|HTTP interno| API
    MS -->|"WSS rtc.webrtc.test"| Caddy
    Caddy -->|WebSocket interno| LK
    MS <-->|"WebRTC UDP 7882 / TCP 7881 / TURN 3478-5349"| LK

    API --> DB
    API -->|API administrativa| LK
    LK -->|"Webhook firmado"| API
    W --> DB
    W -->|"DeleteRoom / ListRooms"| LK
```

---

## 2. Inicio de sesión

```mermaid
sequenceDiagram
    autonumber
    actor U as Usuario
    participant UI
    participant UC as SignIn
    participant API
    participant DB

    U->>UI: Ingresa usuario y contraseña
    UI->>UC: signIn(username, password)
    UC->>API: POST /api/v1/auth/login
    API->>API: Validar Origin y límite de intentos
    alt Origin no permitido
        API-->>UC: 403 ORIGIN_NOT_ALLOWED
    else Demasiados intentos
        API-->>UC: 429 RATE_LIMITED
    else Continuar
        API->>DB: Buscar usuario activo por username
        API->>API: Comparar contraseña con hash bcrypt
        alt Credenciales inválidas
            API-->>UC: 401 INVALID_CREDENTIALS
            UC-->>UI: Mensaje genérico de error
        else Credenciales válidas
            API->>API: Generar token opaco y secreto CSRF
            API->>DB: INSERT auth_sessions con hash del token
            API-->>UC: 204 + Set-Cookie session
            UC->>API: GET /api/v1/auth/me
            API->>DB: Validar sesión vigente
            API-->>UC: 200 user + csrfToken
            UC-->>UI: Sesión iniciada
            UI-->>U: Pantalla principal
        end
    end
```

---

## 3. Cierre de sesión

```mermaid
sequenceDiagram
    autonumber
    participant UI
    participant UC as Logout
    participant API
    participant DB

    UI->>UC: logout()
    UC->>API: POST /api/v1/auth/logout con cookie y X-CSRF-Token
    API->>DB: Validar sesión y CSRF
    alt CSRF inválido
        API-->>UC: 403 CSRF_INVALID
    else Válido
        API->>DB: UPDATE auth_sessions SET revoked_at = now()
        API-->>UC: 204 + cookie eliminada
        UC-->>UI: Volver a login
    end
```

---

## 4. Crear llamada e invitar

```mermaid
sequenceDiagram
    autonumber
    actor C as Creador
    participant UI
    participant UC as CreateCall
    participant API
    participant DB

    C->>UI: Abrir "Nueva llamada"
    UI->>API: GET /api/v1/users
    API->>DB: SELECT usuarios activos
    API-->>UI: 200 lista de usuarios
    C->>UI: Selecciona invitado y rol
    UI->>UC: createCall(inviteeUserId, inviteeRole)
    UC->>API: POST /api/v1/calls + Idempotency-Key + X-CSRF-Token
    API->>DB: Buscar llamada por owner_id + idempotency_key
    alt Clave ya usada con el mismo cuerpo
        API-->>UC: 201 misma llamada
    else Clave ya usada con otro cuerpo
        API-->>UC: 409 IDEMPOTENCY_CONFLICT
    else Nueva
        API->>API: Validar invitado activo, distinto y rol permitido
        rect rgba(120,120,120,0.12)
            note over API,DB: Transacción
            API->>DB: INSERT calls status CREATED, join_deadline = now + 10 min
            API->>DB: INSERT call_members OWNER
            API->>DB: INSERT call_members PARTICIPANT o VIEWER
            API->>DB: INSERT call_events CALL_CREATED
        end
        API-->>UC: 201 callId, joinUrl, status CREATED
    end
    UC-->>UI: Mostrar enlace de invitación
    C-->>C: Comparte joinUrl con el invitado
```

---

## 5. Abrir el enlace y antesala

```mermaid
sequenceDiagram
    autonumber
    actor I as Invitado
    participant UI
    participant UC as LoadCall / PrepareDevices
    participant DA as DeviceAccess
    participant API
    participant DB

    I->>UI: Abre /calls/{id}
    UI->>API: GET /api/v1/auth/me
    alt Sin sesión
        API-->>UI: 401
        UI-->>I: Redirigir a login y volver al enlace
    end
    UI->>UC: loadCall(id)
    UC->>API: GET /api/v1/calls/{id}
    API->>DB: Verificar membresía no revocada
    alt No es miembro o no existe
        API-->>UC: 404 CALL_NOT_FOUND
        UC-->>UI: Acceso rechazado
    else Llamada ENDED, CANCELLED o ENDING
        API-->>UC: 200 con status final
        UC-->>UI: Mostrar "La llamada terminó"
    else CREATED o ACTIVE
        API-->>UC: 200 status, role, permissions, joinDeadline
        UC->>DA: Pedir vista previa según permisos del rol
        alt Permiso denegado o sin dispositivo
            DA-->>UC: Error de dispositivo
            UC-->>UI: Explicar y ofrecer continuar con lo disponible
        else Permiso concedido
            DA-->>UC: Lista de cámaras y micrófonos + vista previa
            UC-->>UI: Antesala lista, sin publicar
        end
    end
```

---

## 6. Unirse a la llamada

```mermaid
sequenceDiagram
    autonumber
    actor U as Usuario
    participant UI
    participant UC as JoinCall
    participant MS as MediaSession
    participant API
    participant DB
    participant LK

    U->>UI: Clic en "Unirse"
    UI->>UC: joinCall(callId, dispositivos)
    UC-->>UI: Estado Conectando, botón deshabilitado
    UC->>API: POST /api/v1/calls/{id}/join-token
    note over API,LK: Ver diagrama 7
    alt Rechazado
        API-->>UC: 403 / 404 / 409 / 410
        UC-->>UI: Acceso rechazado o llamada terminada
    else Concedido
        API-->>UC: 200 serverUrl, participantToken, permissions
        UC->>MS: connect(serverUrl, token)
        MS->>LK: WSS + negociación ICE
        LK-->>MS: Conectado
        MS-->>UC: ConnectionChanged connected
        UC->>MS: Publicar pistas permitidas por el rol
        MS->>LK: Publicar micrófono y cámara
        LK-->>MS: Pistas remotas disponibles
        MS-->>UC: ParticipantUpdated / TrackStateChanged
        UC-->>UI: En llamada
        LK->>API: Webhook participant_joined
        API->>DB: INSERT call_sessions y, si es el primero, calls.status = ACTIVE
    end
```

---

## 7. Emisión del join-token (decisión en el backend)

```mermaid
flowchart TD
    A[POST /calls/:id/join-token] --> B{Sesión válida?}
    B -- No --> E401[401 UNAUTHENTICATED]
    B -- Sí --> C{CSRF y Origin válidos?}
    C -- No --> E403a[403 CSRF_INVALID]
    C -- Sí --> D[BEGIN + SELECT calls ... FOR UPDATE]
    D --> F{Miembro no revocado?}
    F -- No --> E404[404 CALL_NOT_FOUND]
    F -- Sí --> G{Estado de la llamada}
    G -- "ENDING / ENDED / CANCELLED" --> E410[410 CALL_ENDED]
    G -- CREATED --> H{Antes de join_deadline?}
    H -- No --> E410
    H -- Sí --> I
    G -- ACTIVE --> J{Dentro de max_duration?}
    J -- No --> E410
    J -- Sí --> I[Calcular permisos desde role y allowed_sources]
    I --> K[COMMIT]
    K --> L[RoomService: crear o asegurar sala<br/>room_name único, max_participants = 2]
    L --> M{LiveKit disponible?}
    M -- No --> E503[503 DEPENDENCY_UNAVAILABLE]
    M -- Sí --> N[JoinTokenIssuer: firmar JWT<br/>roomJoin a la sala, canSubscribe,<br/>canPublishSources por rol,<br/>sin datos ni admin, TTL 120 s]
    N --> O[INSERT call_events TOKEN_ISSUED]
    O --> P[200 + Cache-Control: no-store]
```

---

## 8. Controles de cámara, micrófono y pantalla

### 8.1. Cámara y micrófono

```mermaid
sequenceDiagram
    autonumber
    actor U as Usuario
    participant UI
    participant UC as SetCameraEnabled / SetMicrophoneEnabled
    participant MS as MediaSession
    participant LK

    U->>UI: Clic en cámara o micrófono
    UI->>UC: setEnabled(fuente, valor)
    UC->>UC: Verificar permiso del rol
    alt Rol sin permiso
        UC-->>UI: Control deshabilitado
    else Permitido
        UC->>MS: setSourceEnabled(fuente, valor)
        alt Apagar cámara
            MS->>MS: Detener captura de video
            MS->>LK: Despublicar o silenciar pista
        else Encender
            MS->>LK: Publicar o reactivar pista
        end
        MS-->>UC: TrackStateChanged
        UC-->>UI: Reflejar estado real de la pista
    end
```

### 8.2. Compartir pantalla

El clic invoca el caso de uso y este llama al puerto de captura de inmediato, sin esperas de red previas, para conservar la activación del usuario.

```mermaid
sequenceDiagram
    autonumber
    actor U as Usuario
    participant UI
    participant UC as StartScreenShare / StopScreenShare
    participant MS as MediaSession
    participant B as Selector del navegador
    participant LK

    U->>UI: Clic en "Compartir pantalla"
    UI->>UC: startScreenShare()
    UC->>MS: startScreenShare() de inmediato
    MS->>B: getDisplayMedia
    alt Usuario cancela el selector
        B-->>MS: NotAllowedError
        MS-->>UC: Captura cancelada
        UC-->>UI: Botón en estado inactivo, la llamada continúa
    else Usuario elige pantalla
        B-->>MS: Pista de pantalla
        MS->>LK: Publicar fuente screen_share
        MS-->>UC: TrackStateChanged screen on
        UC-->>UI: Botón activo
    end

    opt Usuario detiene desde el navegador
        B-->>MS: Evento ended de la pista
        MS->>LK: Despublicar pantalla
        MS-->>UC: TrackStateChanged screen off
        UC-->>UI: Botón inactivo
    end
```

---

## 9. Reconexión

```mermaid
sequenceDiagram
    autonumber
    participant UI
    participant UC as JoinCall
    participant MS as MediaSession
    participant API
    participant LK

    LK--xMS: Pérdida de red
    MS-->>UC: ConnectionChanged reconnecting
    UC-->>UI: Mensaje "Reconectando"
    alt El SDK recupera la sesión
        MS->>LK: Reanudar con credencial renovada por LiveKit
        LK-->>MS: Reconectado
        MS-->>UC: ConnectionChanged connected
        UC-->>UI: En llamada, sin duplicar pistas
    else El SDK requiere nueva conexión
        MS-->>UC: ConnectionChanged disconnected
        UC->>API: GET /api/v1/calls/{id}
        alt Llamada terminada
            API-->>UC: status ENDING / ENDED
            UC->>MS: Liberar dispositivos
            UC-->>UI: Explicar que la llamada terminó
        else Llamada vigente
            UC->>API: POST /api/v1/calls/{id}/join-token
            API-->>UC: Nuevo token tras revalidar acceso
            UC->>MS: connect(serverUrl, token)
            MS->>LK: Nueva conexión
            note over UC,MS: No volver a compartir pantalla automáticamente
            UC-->>UI: En llamada
        end
    else No se recupera en el plazo esperado
        UC-->>UI: Error explícito y opción de reintentar
    end
```

---

## 10. Salir de la llamada

"Salir" solo afecta al usuario actual; no cambia el estado de la llamada.

```mermaid
sequenceDiagram
    autonumber
    actor U as Usuario
    participant UI
    participant UC as LeaveCall
    participant MS as MediaSession
    participant LK
    participant API
    participant DB

    U->>UI: Clic en "Salir"
    UI->>UC: leaveCall()
    UC->>MS: disconnect()
    MS->>LK: Cerrar conexión
    MS->>MS: Detener pistas locales y retirar listeners
    MS-->>UC: ConnectionChanged disconnected
    UC-->>UI: Vista de salida
    LK->>API: Webhook participant_left
    API->>DB: UPDATE call_sessions SET left_at, disconnect_reason
```

---

## 11. Finalizar para todos

```mermaid
sequenceDiagram
    autonumber
    actor C as Creador
    participant UI
    participant UC as EndCall
    participant API
    participant DB
    participant W as Worker
    participant LK
    participant MS2 as MediaSession del otro usuario

    C->>UI: Clic en "Finalizar para todos" y confirma
    UI->>UC: endCall(callId)
    UC->>API: POST /api/v1/calls/{id}/end
    API->>DB: SELECT calls ... FOR UPDATE
    alt No es OWNER
        API-->>UC: 403 FORBIDDEN
    else Ya en ENDING o ENDED
        API-->>UC: 202 con estado actual
    else CREATED o ACTIVE
        rect rgba(120,120,120,0.12)
            note over API,DB: Transacción
            API->>DB: UPDATE calls status ENDING, version + 1
            API->>DB: INSERT outbox_jobs DELETE_ROOM
            API->>DB: INSERT call_events CALL_END_REQUESTED
        end
        API-->>UC: 202 status ENDING
    end
    UC-->>UI: Cerrando llamada

    W->>DB: Tomar outbox_jobs pendientes
    W->>LK: DeleteRoom(room_name)
    alt Sala eliminada o inexistente
        LK-->>MS2: Desconexión por servidor
        MS2->>MS2: Liberar dispositivos
        W->>DB: calls status ENDED, ended_at, job completed_at
        W->>DB: INSERT call_events CALL_ENDED
    else Error transitorio
        W->>DB: attempts + 1, next_attempt_at con backoff
    end

    MS2->>API: GET /api/v1/calls/{id}
    API-->>MS2: status ENDED
```

---

## 12. Procesamiento de webhooks

```mermaid
flowchart TD
    A[POST /api/v1/webhooks/livekit] --> B[Leer cuerpo original]
    B --> C{Firma válida con SDK Go?}
    C -- No --> E401[401]
    C -- Sí --> D[Traducir evento a tipo propio]
    D --> F[BEGIN]
    F --> G{provider_event_id ya existe?}
    G -- Sí --> R204a[ROLLBACK + 204 sin efectos]
    G -- No --> H[Resolver llamada por room_name]
    H --> I{Tipo de evento}
    I -- participant_joined --> J[INSERT call_sessions<br/>participant_sid, room_sid, joined_at]
    J --> K{Llamada en CREATED?}
    K -- Sí --> L[calls.status = ACTIVE, started_at]
    K -- No --> M
    L --> M
    I -- participant_left --> N[UPDATE call_sessions left_at, disconnect_reason]
    N --> M
    I -- room_finished --> O{Llamada en ENDING?}
    O -- Sí --> P[calls.status = ENDED, completar job]
    O -- No --> M
    P --> M
    I -- otros --> M
    M[INSERT call_events con provider_event_id] --> Q[COMMIT]
    Q --> R204[204]
    F -. error de BD .-> E503[503 y LiveKit reintenta]
```

Regla transversal: un evento nunca mueve una llamada de `ENDING`, `ENDED` o `CANCELLED` a un estado anterior.

---

## 13. Trabajador de cierres y reconciliación

Goroutine única del servicio Go, con cancelación por contexto y apagado ordenado.

```mermaid
flowchart TD
    T[Tick periódico] --> A[Cancelar vencidas:<br/>CREATED con join_deadline pasado<br/>y sin sesiones]
    A --> A1[calls.status = CANCELLED<br/>+ call_events CALL_CANCELLED]
    A1 --> B[Detectar duración excedida:<br/>ACTIVE con started_at + max_duration vencido]
    B --> B1[Transacción: status ENDING<br/>+ outbox_jobs DELETE_ROOM]
    B1 --> C[Procesar outbox_jobs pendientes<br/>con next_attempt_at alcanzado]
    C --> D{DeleteRoom OK o sala ausente?}
    D -- Sí --> D1[status ENDED, ended_at,<br/>completed_at del job]
    D -- No --> D2[attempts + 1,<br/>next_attempt_at con backoff]
    D1 --> E
    D2 --> E[Reconciliar con LiveKit:<br/>ListRooms y ListParticipants]
    E --> F{Diferencias}
    F -- "Sala en LK para llamada ENDED" --> F1[Crear outbox_jobs DELETE_ROOM]
    F -- "room_sid distinto al registrado" --> F2[Registrar sala recreada<br/>en call_events]
    F -- "Participante en LK sin call_session" --> F3[Crear call_session<br/>y activar si estaba CREATED]
    F -- "call_session abierta sin participante en LK" --> F4[Cerrar call_session<br/>disconnect_reason RECONCILED]
    F -- Ninguna --> Z[Esperar siguiente tick]
    F1 --> Z
    F2 --> Z
    F3 --> Z
    F4 --> Z
```

---

## 14. Ciclo de vida de la llamada

```mermaid
stateDiagram-v2
    [*] --> CREATED: POST /calls
    CREATED --> ACTIVE: primer participant_joined
    CREATED --> CANCELLED: join_deadline vencido sin conexiones
    CREATED --> ENDING: POST /end del creador
    ACTIVE --> ENDING: POST /end del creador
    ACTIVE --> ENDING: duración máxima superada
    ENDING --> ENDED: DeleteRoom confirmado o sala ausente
    ENDING --> ENDING: reintento de DeleteRoom
    ENDED --> [*]
    CANCELLED --> [*]
```

| Estado | Emite tokens | Acepta `/end` |
| --- | --- | --- |
| `CREATED` | Sí, antes de `join_deadline` | Sí |
| `ACTIVE` | Sí, dentro de la duración máxima | Sí |
| `ENDING` | No (`410`) | Sí, idempotente |
| `ENDED` | No (`410`) | Sí, idempotente |
| `CANCELLED` | No (`410`) | No (`409`) |

---

## 15. Estados de la interfaz

```mermaid
stateDiagram-v2
    [*] --> Login
    Login --> Inicio: sesión iniciada
    Inicio --> Antesala: crear llamada o abrir enlace
    Antesala --> PermisoDenegado: sin permiso o sin dispositivo
    PermisoDenegado --> Antesala: reintentar o continuar con lo disponible
    Antesala --> Conectando: Unirse
    Conectando --> EnLlamada: conectado
    Conectando --> Rechazado: 403 / 404 / 410 / token inválido
    EnLlamada --> Reconectando: red degradada
    Reconectando --> EnLlamada: recuperado
    Reconectando --> Error: no recuperado
    Error --> Conectando: reintentar
    EnLlamada --> Salida: Salir
    EnLlamada --> Finalizada: cierre por el creador o por el servidor
    Rechazado --> Inicio
    Salida --> Inicio
    Finalizada --> Inicio
    Inicio --> Login: cerrar sesión
```

Al entrar en `Rechazado`, `Salida`, `Finalizada` o `Error`, la app detiene las capturas, libera los dispositivos y explica la causa.
