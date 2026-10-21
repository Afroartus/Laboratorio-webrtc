/// <reference types="node" />
import assert from 'node:assert/strict'
import { afterEach, describe, it } from 'node:test'
import { AppError } from '../../domain/errors.ts'
import { CsrfMemory, HttpAuthGateway, HttpCallGateway, createFetchClient } from './http-gateways.ts'

const permissions = {
  publishCamera: true,
  publishMicrophone: true,
  publishScreen: true,
  subscribe: true,
}

function json(status: number, body: unknown, headers?: HeadersInit): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  })
}

describe('adaptador HTTP', () => {
  const original = globalThis.fetch
  afterEach(() => {
    globalThis.fetch = original
  })

  it('guarda el CSRF en memoria y lo envía en las mutaciones, sin cachear el token de medios', async () => {
    const seen: Array<{ url: string; init: RequestInit }> = []
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      seen.push({ url, init: init ?? {} })
      if (url.endsWith('/auth/login')) return new Response(null, { status: 204 })
      if (url.endsWith('/auth/me')) {
        return json(200, {
          user: { id: 'juan', username: 'juan', displayName: 'Juan' },
          csrfToken: 'csrf-secreto',
        })
      }
      if (url.endsWith('/calls/call-1/join-token')) {
        return json(200, {
          callId: 'call-1',
          roomName: 'call_call-1',
          serverUrl: 'wss://rtc.webrtc.test:8443',
          participantToken: 'jwt-no-guardar',
          expiresAt: '2026-10-21T20:00:00Z',
          permissions,
        })
      }
      return json(500, { code: 'UNEXPECTED', message: 'ruta no simulada' })
    }) as typeof fetch

    const csrf = new CsrfMemory()
    const http = createFetchClient()
    const auth = new HttpAuthGateway(http, csrf)
    const calls = new HttpCallGateway(http, csrf)
    const user = await auth.login('juan', 'secreta')
    assert.equal(user.displayName, 'Juan')
    assert.equal(csrf.current(), 'csrf-secreto')

    const join = seen.find((entry) => entry.url.endsWith('/auth/login'))
    assert.equal(join?.init.credentials, 'include')
    assert.equal(join?.init.cache, 'no-store')

    const token = await calls.issueJoinToken('call-1')
    assert.equal(token.participantToken, 'jwt-no-guardar')
    const tokenRequest = seen.find((entry) => entry.url.endsWith('/join-token'))
    assert.equal(tokenRequest?.init.cache, 'no-store')
    assert.equal(tokenRequest?.init.credentials, 'include')
    const headers = new Headers(tokenRequest?.init.headers)
    assert.equal(headers.get('X-CSRF-Token'), 'csrf-secreto')
  })

  it('traduce 410 y el código del cuerpo a errores de aplicación', async () => {
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/end')) return json(410, { code: 'CALL_ENDED', message: 'La llamada terminó', requestId: 'req-1' })
      if (url.includes('/calls/')) return json(403, { code: 'FORBIDDEN', message: 'No participas', requestId: 'req-2' })
      return json(401, {})
    }) as typeof fetch

    const calls = new HttpCallGateway(createFetchClient(), new CsrfMemory())
    await assert.rejects(calls.getCall('call-1'), (error: unknown) => {
      assert.ok(error instanceof AppError)
      assert.equal(error.code, 'FORBIDDEN')
      assert.equal(error.requestId, 'req-2')
      return true
    })
    await assert.rejects(calls.endCall('call-1'), (error: unknown) => {
      assert.ok(error instanceof AppError)
      assert.equal(error.code, 'CALL_ENDED')
      assert.equal(error.message, 'La llamada terminó')
      return true
    })
  })

  it('envía la clave de idempotencia al crear la llamada', async () => {
    let idempotency = ''
    globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
      idempotency = new Headers(init?.headers).get('Idempotency-Key') ?? ''
      return json(201, { callId: 'call-1', joinUrl: 'https://webrtc.test:8443/calls/call-1', status: 'CREATED' })
    }) as typeof fetch

    const calls = new HttpCallGateway(createFetchClient(), new CsrfMemory())
    const created = await calls.createCall({
      inviteeUserId: 'maria',
      inviteeRole: 'spectator',
      idempotencyKey: 'clave-1',
    })
    assert.equal(created.callId, 'call-1')
    assert.equal(idempotency, 'clave-1')
  })
})
