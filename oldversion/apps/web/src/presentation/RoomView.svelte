<script lang="ts">
  import type { Snapshot } from '../application/usecases/session.ts'
  import type { LabSession } from '../application/usecases/session.ts'
  import type { TrackBinder } from './media-bindings.ts'
  import AudioTrack from './AudioTrack.svelte'
  import MediaTile from './MediaTile.svelte'

  let {
    session,
    snapshot,
    binder,
  }: { session: LabSession; snapshot: Snapshot; binder: TrackBinder } = $props()

  const screen = $derived(
    snapshot.tracks.find((track) => track.source === 'screen' && track.kind === 'video' && !track.local),
  )
  const remoteCameras = $derived(
    snapshot.tracks.filter((track) => track.source === 'camera' && track.kind === 'video' && !track.local),
  )
  const localCamera = $derived(
    snapshot.tracks.find((track) => track.source === 'camera' && track.kind === 'video' && track.local),
  )
  const remoteAudio = $derived(snapshot.tracks.filter((track) => track.kind === 'audio' && !track.local))
  const main = $derived(screen ?? remoteCameras[0])
  const thumbs = $derived.by(() => {
    const extra = screen ? remoteCameras : remoteCameras.slice(1)
    return localCamera ? [...extra, localCamera] : extra
  })

  function nameFor(participantId: string, fallback: string): string {
    return snapshot.participants.find((participant) => participant.id === participantId)?.displayName ?? fallback
  }
</script>

<section class="panel">
  <h2>Sala</h2>
  {#if snapshot.networkMessage}
    <p class="status" role="status">{snapshot.networkMessage}</p>
  {/if}
  {#if snapshot.statusMessage}
    <p class="status" role="status">{snapshot.statusMessage}</p>
  {/if}
  {#if snapshot.error}
    <p class="error" role="alert">{snapshot.error.message}</p>
  {/if}
  {#if snapshot.audioBlocked}
    <button class="primary" type="button" onclick={() => session.enableAudioPlayback()}>Activar audio</button>
  {/if}

  <div class="stage" class:screen={Boolean(screen)}>
    {#if main}
      <MediaTile
        trackId={main.id}
        {binder}
        muted={main.local}
        label={main.source === 'screen' ? 'Pantalla compartida' : nameFor(main.participantId, 'Video remoto')}
      />
    {:else}
      <p class="status" role="status">Esperando el video del otro participante.</p>
    {/if}
    <div class="thumbs">
      {#each thumbs as track (track.id)}
        <MediaTile
          trackId={track.id}
          {binder}
          muted={track.local}
          label={track.local ? 'Tu cámara' : nameFor(track.participantId, 'Cámara')}
        />
      {/each}
    </div>
  </div>

  {#each remoteAudio as track (track.id)}
    <AudioTrack trackId={track.id} {binder} />
  {/each}

  <div class="actions">
    {#if snapshot.call?.permissions.publishMicrophone}
      <button
        type="button"
        aria-pressed={snapshot.publication.microphone}
        aria-label={snapshot.publication.microphone ? 'Silenciar micrófono' : 'Activar micrófono'}
        onclick={() => session.setMicrophoneEnabled(!snapshot.publication.microphone)}
      >
        {snapshot.publication.microphone ? 'Silenciar micrófono' : 'Activar micrófono'}
      </button>
    {/if}
    {#if snapshot.call?.permissions.publishCamera}
      <button
        type="button"
        aria-pressed={snapshot.publication.camera}
        aria-label={snapshot.publication.camera ? 'Apagar cámara' : 'Encender cámara'}
        onclick={() => session.setCameraEnabled(!snapshot.publication.camera)}
      >
        {snapshot.publication.camera ? 'Apagar cámara' : 'Encender cámara'}
      </button>
    {/if}
    {#if snapshot.call?.permissions.publishScreen}
      {#if snapshot.publication.screen}
        <button type="button" onclick={() => session.stopScreenShare()}>Dejar de compartir pantalla</button>
      {:else}
        <button type="button" onclick={() => session.startScreenShare()}>Compartir pantalla</button>
      {/if}
    {/if}
    <button type="button" onclick={() => session.leaveCall()}>Salir</button>
    {#if snapshot.call?.role === 'creator'}
      <button class="danger" type="button" onclick={() => session.endCall()}>Finalizar para todos</button>
    {/if}
  </div>

  {#if snapshot.diagnostics}
    <details>
      <summary>Diagnóstico de la llamada</summary>
      <dl>
        <dt>Par ICE</dt>
        <dd>{snapshot.diagnostics.icePair ?? 'sin dato'}</dd>
        <dt>Relé</dt>
        <dd>{snapshot.diagnostics.relay ? 'sí' : 'no'}</dd>
        <dt>RTT</dt>
        <dd>{snapshot.diagnostics.rttMs === null ? 'sin dato' : `${snapshot.diagnostics.rttMs} ms`}</dd>
        <dt>Pérdida</dt>
        <dd>
          {snapshot.diagnostics.packetLoss === null
            ? 'sin dato'
            : `${Math.round(snapshot.diagnostics.packetLoss * 100)} %`}
        </dd>
        <dt>Bytes recibidos</dt>
        <dd>{snapshot.diagnostics.bitrateBps ?? 'sin dato'}</dd>
        <dt>Tiempo hasta medios</dt>
        <dd>
          {snapshot.diagnostics.timeToRemoteMediaMs === null
            ? 'sin dato'
            : `${snapshot.diagnostics.timeToRemoteMediaMs} ms`}
        </dd>
      </dl>
    </details>
  {/if}
</section>
