-- Paso 6: modelo de datos del laboratorio de videollamadas.
--
-- Convenciones:
--   * Fechas en UTC (timestamptz). La conexión de la API fija timezone=UTC.
--   * No se guardan tokens de medios ni contenidos de audio o video.
--   * Los identificadores son uuid generados por PostgreSQL (gen_random_uuid).
--   * room_name es único: una sala por llamada en el servidor de medios.

CREATE TABLE users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    username      text        NOT NULL,
    display_name  text        NOT NULL,
    -- Hash adaptativo (bcrypt) de la contraseña. Lo usa el paso 5.
    password_hash text        NOT NULL DEFAULT '',
    active        boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_username_key UNIQUE (username),
    CONSTRAINT users_username_not_blank CHECK (btrim(username) <> '')
);

CREATE TABLE auth_sessions (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Solo el hash del identificador opaco de sesión se persiste.
    session_token_hash text        NOT NULL,
    csrf_secret        text        NOT NULL,
    expires_at         timestamptz NOT NULL,
    revoked_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT auth_sessions_token_hash_key UNIQUE (session_token_hash)
);

CREATE INDEX auth_sessions_user_id_idx ON auth_sessions (user_id);
CREATE INDEX auth_sessions_expires_at_idx ON auth_sessions (expires_at) WHERE revoked_at IS NULL;

CREATE TABLE calls (
    id                   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    room_name            text        NOT NULL,
    owner_id             uuid        NOT NULL REFERENCES users (id),
    status               text        NOT NULL DEFAULT 'CREATED',
    created_at           timestamptz NOT NULL DEFAULT now(),
    -- Límite para que alguien se una a una llamada todavía vacía.
    join_deadline        timestamptz NOT NULL,
    started_at           timestamptz,
    ended_at             timestamptz,
    max_duration_seconds integer     NOT NULL,
    -- Versión para bloqueo optimista al cambiar el estado.
    version              integer     NOT NULL DEFAULT 0,
    CONSTRAINT calls_room_name_key UNIQUE (room_name),
    CONSTRAINT calls_status_check CHECK (status IN ('CREATED', 'ACTIVE', 'ENDING', 'ENDED', 'CANCELLED')),
    CONSTRAINT calls_max_duration_check CHECK (max_duration_seconds > 0),
    CONSTRAINT calls_version_check CHECK (version >= 0),
    CONSTRAINT calls_dates_check CHECK (ended_at IS NULL OR ended_at >= created_at)
);

CREATE INDEX calls_owner_id_idx ON calls (owner_id);
CREATE INDEX calls_status_idx ON calls (status);
CREATE INDEX calls_join_deadline_idx ON calls (join_deadline) WHERE status = 'CREATED';
CREATE INDEX calls_started_at_idx ON calls (started_at) WHERE status = 'ACTIVE';

CREATE TABLE call_members (
    call_id         uuid        NOT NULL REFERENCES calls (id) ON DELETE CASCADE,
    user_id         uuid        NOT NULL REFERENCES users (id),
    role            text        NOT NULL,
    -- Fuentes que el miembro puede publicar: camera, microphone, screen.
    allowed_sources text[]      NOT NULL DEFAULT ARRAY[]::text[],
    revoked_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (call_id, user_id),
    CONSTRAINT call_members_role_check CHECK (role IN ('creator', 'participant', 'viewer')),
    CONSTRAINT call_members_sources_check CHECK (allowed_sources <@ ARRAY['camera', 'microphone', 'screen'])
);

CREATE INDEX call_members_user_id_idx ON call_members (user_id);

CREATE TABLE call_sessions (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    call_id           uuid        NOT NULL REFERENCES calls (id) ON DELETE CASCADE,
    user_id           uuid        NOT NULL REFERENCES users (id),
    participant_sid   text,
    room_sid          text,
    joined_at         timestamptz NOT NULL DEFAULT now(),
    left_at           timestamptz,
    disconnect_reason text
);

CREATE INDEX call_sessions_call_id_idx ON call_sessions (call_id);
CREATE INDEX call_sessions_open_idx ON call_sessions (call_id) WHERE left_at IS NULL;

CREATE TABLE call_events (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    call_id           uuid        NOT NULL REFERENCES calls (id) ON DELETE CASCADE,
    event_type        text        NOT NULL,
    actor_id          uuid        REFERENCES users (id),
    occurred_at       timestamptz NOT NULL DEFAULT now(),
    -- Identificador del evento del proveedor, para deduplicar webhooks.
    provider_event_id text,
    details           jsonb       NOT NULL DEFAULT '{}'::jsonb
);

CREATE UNIQUE INDEX call_events_provider_event_id_key
    ON call_events (provider_event_id) WHERE provider_event_id IS NOT NULL;
CREATE INDEX call_events_call_id_idx ON call_events (call_id, occurred_at DESC);
CREATE INDEX call_events_occurred_at_idx ON call_events (occurred_at);

CREATE TABLE outbox_jobs (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    call_id         uuid        NOT NULL REFERENCES calls (id) ON DELETE CASCADE,
    operation       text        NOT NULL,
    attempts        integer     NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz,
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outbox_jobs_operation_check CHECK (operation IN ('delete_room')),
    CONSTRAINT outbox_jobs_attempts_check CHECK (attempts >= 0)
);

-- Una sola operación pendiente por llamada: repetir el cierre no duplica efectos.
CREATE UNIQUE INDEX outbox_jobs_pending_key
    ON outbox_jobs (call_id, operation) WHERE completed_at IS NULL;
CREATE INDEX outbox_jobs_due_idx ON outbox_jobs (next_attempt_at) WHERE completed_at IS NULL;
