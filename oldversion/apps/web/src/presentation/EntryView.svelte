<script lang="ts">
  import type { InviteeRole } from '../domain/types.ts'
  import type { Snapshot } from '../application/usecases/session.ts'
  import type { LabSession } from '../application/usecases/session.ts'
  import { navigate } from './router.ts'

  let { session, snapshot }: { session: LabSession; snapshot: Snapshot } = $props()

  let inviteeId = $state('')
  let inviteeRole = $state<InviteeRole>('participant')

  const others = $derived(snapshot.users.filter((user) => user.id !== snapshot.user?.id))

  async function create(event: SubmitEvent) {
    event.preventDefault()
    const callId = await session.createCall(inviteeId, inviteeRole)
    if (!callId) return
    navigate(`/calls/${callId}`)
  }

  function roleLabel(role: string): string {
    if (role === 'creator') return 'Creador'
    if (role === 'spectator') return 'Espectador'
    return 'Participante'
  }

  function statusLabel(status: string): string {
    switch (status) {
      case 'CREATED':
        return 'Creada'
      case 'ACTIVE':
        return 'En curso'
      case 'ENDING':
        return 'Finalizando'
      case 'ENDED':
        return 'Finalizada'
      case 'CANCELLED':
        return 'Cancelada'
      default:
        return status
    }
  }
</script>

{#if snapshot.call && snapshot.phase === 'call-entry'}
  <section class="panel">
    <h2>Entrada a la llamada</h2>
    {#if snapshot.statusMessage}
      <p class="status" role="status">{snapshot.statusMessage}</p>
    {/if}
    {#if snapshot.error}
      <p class="error" role="alert">{snapshot.error.message}</p>
    {/if}
    <dl>
      <dt>Estado</dt>
      <dd>{statusLabel(snapshot.call.status)}</dd>
      <dt>Tu rol</dt>
      <dd>{roleLabel(snapshot.call.role)}</dd>
      <dt>Plazo de ingreso</dt>
      <dd>
        {snapshot.call.joinDeadline
          ? new Intl.DateTimeFormat('es', { dateStyle: 'medium', timeStyle: 'short' }).format(
              new Date(snapshot.call.joinDeadline),
            )
          : 'Sin plazo'}
      </dd>
    </dl>
    {#if snapshot.call.role === 'creator'}
      <label>
        Enlace de invitación
        <input class="link" readonly value={snapshot.call.joinUrl} />
      </label>
    {/if}
    {#if snapshot.call.status === 'CREATED' || snapshot.call.status === 'ACTIVE'}
      <div class="actions">
        <button class="primary" type="button" onclick={() => session.goToLobby()}>Ir a la antesala</button>
      </div>
    {/if}
  </section>
{:else}
  <section class="panel">
    <h2>Nueva llamada</h2>
    {#if snapshot.error}
      <p class="error" role="alert">{snapshot.error.message}</p>
    {/if}
    <form onsubmit={create}>
      <label>
        Invitar a
        <select bind:value={inviteeId} required>
          <option value="">Elige una persona</option>
          {#each others as person (person.id)}
            <option value={person.id}>{person.displayName}</option>
          {/each}
        </select>
      </label>
      <label>
        Rol del invitado
        <select bind:value={inviteeRole}>
          <option value="participant">Participante</option>
          <option value="spectator">Espectador</option>
        </select>
      </label>
      <button class="primary" type="submit" disabled={snapshot.busy || others.length === 0}>Crear llamada</button>
    </form>
  </section>
{/if}
