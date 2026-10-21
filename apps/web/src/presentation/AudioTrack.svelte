<script lang="ts">
  import type { TrackBinder } from './media-bindings.ts'

  let { trackId, binder }: { trackId: string; binder: TrackBinder } = $props()
  let element = $state<HTMLAudioElement | undefined>(undefined)

  $effect(() => {
    const node = element
    const id = trackId
    if (!node) return
    binder.attach(id, node)
    return () => binder.detach(id, node)
  })
</script>

<audio bind:this={element} autoplay></audio>
