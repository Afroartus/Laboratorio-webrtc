import type { Diagnostics, NetworkQuality } from '../../domain/types.ts'

export interface RtcStat {
  type: string
  id?: string
  nominated?: boolean
  state?: string
  selected?: boolean
  currentRoundTripTime?: number
  packetsLost?: number
  packetsReceived?: number
  bytesReceived?: number
  localCandidateId?: string
  remoteCandidateId?: string
  candidateType?: string
}

export interface StatsSummary {
  icePair: string | null
  relay: boolean
  rttMs: number | null
  packetLoss: number | null
  bitrateBps: number | null
}

export function summarizeRtcReport(report: { forEach(callback: (stat: RtcStat) => void): void }): StatsSummary {
  const stats: RtcStat[] = []
  report.forEach((stat) => {
    stats.push(stat)
  })
  const pair = stats.find(
    (stat) => stat.type === 'candidate-pair' && (stat.nominated === true || stat.selected === true || stat.state === 'succeeded'),
  )
  const local = stats.find((stat) => stat.id && stat.id === pair?.localCandidateId)
  const remote = stats.find((stat) => stat.id && stat.id === pair?.remoteCandidateId)
  const received = stats.filter((stat) => stat.type === 'inbound-rtp')
  const packetsLost = received.reduce((sum, stat) => sum + (stat.packetsLost ?? 0), 0)
  const packetsReceived = received.reduce((sum, stat) => sum + (stat.packetsReceived ?? 0), 0)
  const bytesReceived = received.reduce((sum, stat) => sum + (stat.bytesReceived ?? 0), 0)
  const totalPackets = packetsLost + packetsReceived
  return {
    icePair: pair ? `${local?.candidateType ?? 'local'} → ${remote?.candidateType ?? 'remoto'}` : null,
    relay: local?.candidateType === 'relay' || remote?.candidateType === 'relay',
    rttMs: typeof pair?.currentRoundTripTime === 'number' ? Math.round(pair.currentRoundTripTime * 1000) : null,
    packetLoss: totalPackets > 0 ? packetsLost / totalPackets : null,
    bitrateBps: bytesReceived > 0 ? bytesReceived : null,
  }
}

export function qualityFromConnection(value: string): NetworkQuality {
  if (value === 'excellent' || value === 'good') return 'good'
  if (value === 'poor') return 'poor'
  if (value === 'lost') return 'lost'
  return 'unknown'
}

export function emptyDiagnostics(partial: Partial<Diagnostics> = {}): Diagnostics {
  return {
    icePair: null,
    relay: false,
    rttMs: null,
    packetLoss: null,
    bitrateBps: null,
    timeToRemoteMediaMs: null,
    quality: 'unknown',
    ...partial,
  }
}

/** Valores de livekit.DisconnectReason. El núcleo no importa el SDK. */
export type DisconnectClass = 'client' | 'ended' | 'duplicate-identity' | 'reauthorize' | 'failed'

export function classifyDisconnect(reason: number | undefined): DisconnectClass {
  switch (reason) {
    case 1:
      return 'client'
    case 2:
      return 'duplicate-identity'
    case 3:
    case 4:
    case 5:
    case 10:
      return 'ended'
    case 6:
    case 7:
    case 8:
    case 9:
      return 'reauthorize'
    default:
      return 'failed'
  }
}
