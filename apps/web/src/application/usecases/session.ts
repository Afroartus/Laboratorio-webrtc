import { AppError, isAppError, isScreenShareCancelled } from '../../domain/errors.ts'
import type {
  CallDetails,
  CallStatus,
  ConnectionState,
  DeviceInfo,
  Diagnostics,
  InviteeRole,
  MediaEvent,
  ParticipantState,
  Permissions,
  Role,
  TrackState,
  User,
} from '../../domain/types.ts'
import type { AuthGateway } from '../ports/auth-gateway.ts'
import type { CallGateway } from '../ports/call-gateway.ts'
import type { DeviceAccess } from '../ports/device-access.ts'
import type { MediaSession } from '../ports/media-session.ts'

export type Phase =
  | 'logged-out'
  | 'home'
  | 'call-entry'
  | 'lobby'
  | 'connecting'
  | 'in-call'
  | 'ended'
  | 'rejected'

export interface CallSnapshot {
  callId: string
  status: CallStatus
  role: Role
  permissions: Permissions
  joinDeadline: string | null
  joinUrl: string
}

export interface Snapshot {
  secureContext: boolean
  user: User | null
  users: User[]
  phase: Phase
  call: CallSnapshot | null
  connection: ConnectionState
  statusMessage: string | null
  networkMessage: string | null
  devices: DeviceInfo[]
  selectedCameraId: string | null
  selectedMicrophoneId: string | null
  previewReady: boolean
  wantCamera: boolean
  wantMicrophone: boolean
  deviceMessage: string | null
  publication: { microphone: boolean; camera: boolean; screen: boolean }
  participants: ParticipantState[]
  tracks: TrackState[]
  audioBlocked: boolean
  diagnostics: Diagnostics | null
  error: { code: string; message: string } | null
  busy: boolean
}

export interface LabSessionOptions {
  auth: AuthGateway
  calls: CallGateway
  media: MediaSession
  devices: DeviceAccess
  origin: string
  secureContext: boolean
  forceRelay: boolean
}

type Listener = (snapshot: Snapshot) => void

export class LabSession {
  private readonly auth: AuthGateway
  private readonly calls: CallGateway
  private readonly media: MediaSession
  private readonly devices: DeviceAccess
  private readonly origin: string
  private readonly secureContext: boolean
  private readonly forceRelay: boolean

  private user: User | null = null
  private users: User[] = []
  private phase: Phase = 'logged-out'
  private call: CallSnapshot | null = null
  private connection: ConnectionState = 'idle'
  private statusMessage: string | null = null
  private networkMessage: string | null = null
  private devicesList: DeviceInfo[] = []
  private selectedCameraId: string | null = null
  private selectedMicrophoneId: string | null = null
  private previewReady = false
  private previewVideo = false
  private previewAudio = false
  private wantCamera = true
  private wantMicrophone = true
  private deviceMessage: string | null = null
  private publication = { microphone: false, camera: false, screen: false }
  private participants = new Map<string, ParticipantState>()
  private tracks = new Map<string, TrackState>()
  private audioBlocked = false
  private diagnostics: Diagnostics | null = null
  private error: { code: string; message: string } | null = null
  private busy = false
  private releasing = false
  private refreshing = false
  private mediaUnsub: (() => void) | null = null
  private readonly listeners = new Set<Listener>()

  constructor(options: LabSessionOptions) {
    this.auth = options.auth
    this.calls = options.calls
    this.media = options.media
    this.devices = options.devices
    this.origin = options.origin.replace(/\/$/, '')
    this.secureContext = options.secureContext
    this.forceRelay = options.forceRelay
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  getSnapshot(): Snapshot {
    return {
      secureContext: this.secureContext,
      user: this.user ? { ...this.user } : null,
      users: this.users.map((user) => ({ ...user })),
      phase: this.phase,
      call: this.call
        ? {
            ...this.call,
            permissions: { ...this.call.permissions },
          }
        : null,
      connection: this.connection,
      statusMessage: this.statusMessage,
      networkMessage: this.networkMessage,
      devices: this.devicesList.map((device) => ({ ...device })),
      selectedCameraId: this.selectedCameraId,
      selectedMicrophoneId: this.selectedMicrophoneId,
      previewReady: this.previewReady,
      wantCamera: this.wantCamera,
      wantMicrophone: this.wantMicrophone,
      deviceMessage: this.deviceMessage,
      publication: { ...this.publication },
      participants: [...this.participants.values()].map((participant) => ({ ...participant })),
      tracks: [...this.tracks.values()].map((track) => ({ ...track })),
      audioBlocked: this.audioBlocked,
      diagnostics: this.diagnostics ? { ...this.diagnostics } : null,
      error: this.error ? { ...this.error } : null,
      busy: this.busy,
    }
  }

  async loadSession(): Promise<void> {
    try {
      const user = await this.auth.currentSession()
      if (!user) {
        this.user = null
        this.phase = 'logged-out'
        this.emit()
        return
      }
      this.user = user
      this.users = await this.auth.listUsers()
      if (this.phase === 'logged-out') this.phase = 'home'
      this.emit()
    } catch (error) {
      this.user = null
      this.phase = 'logged-out'
      this.setError(error)
    }
  }

  async signIn(username: string, password: string): Promise<boolean> {
    if (!username.trim() || !password) {
      this.error = { code: 'UNAUTHORIZED', message: 'Escribe usuario y contraseña.' }
      this.emit()
      return false
    }
    this.busy = true
    this.error = null
    this.emit()
    try {
      this.user = await this.auth.login(username.trim(), password)
      this.users = await this.auth.listUsers()
      this.phase = 'home'
      this.busy = false
      this.emit()
      return true
    } catch (error) {
      this.user = null
      this.phase = 'logged-out'
      this.setError(error)
      return false
    }
  }

  async signOut(): Promise<void> {
    if (this.phase === 'in-call' || this.phase === 'connecting' || this.phase === 'lobby') {
      await this.releaseLocalMedia()
    }
    try {
      await this.auth.logout()
    } catch (error) {
      this.setError(error)
      return
    }
    this.user = null
    this.users = []
    this.call = null
    this.statusMessage = null
    this.networkMessage = null
    this.error = null
    this.phase = 'logged-out'
    this.emit()
  }

  async createCall(inviteeUserId: string, inviteeRole: InviteeRole): Promise<string | null> {
    if (!this.user) return null
    if (!inviteeUserId || inviteeUserId === this.user.id) {
      this.error = { code: 'FORBIDDEN', message: 'Elige a otra persona de la lista.' }
      this.emit()
      return null
    }
    this.busy = true
    this.error = null
    this.emit()
    try {
      const created = await this.calls.createCall({
        inviteeUserId,
        inviteeRole,
        idempotencyKey: crypto.randomUUID(),
      })
      this.busy = false
      this.emit()
      return created.callId
    } catch (error) {
      this.setError(error)
      return null
    }
  }

  async openCall(callId: string): Promise<void> {
    if (!this.user) return
    if (
      this.call?.callId === callId &&
      this.phase !== 'logged-out' &&
      this.phase !== 'home' &&
      this.phase !== 'rejected'
    ) {
      await this.refreshCall()
      return
    }
    if (this.phase === 'in-call' || this.phase === 'connecting' || this.phase === 'lobby') {
      await this.releaseLocalMedia()
    }
    this.busy = true
    this.error = null
    this.statusMessage = null
    this.emit()
    try {
      const details = await this.calls.getCall(callId)
      this.applyCall(details)
      if (isTerminal(details.status)) {
        this.phase = 'ended'
        this.statusMessage = messageForTerminal(details.status)
      } else {
        this.phase = 'call-entry'
      }
      this.busy = false
      this.emit()
    } catch (error) {
      this.failCallLoad(error)
    }
  }

  async refreshCall(): Promise<void> {
    if (!this.user || !this.call) return
    if (this.phase === 'logged-out') return
    try {
      const details = await this.calls.getCall(this.call.callId)
      this.applyCall(details)
      if (isTerminal(details.status)) {
        await this.releaseLocalMedia()
        this.phase = 'ended'
        this.statusMessage = messageForTerminal(details.status)
      }
      this.emit()
    } catch (error) {
      if (
        isAppError(error) &&
        (error.code === 'CALL_ENDED' || error.code === 'FORBIDDEN' || error.code === 'UNAUTHORIZED')
      ) {
        this.failCallLoad(error)
      }
    }
  }

  async goToLobby(): Promise<void> {
    if (!this.call || isTerminal(this.call.status)) return
    const spectator = this.call.role === 'spectator'
    this.wantCamera = !spectator && this.call.permissions.publishCamera
    this.wantMicrophone = !spectator && this.call.permissions.publishMicrophone
    this.phase = 'lobby'
    this.statusMessage = null
    this.error = null
    this.emit()
    await this.prepareDevices()
  }

  async selectCamera(id: string): Promise<void> {
    this.selectedCameraId = id
    if (this.phase === 'lobby') await this.prepareDevices()
    else this.emit()
  }

  async selectMicrophone(id: string): Promise<void> {
    this.selectedMicrophoneId = id
    if (this.phase === 'lobby') await this.prepareDevices()
    else this.emit()
  }

  async joinCall(mode: 'media' | 'listen' = 'media'): Promise<void> {
    if (this.phase === 'connecting' || this.phase === 'in-call') return
    if (!this.call) return
    if (isTerminal(this.call.status)) {
      this.phase = 'ended'
      this.statusMessage = messageForTerminal(this.call.status)
      this.emit()
      return
    }
    this.phase = 'connecting'
    this.error = null
    this.statusMessage = null
    this.connection = 'connecting'
    this.emit()
    try {
      const credentials = await this.calls.issueJoinToken(this.call.callId)
      const listen = mode === 'listen' || this.call.role === 'spectator'
      const camera =
        !listen && this.wantCamera && this.call.permissions.publishCamera && this.previewVideo
      const microphone =
        !listen &&
        this.wantMicrophone &&
        this.call.permissions.publishMicrophone &&
        this.previewAudio
      await this.devices.stopPreview()
      this.previewReady = false
      this.ensureMediaSubscription()
      await this.media.connect(credentials, {
        forceRelay: this.forceRelay,
        camera,
        microphone,
        cameraId: this.selectedCameraId,
        microphoneId: this.selectedMicrophoneId,
      })
      this.publication = { microphone, camera, screen: false }
      this.phase = 'in-call'
      this.connection = 'connected'
      this.busy = false
      this.emit()
    } catch (error) {
      this.clearMediaSubscription()
      try {
        await this.media.disconnect()
      } catch {
        // La conexión no llegó a establecerse.
      }
      this.connection = 'disconnected'
      if (isAppError(error) && error.code === 'FORBIDDEN') this.phase = 'rejected'
      else if (isAppError(error) && (error.code === 'CALL_ENDED' || error.code === 'NOT_FOUND')) {
        this.phase = 'ended'
      } else this.phase = 'lobby'
      this.setError(error)
    }
  }

  async setMicrophoneEnabled(enabled: boolean): Promise<void> {
    if (this.phase === 'lobby') {
      this.wantMicrophone = enabled
      await this.prepareDevices()
      return
    }
    if (this.phase !== 'in-call') return
    if (enabled && !this.call?.permissions.publishMicrophone) {
      this.setError(new AppError('FORBIDDEN', 'Este rol no puede publicar el micrófono.'))
      return
    }
    await this.media.setMicrophoneEnabled(enabled)
    this.publication.microphone = enabled
    this.emit()
  }

  async setCameraEnabled(enabled: boolean): Promise<void> {
    if (this.phase === 'lobby') {
      this.wantCamera = enabled
      await this.prepareDevices()
      return
    }
    if (this.phase !== 'in-call') return
    if (enabled && !this.call?.permissions.publishCamera) {
      this.setError(new AppError('FORBIDDEN', 'Este rol no puede publicar la cámara.'))
      return
    }
    await this.media.setCameraEnabled(enabled)
    this.publication.camera = enabled
    this.emit()
  }

  startScreenShare(): void {
    if (this.phase !== 'in-call') return
    if (!this.call?.permissions.publishScreen) {
      this.setError(new AppError('FORBIDDEN', 'Este rol no puede compartir pantalla.'))
      return
    }
    let pending: Promise<void>
    try {
      pending = this.media.startScreenShare()
    } catch (error) {
      this.onScreenShareFailed(error)
      return
    }
    void pending.then(
      () => {
        this.publication.screen = true
        this.error = null
        this.statusMessage = null
        this.emit()
      },
      (error: unknown) => {
        this.onScreenShareFailed(error)
      },
    )
  }

  async stopScreenShare(): Promise<void> {
    if (this.phase !== 'in-call') return
    await this.media.stopScreenShare()
    this.publication.screen = false
    this.emit()
  }

  async leaveCall(): Promise<void> {
    if (this.phase !== 'in-call' && this.phase !== 'connecting' && this.phase !== 'lobby') return
    await this.releaseLocalMedia()
    this.networkMessage = null
    if (this.call && !isTerminal(this.call.status)) {
      this.phase = 'call-entry'
      this.statusMessage = 'Saliste de la llamada. Los demás pueden seguir.'
    } else {
      this.phase = 'ended'
    }
    this.emit()
  }

  async endCall(): Promise<void> {
    if (!this.call || this.call.role !== 'creator') {
      this.setError(
        new AppError('FORBIDDEN', 'Solo quien creó la llamada puede finalizarla para todos.'),
      )
      return
    }
    this.busy = true
    this.emit()
    try {
      await this.calls.endCall(this.call.callId)
      await this.releaseLocalMedia()
      try {
        const details = await this.calls.getCall(this.call.callId)
        this.applyCall(details)
      } catch (error) {
        if (!isAppError(error) || error.code !== 'CALL_ENDED') throw error
      }
      this.phase = 'ended'
      this.statusMessage = 'Finalizaste la llamada para todos.'
      this.busy = false
      this.error = null
      this.emit()
    } catch (error) {
      this.setError(error)
    }
  }

  async enableAudioPlayback(): Promise<void> {
    try {
      await this.media.enableAudioPlayback()
      this.audioBlocked = false
      this.emit()
    } catch (error) {
      this.setError(error)
    }
  }

  async goHome(): Promise<void> {
    if (this.phase === 'in-call' || this.phase === 'connecting' || this.phase === 'lobby') {
      await this.releaseLocalMedia()
    }
    this.call = null
    this.error = null
    this.statusMessage = null
    this.networkMessage = null
    this.deviceMessage = null
    this.phase = this.user ? 'home' : 'logged-out'
    this.emit()
  }

  private async prepareDevices(): Promise<void> {
    if (!this.call) return
    if (!this.secureContext) {
      this.deviceMessage =
        'Esta página no está en un contexto seguro. La cámara y el micrófono no están disponibles.'
      this.previewReady = false
      this.previewVideo = false
      this.previewAudio = false
      this.emit()
      return
    }
    const spectator = this.call.role === 'spectator'
    try {
      this.devicesList = await this.devices.listDevices()
      if (!this.selectedCameraId) {
        this.selectedCameraId = this.devicesList.find((device) => device.kind === 'videoinput')?.id ?? null
      }
      if (!this.selectedMicrophoneId) {
        this.selectedMicrophoneId =
          this.devicesList.find((device) => device.kind === 'audioinput')?.id ?? null
      }
      const video = this.wantCamera && this.call.permissions.publishCamera && !spectator
      const audio = this.wantMicrophone && this.call.permissions.publishMicrophone && !spectator
      if (!video && !audio) {
        await this.devices.stopPreview()
        this.previewReady = false
        this.previewVideo = false
        this.previewAudio = false
        this.deviceMessage = spectator
          ? 'Como espectador solo recibes medios. No se publicará cámara ni micrófono.'
          : null
        this.emit()
        return
      }
      const result = await this.devices.startPreview({
        cameraId: video ? this.selectedCameraId : null,
        microphoneId: audio ? this.selectedMicrophoneId : null,
        video,
        audio,
      })
      this.previewVideo = result.video
      this.previewAudio = result.audio
      this.previewReady = result.video || result.audio
      this.deviceMessage = null
      if (video && !result.video) {
        this.deviceMessage = 'No hay cámara disponible. Puedes continuar con el audio o solo escuchar.'
      }
      if (audio && !result.audio && video && result.video) {
        this.deviceMessage = 'No hay micrófono disponible. Puedes continuar con la cámara o solo escuchar.'
      }
    } catch (error) {
      this.previewReady = false
      this.previewVideo = false
      this.previewAudio = false
      if (isAppError(error) && (error.code === 'permission-denied' || error.code === 'device-missing')) {
        this.deviceMessage = error.message
      } else {
        this.setError(error)
        return
      }
    }
    this.emit()
  }

  private applyCall(details: CallDetails): void {
    this.call = {
      callId: details.callId,
      status: details.status,
      role: details.role,
      permissions: { ...details.permissions },
      joinDeadline: details.joinDeadline,
      joinUrl: `${this.origin}/calls/${details.callId}`,
    }
  }

  private ensureMediaSubscription(): void {
    if (this.mediaUnsub) return
    this.mediaUnsub = this.media.subscribe((event) => {
      this.onMedia(event)
    })
  }

  private clearMediaSubscription(): void {
    this.mediaUnsub?.()
    this.mediaUnsub = null
  }

  private onMedia(event: MediaEvent): void {
    if (this.releasing) return
    switch (event.type) {
      case 'ConnectionChanged':
        this.connection = event.state
        if (event.state === 'reconnecting') this.networkMessage = 'Reconectando…'
        if (event.state === 'connected' && event.reason !== 'duplicate-identity') {
          this.phase = 'in-call'
          if (this.networkMessage === 'Reconectando…') this.networkMessage = null
        }
        if (event.reason === 'duplicate-identity') {
          void this.onReplaced()
          return
        }
        if (
          event.state === 'disconnected' &&
          (event.reason === 'ended' || event.reason === 'rejected' || event.reason === 'failed')
        ) {
          void this.onRemoteClose(event.reason, event.message)
          return
        }
        this.emit()
        return
      case 'ReconnectAuthorizationRequired':
        void this.refreshAuthorization()
        return
      case 'ParticipantUpdated':
        this.participants.set(event.participant.id, { ...event.participant })
        this.emit()
        return
      case 'ParticipantLeft':
        this.participants.delete(event.participantId)
        for (const [id, track] of this.tracks) {
          if (track.participantId === event.participantId) this.tracks.delete(id)
        }
        this.emit()
        return
      case 'TrackStateChanged':
        this.tracks.set(event.track.id, { ...event.track })
        this.emit()
        return
      case 'TrackRemoved': {
        const existing = this.tracks.get(event.trackId)
        this.tracks.delete(event.trackId)
        if (existing?.local && existing.source === 'screen') this.publication.screen = false
        if (existing?.local && existing.source === 'camera') this.publication.camera = false
        if (existing?.local && existing.source === 'microphone') this.publication.microphone = false
        this.emit()
        return
      }
      case 'ScreenShareEnded':
        this.publication.screen = false
        this.statusMessage = 'Dejaste de compartir pantalla.'
        this.emit()
        return
      case 'AudioPlaybackBlocked':
        this.audioBlocked = true
        this.emit()
        return
      case 'AudioPlaybackStarted':
        this.audioBlocked = false
        this.emit()
        return
      case 'Diagnostics':
        this.diagnostics = { ...event.diagnostics }
        if (event.diagnostics.quality === 'poor' || event.diagnostics.quality === 'lost') {
          this.networkMessage = 'La red está degradada. Se reduce el video para conservar el audio.'
        } else if (this.connection === 'connected') {
          this.networkMessage = null
        }
        this.emit()
        return
      default:
        return
    }
  }

  private async refreshAuthorization(): Promise<void> {
    if (this.refreshing || !this.call || this.releasing) return
    this.refreshing = true
    this.networkMessage = 'Renovando la autorización de la llamada…'
    this.emit()
    try {
      const credentials = await this.calls.issueJoinToken(this.call.callId)
      await this.media.reconnect(credentials)
      this.publication.screen = false
      this.connection = 'connected'
      this.phase = 'in-call'
      this.networkMessage = null
      this.refreshing = false
      this.emit()
    } catch (error) {
      this.refreshing = false
      await this.releaseLocalMedia()
      if (isAppError(error) && (error.code === 'CALL_ENDED' || error.code === 'FORBIDDEN')) {
        this.failCallLoad(error)
        return
      }
      this.phase = 'call-entry'
      this.setError(error)
    }
  }

  private async onReplaced(): Promise<void> {
    await this.releaseLocalMedia()
    this.phase = 'call-entry'
    this.statusMessage = 'Otra pestaña con la misma identidad sustituyó esta conexión.'
    this.emit()
  }

  private async onRemoteClose(reason: 'ended' | 'rejected' | 'failed', message?: string): Promise<void> {
    const callId = this.call?.callId
    await this.releaseLocalMedia()
    if (callId) {
      try {
        const details = await this.calls.getCall(callId)
        this.applyCall(details)
      } catch {
        // Se explica con el motivo de la desconexión si el estado ya no está disponible.
      }
    }
    this.phase = reason === 'rejected' ? 'rejected' : 'ended'
    this.statusMessage =
      message ??
      (reason === 'failed' ? 'Se perdió la conexión con la llamada.' : 'La llamada terminó.')
    this.emit()
  }

  private onScreenShareFailed(error: unknown): void {
    if (isScreenShareCancelled(error)) {
      this.publication.screen = false
      this.statusMessage = 'Se canceló la selección de pantalla. La llamada sigue activa.'
      this.emit()
      return
    }
    this.setError(error)
  }

  private async releaseLocalMedia(): Promise<void> {
    if (this.releasing) return
    this.releasing = true
    this.clearMediaSubscription()
    try {
      await this.media.disconnect()
    } catch {
      // Ya estaba desconectado.
    }
    try {
      await this.devices.stopPreview()
    } catch {
      // La vista previa ya estaba detenida.
    }
    this.releasing = false
    this.tracks.clear()
    this.participants.clear()
    this.publication = { microphone: false, camera: false, screen: false }
    this.previewReady = false
    this.previewVideo = false
    this.previewAudio = false
    this.connection = 'disconnected'
    this.audioBlocked = false
    this.diagnostics = null
  }

  private failCallLoad(error: unknown): void {
    if (!isAppError(error)) {
      this.setError(error)
      return
    }
    this.error = { code: error.code, message: error.message }
    if (error.code === 'UNAUTHORIZED') this.phase = 'logged-out'
    else if (error.code === 'FORBIDDEN' || error.code === 'NOT_FOUND') this.phase = 'rejected'
    else if (error.code === 'CALL_ENDED') {
      this.phase = 'ended'
      this.statusMessage = error.message
    }
    this.busy = false
    this.emit()
  }

  private setError(error: unknown): void {
    if (isAppError(error)) this.error = { code: error.code, message: error.message }
    else if (error instanceof Error) this.error = { code: 'UNEXPECTED', message: error.message }
    else this.error = { code: 'UNEXPECTED', message: 'Ocurrió un error inesperado.' }
    this.busy = false
    this.emit()
  }

  private emit(): void {
    const snapshot = this.getSnapshot()
    for (const listener of this.listeners) listener(snapshot)
  }
}

function isTerminal(status: CallStatus): boolean {
  return status === 'ENDED' || status === 'CANCELLED' || status === 'ENDING'
}

function messageForTerminal(status: CallStatus): string {
  if (status === 'CANCELLED') return 'La llamada se canceló antes de empezar.'
  if (status === 'ENDING') return 'La llamada se está finalizando.'
  return 'La llamada terminó.'
}
