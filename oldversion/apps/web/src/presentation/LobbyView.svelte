<script lang="ts">
  import type { Snapshot } from '../application/usecases/session.ts'
  import type { LabSession } from '../application/usecases/session.ts'
  import type { PreviewOutlet } from './media-bindings.ts'

  let {
    session,
    snapshot,
    preview,
  }: { session: LabSession; snapshot: Snapshot; preview: PreviewOutlet } = $props()

  let video = $state<HTMLVideoElement | undefined>(undefined)
  const cameras = $derived(snapshot.devices.filter((device) => device.kind === 'videoinput'))
  const microphones = $derived(snapshot.devices.filter((device) => device.kind === 'audioinput'))
  const spectator = $derived(snapshot.call?.role === 'spectator')

  $effect(() => {
    const element = video
    if (!element) return
    preview.attach(element)
    return () => preview.detach(element)
  })
</script>

<section class="panel">
  <h2>Antesala</h2>
  <p>La vista previa es solo local. No se envía nada hasta que te unas.</p>
  {#if snapshot.deviceMessage}
    <p class="status" role="status">{snapshot.deviceMessage}</p>
  {/if}
  {#if snapshot.error}
    <p class="error" role="alert">{snapshot.error.message}</p>
  {/if}
  {#if !spectator}
    <video bind:this={video} autoplay muted playsinline aria-label="Vista previa local"></video>
    <label>
      Cámara
      <select
        value={snapshot.selectedCameraId ?? ''}
        onchange={(event) => {
          const value = event.currentTarget.value
          void session.selectCamera(value)
        }}
      >
        {#each cameras as device (device.id)}
          <option value={device.id}>{device.label}</option>
        {/each}
      </select>
    </label>
    <label>
      Micrófono
      <select
        value={snapshot.selectedMicrophoneId ?? ''}
        onchange={(event) => {
          const value = event.currentTarget.value
          void session.selectMicrophone(value)
        }}
      >
        {#each microphones as device (device.id)}
          <option value={device.id}>{device.label}</option>
        {/each}
      </select>
    </label>
    <div class="actions">
      <button
        type="button"
        aria-pressed={snapshot.wantCamera}
        onclick={() => session.setCameraEnabled(!snapshot.wantCamera)}
      >
        {snapshot.wantCamera ? 'Dejar la cámara fuera' : 'Incluir cámara'}
      </button>
      <button
        type="button"
        aria-pressed={snapshot.wantMicrophone}
        onclick={() => session.setMicrophoneEnabled(!snapshot.wantMicrophone)}
      >
        {snapshot.wantMicrophone ? 'Dejar el micrófono fuera' : 'Incluir micrófono'}
      </button>
    </div>
  {/if}
  <div class="actions">
    {#if !spectator}
      <button class="primary" type="button" disabled={snapshot.busy} onclick={() => session.joinCall('media')}>
        Unirse
      </button>
    {/if}
    <button type="button" disabled={snapshot.busy} onclick={() => session.joinCall('listen')}>
      Unirse para escuchar
    </button>
    <button type="button" onclick={() => session.leaveCall()}>Volver</button>
  </div>
</section>
