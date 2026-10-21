export interface AttachableTrack {
  attach(element: HTMLMediaElement): void
  detach(element: HTMLMediaElement): void
  local: boolean
  kind: 'audio' | 'video'
}

export interface TrackLookup {
  lookup(trackId: string): AttachableTrack | null
}

/** La vista local no reproduce su propio audio. */
export class BrowserTrackBinder {
  private readonly tracks: TrackLookup

  constructor(tracks: TrackLookup) {
    this.tracks = tracks
  }

  attach(trackId: string, element: HTMLMediaElement): void {
    const track = this.tracks.lookup(trackId)
    if (!track || (track.local && track.kind === 'audio')) return
    track.attach(element)
    if (track.local) element.muted = true
  }

  detach(trackId: string, element: HTMLMediaElement): void {
    this.tracks.lookup(trackId)?.detach(element)
  }
}
