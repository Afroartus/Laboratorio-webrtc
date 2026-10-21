import type { JoinCredentials, MediaConnectOptions, MediaEvent } from '../../domain/types.ts'

export interface MediaSession {
  connect(credentials: JoinCredentials, options: MediaConnectOptions): Promise<void>
  /** Nueva autorización. No vuelve a publicar la pantalla. */
  reconnect(credentials: JoinCredentials): Promise<void>
  setMicrophoneEnabled(enabled: boolean): Promise<void>
  /** Con `false`, detiene la captura. No basta con ocultar el video. */
  setCameraEnabled(enabled: boolean): Promise<void>
  /**
   * Debe invocar la captura de pantalla en la misma vuelta, antes de cualquier espera.
   * Rechaza con `screen-share-cancelled` si el usuario cierra el selector.
   */
  startScreenShare(): Promise<void>
  stopScreenShare(): Promise<void>
  enableAudioPlayback(): Promise<void>
  disconnect(): Promise<void>
  subscribe(listener: (event: MediaEvent) => void): () => void
}
