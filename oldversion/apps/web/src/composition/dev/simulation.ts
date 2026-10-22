import type { LabSession } from '../../application/usecases/session.ts'
import type { Diagnostics } from '../../domain/types.ts'
import type { CallSimulation } from '../../presentation/simulation.ts'
import { navigate } from '../../presentation/router.ts'
import {
  currentSimulatedCall,
  endSimulatedCall,
  setInviteeRole,
  userById,
  type FakeCalls,
  type FakeDevices,
  type FakeMedia,
} from './fakes.ts'

const restored: Diagnostics = {
  icePair: 'host → host',
  relay: false,
  rttMs: 18,
  packetLoss: 0,
  bitrateBps: 120_000,
  timeToRemoteMediaMs: 40,
  quality: 'good',
}

const degraded: Diagnostics = {
  icePair: 'host → host',
  relay: false,
  rttMs: 480,
  packetLoss: 0.12,
  bitrateBps: 8_000,
  timeToRemoteMediaMs: 40,
  quality: 'poor',
}

export function createSimulation(
  session: LabSession,
  media: FakeMedia,
  calls: FakeCalls,
  devices: FakeDevices,
): CallSimulation {
  async function switchTo(userId: string, callId: string): Promise<string | null> {
    const user = userById(userId)
    if (!user) return 'No se encontró a esa persona.'
    await session.signOut()
    const signedIn = await session.signIn(user.username, 'simulada')
    if (!signedIn) return 'No se pudo entrar con esa persona.'
    navigate(`/calls/${callId}`)
    return null
  }

  return {
    async enterAsInvitee() {
      const call = currentSimulatedCall()
      if (!call) return 'Crea una llamada antes de cambiar de persona.'
      return switchTo(call.inviteeId, call.id)
    },
    async enterAsSpectator() {
      const call = currentSimulatedCall()
      if (!call) return 'Crea una llamada antes de cambiar de persona.'
      setInviteeRole(call.id, 'spectator')
      return switchTo(call.inviteeId, call.id)
    },
    async returnAsCreator() {
      const call = currentSimulatedCall()
      if (!call) return 'No hay una llamada a la que volver.'
      return switchTo(call.ownerId, call.id)
    },
    denyDevices() {
      devices.setDenied(true)
    },
    allowDevices() {
      devices.setDenied(false)
    },
    holdJoin() {
      calls.holdNextJoin()
    },
    releaseJoin() {
      calls.releaseJoin()
    },
    simulateReconnecting() {
      if (!media.hasSubscribers()) return 'Únete a la llamada para ver la reconexión.'
      media.notify({ type: 'ConnectionChanged', state: 'reconnecting' })
      window.setTimeout(() => {
        if (!media.hasSubscribers()) return
        media.notify({ type: 'ConnectionChanged', state: 'connected' })
        media.notify({ type: 'Diagnostics', diagnostics: restored })
      }, 1200)
      return null
    },
    simulateDegradedNetwork() {
      if (!media.hasSubscribers()) return 'Únete a la llamada para ver la red degradada.'
      media.notify({ type: 'Diagnostics', diagnostics: degraded })
      return null
    },
    simulateRejected() {
      if (!session.getSnapshot().user) return 'Entra con un usuario antes de simular el rechazo.'
      if (media.hasSubscribers()) {
        media.notify({
          type: 'ConnectionChanged',
          state: 'disconnected',
          reason: 'rejected',
          message: 'No tienes acceso a esta llamada.',
        })
        return null
      }
      navigate('/calls/ajena')
      return null
    },
    simulateRemoteEnd() {
      const call = currentSimulatedCall()
      if (!call) return 'No hay una llamada que cerrar.'
      endSimulatedCall(call.id)
      if (media.hasSubscribers()) {
        media.notify({
          type: 'ConnectionChanged',
          state: 'disconnected',
          reason: 'ended',
          message: 'La otra persona finalizó la llamada.',
        })
        return null
      }
      void session.refreshCall()
      return null
    },
  }
}
