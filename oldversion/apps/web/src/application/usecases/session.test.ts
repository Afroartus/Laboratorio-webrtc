/// <reference types="node" />
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { AppError, ScreenShareCancelled } from '../../domain/errors.ts'
import type {
  CallDetails,
  DeviceInfo,
  DeviceSelection,
  JoinCredentials,
  MediaConnectOptions,
  MediaEvent,
  Permissions,
  PreviewResult,
  User,
} from '../../domain/types.ts'
import type { AuthGateway } from '../ports/auth-gateway.ts'
import type { CallGateway } from '../ports/call-gateway.ts'
import type { DeviceAccess } from '../ports/device-access.ts'
import type { MediaSession } from '../ports/media-session.ts'
import { LabSession } from './session.ts'

const token = 'jwt-de-prueba-no-persistir'
const user: User = { id: 'juan', username: 'juan', displayName: 'Juan' }
const permissions: Permissions = {
  publishCamera: true,
  publishMicrophone: true,
  publishScreen: true,
  subscribe: true,
}

function credentials(callId = 'call-1'): JoinCredentials {
  return {
    callId,
    roomName: `call_${callId}`,
    serverUrl: 'wss://rtc.webrtc.test:8443',
    participantToken: token,
    expiresAt: '2026-10-21T20:00:00Z',
    permissions,
  }
}

function callDetails(overrides: Partial<CallDetails> = {}): CallDetails {
  return {
    callId: 'call-1',
    status: 'CREATED',
    role: 'creator',
    permissions,
    joinDeadline: '2026-10-21T18:10:00Z',
    ...overrides,
  }
}

class FakeAuth implements AuthGateway {
  loginError: AppError | null = null
  async login(): Promise<User> {
    if (this.loginError) throw this.loginError
    return user
  }
  async logout(): Promise<void> {}
  async currentSession(): Promise<User | null> {
    return null
  }
  async listUsers(): Promise<User[]> {
    return [user, { id: 'maria', username: 'maria', displayName: 'María' }]
  }
}

class FakeCalls implements CallGateway {
  tokenRequests = 0
  endRequests = 0
  details = callDetails()
  tokenError: AppError | null = null
  async createCall(): Promise<{ callId: string; joinUrl: string; status: 'CREATED' }> {
    return { callId: 'call-1', joinUrl: 'https://webrtc.test:8443/calls/call-1', status: 'CREATED' }
  }
  async getCall(): Promise<CallDetails> {
    return this.details
  }
  async endCall(): Promise<{ status: 'ENDING' }> {
    this.endRequests += 1
    this.details = { ...this.details, status: 'ENDING' }
    return { status: 'ENDING' }
  }
  async issueJoinToken(): Promise<JoinCredentials> {
    this.tokenRequests += 1
    if (this.tokenError) throw this.tokenError
    return credentials(this.details.callId)
  }
}

class FakeMedia implements MediaSession {
  connects = 0
  reconnects = 0
  disconnects = 0
  screenStarts = 0
  cameraStops = 0
  screenShareResult: Promise<void> | null = null
  private readonly listeners = new Set<(event: MediaEvent) => void>()

  async connect(_credentials: JoinCredentials, _options: MediaConnectOptions): Promise<void> {
    this.connects += 1
    this.emit({ type: 'ConnectionChanged', state: 'connected' })
  }
  async reconnect(): Promise<void> {
    this.reconnects += 1
  }
  async setMicrophoneEnabled(): Promise<void> {}
  async setCameraEnabled(enabled: boolean): Promise<void> {
    if (!enabled) this.cameraStops += 1
  }
  startScreenShare(): Promise<void> {
    this.screenStarts += 1
    return this.screenShareResult ?? Promise.resolve()
  }
  async stopScreenShare(): Promise<void> {}
  async enableAudioPlayback(): Promise<void> {}
  async disconnect(): Promise<void> {
    this.disconnects += 1
  }
  subscribe(listener: (event: MediaEvent) => void): () => void {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }
  emit(event: MediaEvent): void {
    for (const listener of this.listeners) listener(event)
  }
  get listenerCount(): number {
    return this.listeners.size
  }
}

class FakeDevices implements DeviceAccess {
  previewStops = 0
  async listDevices(): Promise<DeviceInfo[]> {
    return [
      { id: 'cam', label: 'Cámara', kind: 'videoinput' },
      { id: 'mic', label: 'Micrófono', kind: 'audioinput' },
    ]
  }
  async startPreview(_selection: DeviceSelection): Promise<PreviewResult> {
    return { audio: true, video: true }
  }
  async stopPreview(): Promise<void> {
    this.previewStops += 1
  }
}

function harness() {
  const auth = new FakeAuth()
  const calls = new FakeCalls()
  const media = new FakeMedia()
  const devices = new FakeDevices()
  const session = new LabSession({
    auth,
    calls,
    media,
    devices,
    origin: 'https://webrtc.test:8443',
    secureContext: true,
    forceRelay: false,
  })
  return { session, auth, calls, media, devices }
}

async function settle(): Promise<void> {
  for (let i = 0; i < 5; i += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
}

async function signedIn() {
  const parts = harness()
  await parts.session.signIn('juan', 'secreta')
  return parts
}

async function inCall() {
  const parts = await signedIn()
  await parts.session.openCall('call-1')
  await parts.session.goToLobby()
  await parts.session.joinCall()
  return parts
}

describe('sesión de la aplicación', () => {
  it('el inicio de sesión fallido deja la fase sin autenticar', async () => {
    const { session, auth } = harness()
    auth.loginError = new AppError('UNAUTHORIZED', 'Credenciales inválidas')
    const ok = await session.signIn('juan', 'mala')
    assert.equal(ok, false)
    assert.equal(session.getSnapshot().phase, 'logged-out')
    assert.equal(session.getSnapshot().error?.code, 'UNAUTHORIZED')
  })

  it('cargar una llamada ajena, terminada o sin sesión explica el rechazo', async () => {
    const forbidden = await signedIn()
    forbidden.calls.getCall = async () => {
      throw new AppError('FORBIDDEN', 'No participas en esta llamada')
    }
    await forbidden.session.openCall('call-1')
    assert.equal(forbidden.session.getSnapshot().phase, 'rejected')

    const ended = await signedIn()
    ended.calls.getCall = async () => {
      throw new AppError('CALL_ENDED', 'La llamada terminó')
    }
    await ended.session.openCall('call-1')
    assert.equal(ended.session.getSnapshot().phase, 'ended')
    assert.equal(ended.session.getSnapshot().statusMessage, 'La llamada terminó')

    const missing = await signedIn()
    missing.calls.details = callDetails({ status: 'ENDED' })
    await missing.session.openCall('call-1')
    assert.equal(missing.session.getSnapshot().phase, 'ended')

    const anonymous = harness()
    anonymous.calls.getCall = async () => {
      throw new AppError('UNAUTHORIZED', 'Sesión inválida')
    }
    await anonymous.session.signIn('juan', 'secreta')
    anonymous.auth.currentSession = async () => null
    await anonymous.session.openCall('call-1')
    anonymous.calls.getCall = async () => {
      throw new AppError('UNAUTHORIZED', 'Sesión inválida')
    }
    await anonymous.session.openCall('call-9')
    assert.equal(anonymous.session.getSnapshot().phase, 'logged-out')
  })

  it('unirse pide autorización y no acepta una segunda unión mientras conecta', async () => {
    const { session, calls, media } = await signedIn()
    await session.openCall('call-1')
    let release: (value: JoinCredentials) => void = () => undefined
    calls.issueJoinToken = () => {
      calls.tokenRequests += 1
      return new Promise((resolve) => {
        release = resolve
      })
    }
    const first = session.joinCall()
    const second = session.joinCall()
    assert.equal(calls.tokenRequests, 1)
    assert.equal(session.getSnapshot().phase, 'connecting')
    release(credentials())
    await first
    await second
    assert.equal(media.connects, 1)
    assert.equal(session.getSnapshot().phase, 'in-call')
    assert.equal(JSON.stringify(session.getSnapshot()).includes(token), false)
  })

  it('compartir pantalla llama al puerto en la misma vuelta y cancelar no cierra la llamada', async () => {
    const { session, media } = await inCall()
    let called = false
    media.startScreenShare = () => {
      called = true
      media.screenStarts += 1
      return Promise.reject(new ScreenShareCancelled())
    }
    session.startScreenShare()
    assert.equal(called, true)
    await settle()
    assert.equal(session.getSnapshot().phase, 'in-call')
    assert.equal(session.getSnapshot().publication.screen, false)
    assert.match(session.getSnapshot().statusMessage ?? '', /sigue activa/)
  })

  it('una reconexión no vuelve a compartir la pantalla ni duplica pistas', async () => {
    const { session, calls, media } = await inCall()
    const tokensBefore = calls.tokenRequests
    media.screenShareResult = Promise.resolve()
    session.startScreenShare()
    await settle()
    assert.equal(media.screenStarts, 1)

    media.emit({
      type: 'TrackStateChanged',
      track: {
        id: 'remote-cam',
        participantId: 'maria',
        kind: 'video',
        source: 'camera',
        local: false,
        muted: false,
      },
    })
    media.emit({ type: 'ReconnectAuthorizationRequired' })
    await settle()
    media.emit({
      type: 'TrackStateChanged',
      track: {
        id: 'remote-cam',
        participantId: 'maria',
        kind: 'video',
        source: 'camera',
        local: false,
        muted: false,
      },
    })
    assert.equal(calls.tokenRequests, tokensBefore + 1)
    assert.equal(media.reconnects, 1)
    assert.equal(media.screenStarts, 1)
    assert.equal(session.getSnapshot().publication.screen, false)
    assert.equal(session.getSnapshot().tracks.filter((track) => track.id === 'remote-cam').length, 1)
    assert.equal(session.getSnapshot().phase, 'in-call')
  })

  it('un corte de red no pide otro token', async () => {
    const { session, calls, media } = await inCall()
    const tokensBefore = calls.tokenRequests
    media.emit({ type: 'ConnectionChanged', state: 'reconnecting' })
    assert.match(session.getSnapshot().networkMessage ?? '', /Reconectando/)
    media.emit({ type: 'ConnectionChanged', state: 'connected' })
    await settle()
    assert.equal(session.getSnapshot().networkMessage, null)
    assert.equal(calls.tokenRequests, tokensBefore)
    assert.equal(media.reconnects, 0)
    assert.equal(session.getSnapshot().phase, 'in-call')
  })

  it('una red degradada avisa y conserva la llamada', async () => {
    const { session, calls, media } = await inCall()
    const tokensBefore = calls.tokenRequests
    media.emit({
      type: 'Diagnostics',
      diagnostics: {
        icePair: 'host → host',
        relay: false,
        rttMs: 480,
        packetLoss: 0.12,
        bitrateBps: 8000,
        timeToRemoteMediaMs: 40,
        quality: 'poor',
      },
    })
    assert.match(session.getSnapshot().networkMessage ?? '', /degradada/)
    assert.equal(session.getSnapshot().phase, 'in-call')
    assert.equal(calls.tokenRequests, tokensBefore)
  })

  it('apagar la cámara detiene la captura', async () => {
    const { session, media } = await inCall()
    await session.setCameraEnabled(false)
    assert.equal(media.cameraStops, 1)
    assert.equal(session.getSnapshot().publication.camera, false)
  })

  it('salir desconecta y no finaliza la llamada para los demás', async () => {
    const { session, calls, media, devices } = await inCall()
    assert.equal(media.listenerCount, 1)
    await session.leaveCall()
    assert.equal(calls.endRequests, 0)
    assert.equal(media.disconnects, 1)
    assert.equal(devices.previewStops >= 1, true)
    assert.equal(media.listenerCount, 0)
    assert.equal(session.getSnapshot().phase, 'call-entry')
    media.emit({ type: 'ConnectionChanged', state: 'disconnected', reason: 'ended' })
    await settle()
    assert.equal(session.getSnapshot().phase, 'call-entry')
  })

  it('finalizar para todos solo lo hace quien creó la llamada', async () => {
    const participant = await signedIn()
    participant.calls.details = callDetails({ role: 'participant' })
    await participant.session.openCall('call-1')
    await participant.session.goToLobby()
    await participant.session.joinCall()
    await participant.session.endCall()
    assert.equal(participant.calls.endRequests, 0)
    assert.equal(participant.session.getSnapshot().phase, 'in-call')
    assert.equal(participant.session.getSnapshot().error?.code, 'FORBIDDEN')

    const creator = await inCall()
    await creator.session.endCall()
    assert.equal(creator.calls.endRequests, 1)
    assert.equal(creator.session.getSnapshot().phase, 'ended')
    assert.match(creator.session.getSnapshot().statusMessage ?? '', /para todos/)
  })
})
