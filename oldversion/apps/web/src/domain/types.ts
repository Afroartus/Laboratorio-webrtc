export type CallStatus = 'CREATED' | 'ACTIVE' | 'ENDING' | 'ENDED' | 'CANCELLED'

export type Role = 'creator' | 'participant' | 'spectator'

export type InviteeRole = 'participant' | 'spectator'

export type ConnectionState = 'idle' | 'connecting' | 'connected' | 'reconnecting' | 'disconnected'

export type TrackKind = 'audio' | 'video'

export type TrackSource = 'camera' | 'microphone' | 'screen' | 'screen-audio'

export type DisconnectReason = 'client' | 'ended' | 'duplicate-identity' | 'failed' | 'rejected'

export type NetworkQuality = 'unknown' | 'good' | 'poor' | 'lost'

export interface User {
  id: string
  username: string
  displayName: string
}

export interface Permissions {
  publishCamera: boolean
  publishMicrophone: boolean
  publishScreen: boolean
  subscribe: boolean
}

export interface CallDetails {
  callId: string
  status: CallStatus
  role: Role
  permissions: Permissions
  joinDeadline: string | null
}

export interface CreatedCall {
  callId: string
  joinUrl: string
  status: CallStatus
}

/** Credencial de ingreso. Solo vive en memoria, de la puerta HTTP a la sesión de medios. */
export interface JoinCredentials {
  callId: string
  roomName: string
  serverUrl: string
  participantToken: string
  expiresAt: string
  permissions: Permissions
}

export interface DeviceInfo {
  id: string
  label: string
  kind: 'audioinput' | 'videoinput'
}

export interface DeviceSelection {
  cameraId: string | null
  microphoneId: string | null
  video: boolean
  audio: boolean
}

export interface PreviewResult {
  audio: boolean
  video: boolean
}

export interface ParticipantState {
  id: string
  displayName: string
  local: boolean
  microphoneEnabled: boolean
  cameraEnabled: boolean
  screenShareEnabled: boolean
}

export interface TrackState {
  id: string
  participantId: string
  kind: TrackKind
  source: TrackSource
  local: boolean
  muted: boolean
}

export interface Diagnostics {
  icePair: string | null
  relay: boolean
  rttMs: number | null
  packetLoss: number | null
  bitrateBps: number | null
  timeToRemoteMediaMs: number | null
  quality: NetworkQuality
}

export interface MediaConnectOptions {
  forceRelay: boolean
  microphone: boolean
  camera: boolean
  cameraId: string | null
  microphoneId: string | null
}

export type MediaEvent =
  | {
      type: 'ConnectionChanged'
      state: ConnectionState
      reason?: DisconnectReason
      message?: string
    }
  | { type: 'ParticipantUpdated'; participant: ParticipantState }
  | { type: 'ParticipantLeft'; participantId: string }
  | { type: 'TrackStateChanged'; track: TrackState }
  | { type: 'TrackRemoved'; trackId: string }
  | { type: 'ReconnectAuthorizationRequired' }
  | { type: 'AudioPlaybackBlocked' }
  | { type: 'AudioPlaybackStarted' }
  | { type: 'Diagnostics'; diagnostics: Diagnostics }
  | { type: 'ScreenShareEnded' }
