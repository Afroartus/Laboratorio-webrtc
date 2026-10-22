# adapters/livekit

Implementación de `MediaSession` con `livekit-client`. Posee la instancia del SDK y sus listeners y
traduce sus eventos a eventos propios (`ConnectionChanged`, `ParticipantUpdated`, `TrackStateChanged`).
