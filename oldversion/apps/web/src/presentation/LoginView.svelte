<script lang="ts">
  import type { Snapshot } from '../application/usecases/session.ts'
  import type { LabSession } from '../application/usecases/session.ts'

  let {
    session,
    snapshot,
    pendingCall,
  }: { session: LabSession; snapshot: Snapshot; pendingCall: boolean } = $props()

  let username = $state('')
  let password = $state('')

  async function submit(event: SubmitEvent) {
    event.preventDefault()
    await session.signIn(username, password)
  }
</script>

<section class="panel">
  <h2>Iniciar sesión</h2>
  {#if pendingCall}
    <p>Inicia sesión para abrir la llamada.</p>
  {/if}
  {#if snapshot.error}
    <p class="error" role="alert">{snapshot.error.message}</p>
  {/if}
  <form onsubmit={submit}>
    <label>
      Usuario
      <input name="username" autocomplete="username" bind:value={username} />
    </label>
    <label>
      Contraseña
      <input name="password" type="password" autocomplete="current-password" bind:value={password} />
    </label>
    <button class="primary" type="submit" disabled={snapshot.busy}>Entrar</button>
  </form>
</section>
