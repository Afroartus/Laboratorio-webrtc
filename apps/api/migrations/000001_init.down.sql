-- Revierte el modelo de datos del paso 6. El orden respeta las dependencias.
DROP TABLE IF EXISTS outbox_jobs;
DROP TABLE IF EXISTS call_events;
DROP TABLE IF EXISTS call_sessions;
DROP TABLE IF EXISTS call_members;
DROP TABLE IF EXISTS calls;
DROP TABLE IF EXISTS auth_sessions;
DROP TABLE IF EXISTS users;
