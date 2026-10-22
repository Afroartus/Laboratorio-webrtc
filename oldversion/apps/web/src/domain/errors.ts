export class AppError extends Error {
  readonly code: string
  readonly requestId: string | undefined

  constructor(code: string, message: string, requestId?: string) {
    super(message)
    this.name = 'AppError'
    this.code = code
    this.requestId = requestId
  }
}

export class ScreenShareCancelled extends Error {
  readonly code = 'screen-share-cancelled'

  constructor() {
    super('Se canceló la selección de pantalla')
    this.name = 'ScreenShareCancelled'
  }
}

export function isAppError(error: unknown): error is AppError {
  return error instanceof AppError
}

export function isScreenShareCancelled(error: unknown): boolean {
  return (
    error instanceof ScreenShareCancelled ||
    (typeof error === 'object' &&
      error !== null &&
      'code' in error &&
      (error as { code: unknown }).code === 'screen-share-cancelled')
  )
}
