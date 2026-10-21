/** Acciones del perfil simulado. La presentación no conoce los dobles ni el DOM de los medios. */
export interface CallSimulation {
  enterAsInvitee(): Promise<string | null>
  enterAsSpectator(): Promise<string | null>
  returnAsCreator(): Promise<string | null>
  denyDevices(): void
  allowDevices(): void
  holdJoin(): void
  releaseJoin(): void
  simulateReconnecting(): string | null
  simulateDegradedNetwork(): string | null
  simulateRejected(): string | null
  simulateRemoteEnd(): string | null
}
