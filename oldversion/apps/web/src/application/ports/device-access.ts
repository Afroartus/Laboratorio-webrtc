import type { DeviceInfo, DeviceSelection, PreviewResult } from '../../domain/types.ts'

export interface DeviceAccess {
  listDevices(): Promise<DeviceInfo[]>
  /** Vista previa local. No publica en la llamada. */
  startPreview(selection: DeviceSelection): Promise<PreviewResult>
  stopPreview(): Promise<void>
}
