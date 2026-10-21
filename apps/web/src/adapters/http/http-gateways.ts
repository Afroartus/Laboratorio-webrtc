import { AppError } from '../../domain/errors.ts'
import type { CallDetails, CallStatus, CreatedCall, InviteeRole, JoinCredentials, Permissions, Role, User } from '../../domain/types.ts'
import type { AuthGateway } from '../../application/ports/auth-gateway.ts'
import type { CallGateway } from '../../application/ports/call-gateway.ts'

const CSRF_HEADER = 'X-CSRF-Token'

/** El secreto CSRF vive solo en memoria de este objeto. No se escribe en almacenamiento del navegador. */
export class CsrfMemory {
  private token: string | null = null

  current(): string | null {
    return this.token
  }

  replace(token: string | null): void {
    this.token = token
  }
}

export interface HttpClient {
  request(path: string, init: RequestInit, csrfToken: string | null): Promise<Response>
}

export function createFetchClient(fetchImpl: typeof fetch = fetch): HttpClient {
  return {
    request(path, init, csrfToken) {
      const headers = new Headers(init.headers)
      headers.set('accept', 'application/json')
      if (csrfToken) headers.set(CSRF_HEADER, csrfToken)
      if (init.body && !headers.has('content-type')) headers.set('content-type', 'application/json')
      return fetchImpl(path, {
        ...init,
        headers,
        credentials: 'include',
        cache: 'no-store',
      })
    },
  }
}

export class HttpAuthGateway implements AuthGateway {
  private readonly http: HttpClient
  private readonly csrf: CsrfMemory

  constructor(http: HttpClient, csrf: CsrfMemory) {
    this.http = http
    this.csrf = csrf
  }

  async login(username: string, password: string): Promise<User> {
    const response = await this.http.request(
      '/api/v1/auth/login',
      { method: 'POST', body: JSON.stringify({ username, password }) },
      this.csrf.current(),
    )
    await assertOk(response)
    const user = await this.currentSession()
    if (!user) throw new AppError('UNAUTHORIZED', 'La sesión no quedó establecida.')
    return user
  }

  async logout(): Promise<void> {
    const response = await this.http.request('/api/v1/auth/logout', { method: 'POST' }, this.csrf.current())
    await assertOk(response)
    this.csrf.replace(null)
  }

  async currentSession(): Promise<User | null> {
    const response = await this.http.request('/api/v1/auth/me', { method: 'GET' }, null)
    if (response.status === 401) return null
    await assertOk(response)
    const session = parseSession(await readJson(response))
    this.csrf.replace(session.csrfToken)
    return session.user
  }

  async listUsers(): Promise<User[]> {
    const response = await this.http.request('/api/v1/users', { method: 'GET' }, this.csrf.current())
    await assertOk(response)
    return parseUsers(await readJson(response))
  }
}

export class HttpCallGateway implements CallGateway {
  private readonly http: HttpClient
  private readonly csrf: CsrfMemory

  constructor(http: HttpClient, csrf: CsrfMemory) {
    this.http = http
    this.csrf = csrf
  }

  async createCall(input: {
    inviteeUserId: string
    inviteeRole: InviteeRole
    idempotencyKey: string
  }): Promise<CreatedCall> {
    const response = await this.http.request(
      '/api/v1/calls',
      {
        method: 'POST',
        headers: { 'Idempotency-Key': input.idempotencyKey },
        body: JSON.stringify({ inviteeUserId: input.inviteeUserId, inviteeRole: input.inviteeRole }),
      },
      this.csrf.current(),
    )
    await assertOk(response)
    return parseCreatedCall(await readJson(response))
  }

  async getCall(callId: string): Promise<CallDetails> {
    const response = await this.http.request(
      `/api/v1/calls/${encodeURIComponent(callId)}`,
      { method: 'GET' },
      this.csrf.current(),
    )
    await assertOk(response)
    return parseCallDetails(await readJson(response))
  }

  async endCall(callId: string): Promise<{ status: CallStatus }> {
    const response = await this.http.request(
      `/api/v1/calls/${encodeURIComponent(callId)}/end`,
      { method: 'POST' },
      this.csrf.current(),
    )
    await assertOk(response)
    const body = asRecord(await readJson(response), 'cierre')
    return { status: parseStatus(readString(body, 'status')) }
  }

  async issueJoinToken(callId: string): Promise<JoinCredentials> {
    const response = await this.http.request(
      `/api/v1/calls/${encodeURIComponent(callId)}/join-token`,
      { method: 'POST' },
      this.csrf.current(),
    )
    await assertOk(response)
    return parseJoinCredentials(await readJson(response))
  }
}

async function assertOk(response: Response): Promise<void> {
  if (response.ok) return
  throw await toAppError(response)
}

async function toAppError(response: Response): Promise<AppError> {
  let code = statusCode(response.status)
  let message = statusMessage(response.status)
  let requestId: string | undefined
  try {
    const body = (await response.json()) as unknown
    if (isRecord(body)) {
      if (typeof body.code === 'string' && body.code) code = body.code
      if (typeof body.message === 'string' && body.message) message = body.message
      if (typeof body.requestId === 'string') requestId = body.requestId
    }
  } catch {
    // Cuerpo vacío o no JSON: queda el código derivado del estado HTTP.
  }
  return new AppError(code, message, requestId)
}

function statusCode(status: number): string {
  switch (status) {
    case 401:
      return 'UNAUTHORIZED'
    case 403:
      return 'FORBIDDEN'
    case 404:
      return 'NOT_FOUND'
    case 409:
      return 'CONFLICT'
    case 410:
      return 'CALL_ENDED'
    case 429:
      return 'RATE_LIMITED'
    case 503:
      return 'UNAVAILABLE'
    default:
      return 'UNEXPECTED'
  }
}

function statusMessage(status: number): string {
  switch (status) {
    case 401:
      return 'La sesión no es válida.'
    case 403:
      return 'No tienes permiso para esta operación.'
    case 404:
      return 'No se encontró la llamada.'
    case 409:
      return 'La llamada cambió de estado.'
    case 410:
      return 'La llamada terminó.'
    case 429:
      return 'Demasiados intentos. Espera un momento.'
    case 503:
      return 'Un servicio del laboratorio no está disponible.'
    default:
      return 'La solicitud no se pudo completar.'
  }
}

async function readJson(response: Response): Promise<unknown> {
  return response.json() as Promise<unknown>
}

function parseSession(body: unknown): { user: User; csrfToken: string } {
  const record = asRecord(body, 'sesión')
  const csrfToken = readString(record, 'csrfToken')
  const source = isRecord(record.user) ? record.user : record
  return { user: parseUser(source), csrfToken }
}

function parseUsers(body: unknown): User[] {
  const list = Array.isArray(body) ? body : asRecord(body, 'usuarios').users
  if (!Array.isArray(list)) throw new AppError('UNEXPECTED', 'La lista de usuarios no tiene el formato esperado.')
  return list.map((item) => parseUser(item))
}

function parseCreatedCall(body: unknown): CreatedCall {
  const record = asRecord(body, 'llamada')
  return {
    callId: readString(record, 'callId'),
    joinUrl: readString(record, 'joinUrl'),
    status: parseStatus(readString(record, 'status')),
  }
}

function parseCallDetails(body: unknown): CallDetails {
  const record = asRecord(body, 'llamada')
  const joinDeadline = record.joinDeadline
  return {
    callId: readString(record, 'callId'),
    status: parseStatus(readString(record, 'status')),
    role: parseRole(readString(record, 'role')),
    permissions: parsePermissions(record.permissions),
    joinDeadline: typeof joinDeadline === 'string' ? joinDeadline : null,
  }
}

function parseJoinCredentials(body: unknown): JoinCredentials {
  const record = asRecord(body, 'credencial')
  return {
    callId: readString(record, 'callId'),
    roomName: readString(record, 'roomName'),
    serverUrl: readString(record, 'serverUrl'),
    participantToken: readString(record, 'participantToken'),
    expiresAt: readString(record, 'expiresAt'),
    permissions: parsePermissions(record.permissions),
  }
}

function parseUser(body: unknown): User {
  const record = asRecord(body, 'usuario')
  return {
    id: readString(record, 'id'),
    username: readString(record, 'username'),
    displayName: readString(record, 'displayName'),
  }
}

function parsePermissions(body: unknown): Permissions {
  const record = asRecord(body, 'permisos')
  return {
    publishCamera: readBoolean(record, 'publishCamera'),
    publishMicrophone: readBoolean(record, 'publishMicrophone'),
    publishScreen: readBoolean(record, 'publishScreen'),
    subscribe: readBoolean(record, 'subscribe'),
  }
}

function parseRole(value: string): Role {
  if (value === 'creator' || value === 'participant' || value === 'spectator') return value
  throw new AppError('UNEXPECTED', 'El rol de la llamada no es reconocido.')
}

function parseStatus(value: string): CallStatus {
  if (
    value === 'CREATED' ||
    value === 'ACTIVE' ||
    value === 'ENDING' ||
    value === 'ENDED' ||
    value === 'CANCELLED'
  ) {
    return value
  }
  throw new AppError('UNEXPECTED', 'El estado de la llamada no es reconocido.')
}

function asRecord(body: unknown, label: string): Record<string, unknown> {
  if (!isRecord(body)) throw new AppError('UNEXPECTED', `La respuesta de ${label} no es un objeto.`)
  return body
}

function readString(record: Record<string, unknown>, key: string): string {
  const value = record[key]
  if (typeof value !== 'string' || !value) {
    throw new AppError('UNEXPECTED', `Falta el campo ${key} en la respuesta.`)
  }
  return value
}

function readBoolean(record: Record<string, unknown>, key: string): boolean {
  const value = record[key]
  if (typeof value !== 'boolean') throw new AppError('UNEXPECTED', `Falta el permiso ${key} en la respuesta.`)
  return value
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
