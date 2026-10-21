<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import type { LabSession, Snapshot } from '../application/usecases/session.ts'
  import EntryView from './EntryView.svelte'
  import LobbyView from './LobbyView.svelte'
  import LoginView from './LoginView.svelte'
  import type { PreviewOutlet, TrackBinder } from './media-bindings.ts'
  import RoomView from './RoomView.svelte'
  import { navigate, readRoute, type Route } from './router.ts'
  import SimulationPanel from './SimulationPanel.svelte'
  import type { CallSimulation } from './simulation.ts'
  import './app.css'

  let {
    session,
    preview,
    binder,
    simulated = false,
    simulation = null,
  }: {
    session: LabSession
    preview: PreviewOutlet
    binder: TrackBinder
    simulated?: boolean
    simulation?: CallSimulation | null
  } = $props()

  let snapshot = $state<Snapshot>(untrack(() => session.getSnapshot()))
  let route = $state<Route>(readRoute(window.location.pathname))
  let loadedKey = ''

  onMount(() => {
    const stop = session.subscribe((next) => {
      snapshot = next
    })
    const onPop = () => {
      route = readRoute(window.location.pathname)
    }
    window.addEventListener('popstate', onPop)
    void session.loadSession()
    return () => {
      stop()
      window.removeEventListener('popstate', onPop)
    }
  })

  $effect(() => {
    const current = route
    const userId = snapshot.user?.id ?? ''
    const phase = snapshot.phase
    if (current.name === 'home') {
      loadedKey = ''
      if (
        phase === 'in-call' ||
        phase === 'connecting' ||
        phase === 'lobby' ||
        phase === 'call-entry' ||
        phase === 'ended' ||
        phase === 'rejected'
      ) {
        void session.goHome()
      }
      return
    }
    if (!userId) return
    const key = `${userId}:${current.callId}`
    if (key === loadedKey) return
    loadedKey = key
    void session.openCall(current.callId)
  })

  $effect(() => {
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') void session.refreshCall()
    }, 10_000)
    const onFocus = () => {
      void session.refreshCall()
    }
    const onVisibility = () => {
      if (document.visibilityState === 'visible') void session.refreshCall()
    }
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', onFocus)
      document.removeEventListener('visibilitychange', onVisibility)
    }
  })

  async function signOut() {
    loadedKey = ''
    await session.signOut()
    navigate('/')
  }
</script>

<div class="shell">
  <header>
    <h1>Laboratorio WebRTC</h1>
    {#if snapshot.user}
      <div class="actions">
        <span>{snapshot.user.displayName}</span>
        <button type="button" onclick={() => navigate('/')}>Inicio</button>
        <button type="button" onclick={signOut}>Cerrar sesión</button>
      </div>
    {/if}
  </header>

  {#if !snapshot.secureContext}
    <p class="banner" role="status">
      Esta página no está en un contexto seguro. La cámara y el micrófono no están disponibles.
    </p>
  {/if}
  {#if simulated}
    <p class="banner">
      Perfil simulado: la API y LiveKit no están conectados. Los medios son simulados. El primer
      intento de compartir pantalla se cancela; el siguiente sí la comparte.
    </p>
    {#if simulation}
      <SimulationPanel {simulation} {session} {snapshot} />
    {/if}
  {/if}

  {#if snapshot.phase === 'logged-out'}
    <LoginView {session} {snapshot} pendingCall={route.name === 'call'} />
  {:else if snapshot.phase === 'connecting'}
    <p class="status" role="status" aria-busy="true">Conectando…</p>
  {:else if snapshot.phase === 'lobby'}
    <LobbyView {session} {snapshot} {preview} />
  {:else if snapshot.phase === 'in-call'}
    <RoomView {session} {snapshot} {binder} />
  {:else if snapshot.phase === 'ended' || snapshot.phase === 'rejected'}
    <section class="panel">
      <h2>{snapshot.phase === 'rejected' ? 'Acceso rechazado' : 'Llamada finalizada'}</h2>
      <p class="status" role="status">{snapshot.statusMessage ?? snapshot.error?.message ?? 'La llamada no está disponible.'}</p>
      <button type="button" onclick={() => navigate('/')}>Volver al inicio</button>
    </section>
  {:else}
    <EntryView {session} {snapshot} />
  {/if}
</div>
