<script lang="ts">
  import type { LabSession, Snapshot } from '../application/usecases/session.ts'
  import type { CallSimulation } from './simulation.ts'

  let {
    simulation,
    session,
    snapshot,
  }: { simulation: CallSimulation; session: LabSession; snapshot: Snapshot } = $props()

  let notice = $state<string | null>(null)
  let held = $state(false)

  async function run(action: () => Promise<string | null>): Promise<void> {
    notice = await action()
  }

  async function denyDevices(): Promise<void> {
    simulation.denyDevices()
    if (snapshot.phase === 'lobby' || snapshot.phase === 'call-entry') {
      notice = null
      await session.goToLobby()
      return
    }
    notice = 'La próxima antesala denegará la cámara y el micrófono.'
  }

  async function allowDevices(): Promise<void> {
    simulation.allowDevices()
    notice = null
    if (snapshot.phase === 'lobby') await session.goToLobby()
  }

  function holdJoin(): void {
    held = true
    simulation.holdJoin()
    notice = 'La próxima unión se queda en «Conectando…» hasta que la sueltes.'
  }

  function releaseJoin(): void {
    held = false
    simulation.releaseJoin()
    notice = null
  }
</script>

<section class="sim" aria-label="Controles de simulación">
  <h2>Controles de simulación</h2>
  <p>Recorren roles y estados de la llamada sin salir de esta página.</p>
  {#if notice}
    <p class="status" role="status">{notice}</p>
  {/if}
  <div class="actions">
    {#if snapshot.call?.role === 'creator'}
      <button type="button" onclick={() => run(() => simulation.enterAsInvitee())}>Entrar como invitado</button>
      <button type="button" onclick={() => run(() => simulation.enterAsSpectator())}>Entrar como espectador</button>
    {:else if snapshot.call}
      <button type="button" onclick={() => run(() => simulation.returnAsCreator())}>Volver como creador</button>
    {/if}
    <button type="button" onclick={denyDevices}>Denegar cámara y micrófono</button>
    <button type="button" onclick={allowDevices}>Permitir cámara y micrófono</button>
    {#if held}
      <button type="button" onclick={releaseJoin}>Soltar la unión</button>
    {:else}
      <button type="button" onclick={holdJoin}>Retener la próxima unión</button>
    {/if}
    <button type="button" onclick={() => (notice = simulation.simulateReconnecting())}>Simular reconexión</button>
    <button type="button" onclick={() => (notice = simulation.simulateDegradedNetwork())}>Simular red degradada</button>
    <button type="button" onclick={() => (notice = simulation.simulateRejected())}>Simular acceso rechazado</button>
    <button type="button" onclick={() => (notice = simulation.simulateRemoteEnd())}>Simular cierre del otro lado</button>
  </div>
</section>
