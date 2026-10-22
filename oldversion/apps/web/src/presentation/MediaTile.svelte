<script lang="ts">
  import type { TrackBinder } from './media-bindings.ts'

  let {
    trackId,
    binder,
    muted = false,
    label,
  }: { trackId: string; binder: TrackBinder; muted?: boolean; label: string } = $props()

  let element = $state<HTMLVideoElement | undefined>(undefined)

  $effect(() => {
    const node = element
    const id = trackId
    if (!node) return
    binder.attach(id, node)
    if (muted) node.muted = true
    return () => binder.detach(id, node)
  })
</script>

<figure>
  <video bind:this={element} autoplay muted={muted} playsinline aria-label={label}></video>
  <figcaption>{label}</figcaption>
</figure>
