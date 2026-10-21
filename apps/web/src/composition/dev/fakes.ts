import { AppError, ScreenShareCancelled } from '../../domain/errors.ts'
import type {
  CallDetails,
  CallStatus,
  CreatedCall,
  DeviceInfo,
  DeviceSelection,
  InviteeRole,
  JoinCredentials,
  MediaConnectOptions,
  MediaEvent,
  Permissions,
  PreviewResult,
  TrackState,
  User,
} from '../../domain/types.ts'
import type { AuthGateway } from '../../application/ports/auth-gateway.ts'
import type { CallGateway } from '../../application/ports/call-gateway.ts'
import type { DeviceAccess } from '../../application/ports/device-access.ts'
import type { MediaSession } from '../../application/ports/media-session.ts'

const users: User[] = [
  { id: 'juan', username: 'juan', displayName: 'Juan' },
  { id: 'maria', username: 'maria', displayName: 'María' },
  { id: 'carlos', username: 'carlos', displayName: 'Carlos' },
]

interface StoredCall {
  id: string
  status: CallStatus
  ownerId: string
  inviteeId: string
  inviteeRole: InviteeRole
}

const calls = new Map<string, StoredCall>()
let current: User | null = null
let sequence = 0

function permissionsFor(role: CallDetails['role']): Permissions {
  if (role === 'spectator') {
    return { publishCamera: false, publishMicrophone: false, publishScreen: false, subscribe: true }
  }
  return { publishCamera: true, publishMicrophone: true, publishScreen: true, subscribe: true }
}

function latestCall(userId: string): StoredCall | undefined {
  let found: StoredCall | undefined
  for (const call of calls.values()) {
    if (call.ownerId === userId || call.inviteeId === userId) found = call
  }
  return found
}

export function currentSimulatedCall(): {
  id: string
  ownerId: string
  inviteeId: string
  inviteeRole: InviteeRole
} | null {
  if (!current) return null
  const call = latestCall(current.id)
  if (!call) return null
  return { id: call.id, ownerId: call.ownerId, inviteeId: call.inviteeId, inviteeRole: call.inviteeRole }
}

export function userById(id: string): User | undefined {
  const user = users.find((item) => item.id === id)
  return user ? { ...user } : undefined
}

export function setInviteeRole(callId: string, role: InviteeRole): void {
  const call = calls.get(callId)
  if (call) call.inviteeRole = role
}

export function endSimulatedCall(callId: string): void {
  const call = calls.get(callId)
  if (call) call.status = 'ENDED'
}

function detailsFor(call: StoredCall, user: User): CallDetails {
  const role = call.ownerId === user.id ? 'creator' : call.inviteeRole
  return {
    callId: call.id,
    status: call.status,
    role,
    permissions: permissionsFor(role),
    joinDeadline: '2026-10-21T18:10:00Z',
  }
}

export class FakeAuth implements AuthGateway {
  async login(username: string, password: string): Promise<User> {
    const user = users.find((item) => item.username === username)
    if (!user || password === 'mala') throw new AppError('UNAUTHORIZED', 'Credenciales inválidas')
    current = user
    return user
  }

  async logout(): Promise<void> {
    current = null
  }

  async currentSession(): Promise<User | null> {
    return current
  }

  async listUsers(): Promise<User[]> {
    if (!current) throw new AppError('UNAUTHORIZED', 'La sesión no es válida.')
    return users.map((user) => ({ ...user }))
  }
}

export class FakeCalls implements CallGateway {
  private gate: Promise<void> | null = null
  private releaseGate: (() => void) | null = null

  holdNextJoin(): void {
    if (this.gate) return
    this.gate = new Promise((resolve) => {
      this.releaseGate = resolve
    })
  }

  releaseJoin(): void {
    this.releaseGate?.()
    this.releaseGate = null
    this.gate = null
  }

  async createCall(input: {
    inviteeUserId: string
    inviteeRole: InviteeRole
    idempotencyKey: string
  }): Promise<CreatedCall> {
    if (!current) throw new AppError('UNAUTHORIZED', 'La sesión no es válida.')
    void input.idempotencyKey
    if (!users.some((user) => user.id === input.inviteeUserId) || input.inviteeUserId === current.id) {
      throw new AppError('FORBIDDEN', 'Elige a otra persona de la lista.')
    }
    sequence += 1
    const id = `dev-${sequence}`
    calls.set(id, {
      id,
      status: 'CREATED',
      ownerId: current.id,
      inviteeId: input.inviteeUserId,
      inviteeRole: input.inviteeRole,
    })
    return { callId: id, joinUrl: `/calls/${id}`, status: 'CREATED' }
  }

  async getCall(callId: string): Promise<CallDetails> {
    const call = this.requireMember(callId)
    return detailsFor(call, current as User)
  }

  async endCall(callId: string): Promise<{ status: CallStatus }> {
    const call = this.requireMember(callId)
    if (call.ownerId !== current?.id) {
      throw new AppError('FORBIDDEN', 'Solo quien creó la llamada puede finalizarla para todos.')
    }
    call.status = 'ENDED'
    return { status: 'ENDING' }
  }

  async issueJoinToken(callId: string): Promise<JoinCredentials> {
    const gate = this.gate
    if (gate) await gate
    const call = this.requireMember(callId)
    if (call.status === 'ENDED' || call.status === 'CANCELLED' || call.status === 'ENDING') {
      throw new AppError('CALL_ENDED', 'La llamada terminó')
    }
    const details = detailsFor(call, current as User)
    if (call.status === 'CREATED') call.status = 'ACTIVE'
    return {
      callId,
      roomName: `call_${callId}`,
      serverUrl: 'wss://rtc.webrtc.test:8443',
      participantToken: 'dev-media-token',
      expiresAt: '2026-10-21T20:00:00Z',
      permissions: details.permissions,
    }
  }

  private requireMember(callId: string): StoredCall {
    if (!current) throw new AppError('UNAUTHORIZED', 'La sesión no es válida.')
    const call = calls.get(callId)
    if (!call || (call.ownerId !== current.id && call.inviteeId !== current.id)) {
      throw new AppError('FORBIDDEN', 'No participas en esta llamada')
    }
    return call
  }
}

export class FakeMedia implements MediaSession {
  connects = 0
  screenStarts = 0
  private remoteName = 'María'
  private readonly listeners = new Set<(event: MediaEvent) => void>()
  private readonly described = new Map<string, { label: string; color: string; local: boolean; kind: 'audio' | 'video' }>()

  hasSubscribers(): boolean {
    return this.listeners.size > 0
  }

  notify(event: MediaEvent): void {
    this.emit(event)
  }

  async connect(_credentials: JoinCredentials, options: MediaConnectOptions): Promise<void> {
    this.connects += 1
    const mine = current
    const call = mine ? latestCall(mine.id) : undefined
    const otherId = call && mine ? (call.ownerId === mine.id ? call.inviteeId : call.ownerId) : ''
    const other = users.find((user) => user.id === otherId)
    this.remoteName = other?.displayName ?? 'María'
    const remoteId = other?.id ?? 'remote'
    this.emit({ type: 'ConnectionChanged', state: 'connected' })
    this.emit({
      type: 'ParticipantUpdated',
      participant: {
        id: 'local',
        displayName: 'Tú',
        local: true,
        microphoneEnabled: options.microphone,
        cameraEnabled: options.camera,
        screenShareEnabled: false,
      },
    })
    this.emit({
      type: 'ParticipantUpdated',
      participant: {
        id: remoteId,
        displayName: this.remoteName,
        local: false,
        microphoneEnabled: true,
        cameraEnabled: true,
        screenShareEnabled: false,
      },
    })
    if (options.camera) this.addTrack('local-camera', 'Tu cámara', '#1e3a5f', true, 'video', 'camera', 'local')
    this.addTrack('remote-camera', this.remoteName, '#3d5a3a', false, 'video', 'camera', remoteId)
    this.addTrack('remote-audio', this.remoteName, '#333', false, 'audio', 'microphone', remoteId)
    this.emit({
      type: 'Diagnostics',
      diagnostics: {
        icePair: 'host → host',
        relay: options.forceRelay,
        rttMs: 18,
        packetLoss: 0,
        bitrateBps: 120000,
        timeToRemoteMediaMs: 40,
        quality: 'good',
      },
    })
    this.emit({ type: 'AudioPlaybackBlocked' })
  }

  async reconnect(): Promise<void> {}

  async setMicrophoneEnabled(enabled: boolean): Promise<void> {
    if (!enabled) this.emit({ type: 'TrackRemoved', trackId: 'local-mic' })
  }

  async setCameraEnabled(enabled: boolean): Promise<void> {
    if (!enabled) {
      this.described.delete('local-camera')
      this.emit({ type: 'TrackRemoved', trackId: 'local-camera' })
      return
    }
    this.addTrack('local-camera', 'Tu cámara', '#1e3a5f', true, 'video', 'camera', 'local')
  }

  startScreenShare(): Promise<void> {
    this.screenStarts += 1
    if (this.screenStarts === 1) return Promise.reject(new ScreenShareCancelled())
    this.addTrack('remote-screen', `Pantalla de ${this.remoteName}`, '#5a3a1e', false, 'video', 'screen', 'remote')
    return Promise.resolve()
  }

  async stopScreenShare(): Promise<void> {
    this.described.delete('remote-screen')
    this.emit({ type: 'TrackRemoved', trackId: 'remote-screen' })
  }

  async enableAudioPlayback(): Promise<void> {
    this.emit({ type: 'AudioPlaybackStarted' })
  }

  async disconnect(): Promise<void> {
    this.described.clear()
  }

  subscribe(listener: (event: MediaEvent) => void): () => void {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  describe(trackId: string): { label: string; color: string; local: boolean; kind: 'audio' | 'video' } | null {
    return this.described.get(trackId) ?? null
  }

  private addTrack(
    id: string,
    label: string,
    color: string,
    local: boolean,
    kind: 'audio' | 'video',
    source: TrackState['source'],
    participantId: string,
  ): void {
    this.described.set(id, { label, color, local, kind })
    this.emit({
      type: 'TrackStateChanged',
      track: { id, participantId, kind, source, local, muted: false },
    })
  }

  private emit(event: MediaEvent): void {
    for (const listener of this.listeners) listener(event)
  }
}

export class FakeDevices implements DeviceAccess {
  private preview: HTMLVideoElement | null = null
  private deny: boolean

  constructor(deny: boolean) {
    this.deny = deny
  }

  setDenied(denied: boolean): void {
    this.deny = denied
  }

  async listDevices(): Promise<DeviceInfo[]> {
    return [
      { id: 'cam', label: 'Cámara simulada', kind: 'videoinput' },
      { id: 'mic', label: 'Micrófono simulado', kind: 'audioinput' },
    ]
  }

  async startPreview(selection: DeviceSelection): Promise<PreviewResult> {
    if (this.deny) {
      throw new AppError(
        'permission-denied',
        'El navegador bloqueó la cámara o el micrófono. Puedes unirte solo para escuchar.',
      )
    }
    if (selection.video || selection.audio) this.paint()
    return { audio: selection.audio, video: selection.video }
  }

  async stopPreview(): Promise<void> {
    if (this.preview) this.preview.srcObject = null
  }

  attach(element: HTMLVideoElement): void {
    this.preview = element
    element.muted = true
    this.paint()
  }

  detach(element: HTMLVideoElement): void {
    if (this.preview !== element) return
    element.srcObject = null
    this.preview = null
  }

  private paint(): void {
    if (!this.preview) return
    paint(this.preview, 'Vista previa local', '#1e3a5f')
  }
}

export class FakeTrackBinder {
  private readonly media: FakeMedia

  constructor(media: FakeMedia) {
    this.media = media
  }

  attach(trackId: string, element: HTMLMediaElement): void {
    const info = this.media.describe(trackId)
    if (!info || (info.local && info.kind === 'audio')) return
    if (element instanceof HTMLVideoElement) paint(element, info.label, info.color)
  }

  detach(_trackId: string, element: HTMLMediaElement): void {
    if (element instanceof HTMLVideoElement && element.srcObject instanceof MediaStream) {
      element.srcObject.getTracks().forEach((track) => track.stop())
      element.srcObject = null
    }
  }
}

function paint(element: HTMLVideoElement, label: string, color: string): void {
  const canvas = document.createElement('canvas')
  canvas.width = 640
  canvas.height = 360
  const context = canvas.getContext('2d')
  if (!context) return
  context.fillStyle = color
  context.fillRect(0, 0, 640, 360)
  context.fillStyle = '#ffffff'
  context.font = '32px system-ui, sans-serif'
  context.fillText(label, 40, 190)
  const stream = canvas.captureStream()
  element.srcObject = stream
  element.muted = true
  void element.play().catch(() => undefined)
}
