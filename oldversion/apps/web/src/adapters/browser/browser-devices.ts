import { AppError } from '../../domain/errors.ts'
import type { DeviceInfo, DeviceSelection, PreviewResult } from '../../domain/types.ts'
import type { DeviceAccess } from '../../application/ports/device-access.ts'

export class BrowserDevices implements DeviceAccess {
  private stream: MediaStream | null = null
  private preview: HTMLVideoElement | null = null

  async listDevices(): Promise<DeviceInfo[]> {
    if (!navigator.mediaDevices?.enumerateDevices) return []
    const devices = await navigator.mediaDevices.enumerateDevices()
    return devices
      .filter((device) => device.kind === 'audioinput' || device.kind === 'videoinput')
      .map((device) => ({
        id: device.deviceId,
        label: device.label || (device.kind === 'videoinput' ? 'Cámara' : 'Micrófono'),
        kind: device.kind === 'videoinput' ? 'videoinput' : 'audioinput',
      }))
  }

  async startPreview(selection: DeviceSelection): Promise<PreviewResult> {
    await this.stopPreview()
    if (!selection.audio && !selection.video) return { audio: false, video: false }
    if (!window.isSecureContext) {
      throw new AppError(
        'permission-denied',
        'La cámara y el micrófono requieren una página segura (HTTPS).',
      )
    }
    try {
      this.stream = await navigator.mediaDevices.getUserMedia(constraints(selection))
    } catch (error) {
      if (selection.video && selection.audio && isMissing(error)) {
        try {
          this.stream = await navigator.mediaDevices.getUserMedia(
            constraints({ ...selection, video: false }),
          )
        } catch (audioError) {
          throw toDeviceError(audioError)
        }
      } else {
        throw toDeviceError(error)
      }
    }
    this.attachCurrent()
    return {
      audio: (this.stream?.getAudioTracks().length ?? 0) > 0,
      video: (this.stream?.getVideoTracks().length ?? 0) > 0,
    }
  }

  async stopPreview(): Promise<void> {
    this.stream?.getTracks().forEach((track) => track.stop())
    this.stream = null
    if (this.preview) this.preview.srcObject = null
  }

  attach(element: HTMLVideoElement): void {
    this.preview = element
    element.muted = true
    this.attachCurrent()
  }

  detach(element: HTMLVideoElement): void {
    if (this.preview !== element) return
    element.srcObject = null
    this.preview = null
  }

  private attachCurrent(): void {
    if (!this.preview || !this.stream) return
    this.preview.srcObject = this.stream
    this.preview.muted = true
    void this.preview.play().catch(() => undefined)
  }
}

export function readSecureContext(): boolean {
  return window.isSecureContext
}

function constraints(selection: DeviceSelection): MediaStreamConstraints {
  return {
    audio: selection.audio
      ? selection.microphoneId
        ? { deviceId: { exact: selection.microphoneId } }
        : true
      : false,
    video: selection.video
      ? selection.cameraId
        ? { deviceId: { exact: selection.cameraId } }
        : true
      : false,
  }
}

function isMissing(error: unknown): boolean {
  return error instanceof DOMException && (error.name === 'NotFoundError' || error.name === 'OverconstrainedError')
}

function toDeviceError(error: unknown): AppError {
  if (error instanceof DOMException && (error.name === 'NotAllowedError' || error.name === 'SecurityError')) {
    return new AppError(
      'permission-denied',
      'El navegador bloqueó la cámara o el micrófono. Puedes unirte solo para escuchar.',
    )
  }
  if (isMissing(error)) {
    return new AppError(
      'device-missing',
      'No se encontró la cámara o el micrófono. Puedes continuar con los medios disponibles.',
    )
  }
  const message = error instanceof Error ? error.message : 'No se pudo preparar la cámara o el micrófono.'
  return new AppError('device-missing', message)
}
