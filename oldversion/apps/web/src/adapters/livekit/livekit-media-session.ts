import {
  ConnectionQuality,
  DisconnectReason,
  LogLevel,
  Room,
  RoomEvent,
  Track,
  setLogLevel,
  type Participant,
  type RemoteParticipant,
  type RemoteTrack,
  type RemoteTrackPublication,
  type LocalTrackPublication,
} from 'livekit-client'
import { AppError, ScreenShareCancelled } from '../../domain/errors.ts'
import type {
  Diagnostics,
  JoinCredentials,
  MediaConnectOptions,
  MediaEvent,
  ParticipantState,
  Permissions,
  TrackSource,
  TrackState,
} from '../../domain/types.ts'
import type { MediaSession } from '../../application/ports/media-session.ts'
import { classifyDisconnect, emptyDiagnostics, qualityFromConnection, summarizeRtcReport, type RtcStat } from './stats.ts'

setLogLevel(LogLevel.warn)

interface StoredTrack {
  track: Track
  local: boolean
  kind: 'audio' | 'video'
}

export interface AttachableTrack {
  attach(element: HTMLMediaElement): void
  detach(element: HTMLMediaElement): void
  local: boolean
  kind: 'audio' | 'video'
}

type Listener = (event: MediaEvent) => void

export class LiveKitMediaSession implements MediaSession {
  private readonly room = new Room({
    adaptiveStream: true,
    dynacast: true,
    disconnectOnPageLeave: true,
    stopLocalTrackOnUnpublish: true,
  })
  private readonly listeners = new Set<Listener>()
  private readonly bound = new Map<string, StoredTrack>()
  private permissions: Permissions | null = null
  private forceRelay = false
  private cameraId: string | null = null
  private microphoneId: string | null = null
  private cameraWanted = false
  private microphoneWanted = false
  private screenTrack: MediaStreamTrack | null = null
  private hasJoined = false
  private leaving = false
  private suppressDisconnect = false
  private connectedAt = 0
  private timeToRemoteMediaMs: number | null = null
  private quality: Diagnostics['quality'] = 'unknown'
  private statsTimer: ReturnType<typeof setInterval> | undefined

  constructor() {
    this.bindRoom()
  }

  lookup(trackId: string): AttachableTrack | null {
    const stored = this.bound.get(trackId)
    if (!stored) return null
    return {
      local: stored.local,
      kind: stored.kind,
      attach: (element) => {
        stored.track.attach(element)
      },
      detach: (element) => {
        stored.track.detach(element)
      },
    }
  }

  async connect(credentials: JoinCredentials, options: MediaConnectOptions): Promise<void> {
    this.permissions = credentials.permissions
    this.forceRelay = options.forceRelay
    this.cameraId = options.cameraId
    this.microphoneId = options.microphoneId
    this.timeToRemoteMediaMs = null
    this.connectedAt = performance.now()
    try {
      await this.room.connect(credentials.serverUrl, credentials.participantToken, {
        autoSubscribe: credentials.permissions.subscribe,
        rtcConfig: options.forceRelay ? { iceTransportPolicy: 'relay' } : undefined,
      })
    } catch (error) {
      await this.room.disconnect(true).catch(() => undefined)
      throw new AppError('UNEXPECTED', safeError(error, credentials.participantToken))
    }
    this.hasJoined = true
    this.publishExisting()
    if (options.microphone && credentials.permissions.publishMicrophone) {
      await this.setMicrophoneEnabled(true)
    }
    if (options.camera && credentials.permissions.publishCamera) {
      await this.setCameraEnabled(true)
    }
    if (!this.room.canPlaybackAudio) this.emit({ type: 'AudioPlaybackBlocked' })
    this.startStats()
  }

  async reconnect(credentials: JoinCredentials): Promise<void> {
    const camera = this.cameraWanted && credentials.permissions.publishCamera
    const microphone = this.microphoneWanted && credentials.permissions.publishMicrophone
    this.permissions = credentials.permissions
    this.suppressDisconnect = true
    try {
      await this.room.disconnect(true)
      await this.room.connect(credentials.serverUrl, credentials.participantToken, {
        autoSubscribe: credentials.permissions.subscribe,
        rtcConfig: this.forceRelay ? { iceTransportPolicy: 'relay' } : undefined,
      })
      this.hasJoined = true
      this.bound.clear()
      this.screenTrack = null
      this.publishExisting()
      if (microphone) await this.setMicrophoneEnabled(true)
      if (camera) await this.setCameraEnabled(true)
    } catch (error) {
      throw new AppError('UNEXPECTED', safeError(error, credentials.participantToken))
    } finally {
      this.suppressDisconnect = false
    }
  }

  async setMicrophoneEnabled(enabled: boolean): Promise<void> {
    if (enabled && !this.permissions?.publishMicrophone) return
    this.microphoneWanted = enabled
    await this.room.localParticipant.setMicrophoneEnabled(
      enabled,
      this.microphoneId ? { deviceId: this.microphoneId } : undefined,
    )
  }

  async setCameraEnabled(enabled: boolean): Promise<void> {
    if (!enabled) {
      this.cameraWanted = false
      await this.stopPublished(Track.Source.Camera)
      return
    }
    if (!this.permissions?.publishCamera) return
    this.cameraWanted = true
    await this.room.localParticipant.setCameraEnabled(
      true,
      this.cameraId ? { deviceId: this.cameraId } : undefined,
    )
  }

  startScreenShare(): Promise<void> {
    if (!this.permissions?.publishScreen) {
      return Promise.reject(new AppError('FORBIDDEN', 'Este rol no puede compartir pantalla.'))
    }
    const capture = navigator.mediaDevices.getDisplayMedia({ video: true, audio: false })
    return capture.then(
      async (stream) => {
        const [video] = stream.getVideoTracks()
        if (!video) {
          stream.getTracks().forEach((track) => track.stop())
          throw new AppError('device-missing', 'El navegador no entregó una pista de pantalla.')
        }
        video.addEventListener('ended', this.onScreenEnded)
        this.screenTrack = video
        await this.room.localParticipant.publishTrack(video, { source: Track.Source.ScreenShare })
      },
      (error: unknown) => {
        if (isDisplayCancelled(error)) throw new ScreenShareCancelled()
        throw error instanceof Error
          ? new AppError('UNEXPECTED', error.message)
          : new AppError('UNEXPECTED', 'No se pudo compartir la pantalla.')
      },
    )
  }

  async stopScreenShare(): Promise<void> {
    const track = this.screenTrack
    if (track) track.removeEventListener('ended', this.onScreenEnded)
    this.screenTrack = null
    if (track) {
      await this.room.localParticipant.unpublishTrack(track, true)
      return
    }
    await this.stopPublished(Track.Source.ScreenShare)
  }

  async enableAudioPlayback(): Promise<void> {
    await this.room.startAudio()
    this.emit({ type: 'AudioPlaybackStarted' })
  }

  async disconnect(): Promise<void> {
    this.leaving = true
    this.stopStats()
    const track = this.screenTrack
    if (track) track.removeEventListener('ended', this.onScreenEnded)
    this.screenTrack = null
    try {
      await this.room.disconnect(true)
    } finally {
      this.leaving = false
      this.hasJoined = false
      this.bound.clear()
      this.cameraWanted = false
      this.microphoneWanted = false
    }
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  private readonly onScreenEnded = (): void => {
    const track = this.screenTrack
    this.screenTrack = null
    if (track) void this.room.localParticipant.unpublishTrack(track, true)
    this.emit({ type: 'ScreenShareEnded' })
  }

  private bindRoom(): void {
    this.room.on(RoomEvent.Reconnecting, () => {
      this.emit({ type: 'ConnectionChanged', state: 'reconnecting' })
    })
    this.room.on(RoomEvent.Reconnected, () => {
      this.emit({ type: 'ConnectionChanged', state: 'connected' })
    })
    this.room.on(RoomEvent.Disconnected, (reason?: DisconnectReason) => {
      this.onDisconnected(reason)
    })
    this.room.on(RoomEvent.ParticipantConnected, (participant: RemoteParticipant) => {
      this.emitParticipant(participant)
    })
    this.room.on(RoomEvent.ParticipantDisconnected, (participant: RemoteParticipant) => {
      this.emit({ type: 'ParticipantLeft', participantId: participant.identity })
    })
    this.room.on(
      RoomEvent.TrackSubscribed,
      (track: RemoteTrack, _publication: RemoteTrackPublication, participant: RemoteParticipant) => {
        this.remember(track, participant)
      },
    )
    this.room.on(
      RoomEvent.TrackUnsubscribed,
      (track: RemoteTrack, _publication: RemoteTrackPublication, participant: RemoteParticipant) => {
        this.forget(track, participant)
      },
    )
    this.room.on(RoomEvent.LocalTrackPublished, (publication: LocalTrackPublication) => {
      if (publication.track) this.remember(publication.track, this.room.localParticipant)
    })
    this.room.on(RoomEvent.LocalTrackUnpublished, (publication: LocalTrackPublication) => {
      if (publication.track) this.forget(publication.track, this.room.localParticipant)
      else this.emit({ type: 'TrackRemoved', trackId: this.idFor(this.room.localParticipant.identity, publication.source) })
    })
    this.room.on(RoomEvent.TrackMuted, (publication) => {
      this.repaintPublication(publication.track, publication.isMuted)
    })
    this.room.on(RoomEvent.TrackUnmuted, (publication) => {
      this.repaintPublication(publication.track, false)
    })
    this.room.on(RoomEvent.ConnectionQualityChanged, (quality: ConnectionQuality, participant: Participant) => {
      if (!participant.isLocal) return
      this.quality = qualityFromConnection(quality)
      void this.publishStats()
    })
    this.room.on(RoomEvent.AudioPlaybackStatusChanged, (playing: boolean) => {
      this.emit(playing ? { type: 'AudioPlaybackStarted' } : { type: 'AudioPlaybackBlocked' })
    })
  }

  private onDisconnected(reason?: DisconnectReason): void {
    if (this.suppressDisconnect || this.leaving) return
    const kind = classifyDisconnect(reason)
    if (kind === 'duplicate-identity') {
      this.emit({ type: 'ConnectionChanged', state: 'disconnected', reason: 'duplicate-identity' })
      return
    }
    if (kind === 'ended') {
      this.emit({
        type: 'ConnectionChanged',
        state: 'disconnected',
        reason: 'ended',
        message: 'La llamada terminó.',
      })
      return
    }
    if (kind === 'reauthorize' && this.hasJoined) {
      this.emit({ type: 'ReconnectAuthorizationRequired' })
      return
    }
    if (kind === 'client') {
      this.emit({ type: 'ConnectionChanged', state: 'disconnected', reason: 'client' })
      return
    }
    this.emit({
      type: 'ConnectionChanged',
      state: 'disconnected',
      reason: 'failed',
      message: 'Se perdió la conexión con la llamada.',
    })
  }

  private publishExisting(): void {
    this.emitParticipant(this.room.localParticipant)
    for (const publication of this.room.localParticipant.trackPublications.values()) {
      if (publication.track) this.remember(publication.track, this.room.localParticipant)
    }
    for (const participant of this.room.remoteParticipants.values()) {
      this.emitParticipant(participant)
      for (const publication of participant.trackPublications.values()) {
        if (publication.track) this.remember(publication.track, participant)
      }
    }
  }

  private emitParticipant(participant: Participant): void {
    const state: ParticipantState = {
      id: participant.identity,
      displayName: participant.name?.trim() || participant.identity,
      local: participant.isLocal,
      microphoneEnabled: participant.isMicrophoneEnabled,
      cameraEnabled: participant.isCameraEnabled,
      screenShareEnabled: participant.isScreenShareEnabled,
    }
    this.emit({ type: 'ParticipantUpdated', participant: state })
  }

  private remember(track: Track, participant: Participant): void {
    const kind = kindOf(track.kind)
    if (!kind) return
    const id = track.sid ?? this.idFor(participant.identity, track.source)
    this.bound.set(id, { track, local: participant.isLocal, kind })
    if (!participant.isLocal && this.timeToRemoteMediaMs === null && this.connectedAt > 0) {
      this.timeToRemoteMediaMs = Math.round(performance.now() - this.connectedAt)
    }
    this.emit({ type: 'TrackStateChanged', track: this.toTrackState(id, track, participant) })
  }

  private forget(track: Track, participant: Participant): void {
    const id = track.sid ?? this.idFor(participant.identity, track.source)
    this.bound.delete(id)
    this.emit({ type: 'TrackRemoved', trackId: id })
  }

  private repaintPublication(track: Track | undefined, muted: boolean): void {
    if (!track) return
    for (const [id, stored] of this.bound) {
      if (stored.track !== track) continue
      const participant = this.participantFor(stored)
      this.emit({
        type: 'TrackStateChanged',
        track: {
          id,
          participantId: participant?.identity ?? '',
          kind: stored.kind,
          source: sourceOf(track.source),
          local: stored.local,
          muted,
        },
      })
    }
  }

  private participantFor(stored: StoredTrack): Participant | undefined {
    if (stored.local) return this.room.localParticipant
    for (const participant of this.room.remoteParticipants.values()) {
      for (const publication of participant.trackPublications.values()) {
        if (publication.track === stored.track) return participant
      }
    }
    return undefined
  }

  private toTrackState(id: string, track: Track, participant: Participant): TrackState {
    return {
      id,
      participantId: participant.identity,
      kind: kindOf(track.kind) ?? 'video',
      source: sourceOf(track.source),
      local: participant.isLocal,
      muted: track.isMuted,
    }
  }

  private idFor(identity: string, source: Track.Source): string {
    return `${identity}:${source}`
  }

  private async stopPublished(source: Track.Source): Promise<void> {
    const publication = this.room.localParticipant.getTrackPublication(source)
    const mediaTrack = publication?.track?.mediaStreamTrack
    if (!mediaTrack) return
    await this.room.localParticipant.unpublishTrack(mediaTrack, true)
  }

  private startStats(): void {
    this.stopStats()
    this.statsTimer = setInterval(() => {
      void this.publishStats()
    }, 5000)
  }

  private stopStats(): void {
    if (this.statsTimer !== undefined) clearInterval(this.statsTimer)
    this.statsTimer = undefined
  }

  private async publishStats(): Promise<void> {
    let summary = emptyDiagnostics({ quality: this.quality, timeToRemoteMediaMs: this.timeToRemoteMediaMs, relay: this.forceRelay })
    for (const stored of this.bound.values()) {
      const report = await stored.track.getRTCStatsReport()
      if (!report) continue
      const next = summarizeRtcReport({
        forEach(callback) {
          report.forEach((stat) => {
            callback(stat as unknown as RtcStat)
          })
        },
      })
      if (next.icePair) {
        summary = emptyDiagnostics({
          ...next,
          quality: this.quality,
          timeToRemoteMediaMs: this.timeToRemoteMediaMs,
        })
      }
    }
    this.emit({ type: 'Diagnostics', diagnostics: summary })
  }

  private emit(event: MediaEvent): void {
    for (const listener of this.listeners) listener(event)
  }
}

function sourceOf(source: Track.Source): TrackSource {
  switch (source) {
    case Track.Source.Microphone:
      return 'microphone'
    case Track.Source.ScreenShare:
      return 'screen'
    case Track.Source.ScreenShareAudio:
      return 'screen-audio'
    default:
      return 'camera'
  }
}

function kindOf(kind: Track.Kind): 'audio' | 'video' | null {
  if (kind === Track.Kind.Audio) return 'audio'
  if (kind === Track.Kind.Video) return 'video'
  return null
}

function isDisplayCancelled(error: unknown): boolean {
  return error instanceof DOMException && (error.name === 'NotAllowedError' || error.name === 'AbortError')
}

function safeError(error: unknown, token: string): string {
  const message = error instanceof Error ? error.message : 'No se pudo conectar con el servidor de medios.'
  if (token && message.includes(token)) return 'No se pudo conectar con el servidor de medios.'
  return message
}
