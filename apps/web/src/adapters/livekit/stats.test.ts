/// <reference types="node" />
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { classifyDisconnect, summarizeRtcReport } from './stats.ts'

describe('traducción de estadísticas y desconexión', () => {
  it('resume el par ICE seleccionado, el relé y la pérdida', () => {
    const summary = summarizeRtcReport({
      forEach(callback) {
        callback({ type: 'local-candidate', id: 'local', candidateType: 'relay' })
        callback({ type: 'remote-candidate', id: 'remote', candidateType: 'host' })
        callback({
          type: 'candidate-pair',
          nominated: true,
          state: 'succeeded',
          currentRoundTripTime: 0.042,
          localCandidateId: 'local',
          remoteCandidateId: 'remote',
        })
        callback({ type: 'inbound-rtp', packetsLost: 2, packetsReceived: 8, bytesReceived: 1000 })
      },
    })
    assert.equal(summary.icePair, 'relay → host')
    assert.equal(summary.relay, true)
    assert.equal(summary.rttMs, 42)
    assert.equal(summary.packetLoss, 0.2)
  })

  it('clasifica identidad duplicada, cierre de sala y necesidad de otro token', () => {
    assert.equal(classifyDisconnect(2), 'duplicate-identity')
    assert.equal(classifyDisconnect(5), 'ended')
    assert.equal(classifyDisconnect(9), 'reauthorize')
    assert.equal(classifyDisconnect(1), 'client')
  })
})
