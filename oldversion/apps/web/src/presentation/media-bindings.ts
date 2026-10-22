export interface PreviewOutlet {
  attach(element: HTMLVideoElement): void
  detach(element: HTMLVideoElement): void
}

export interface TrackBinder {
  attach(trackId: string, element: HTMLMediaElement): void
  detach(trackId: string, element: HTMLMediaElement): void
}
