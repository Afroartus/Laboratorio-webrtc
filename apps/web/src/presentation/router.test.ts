/// <reference types="node" />
import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { readRoute } from './router.ts'

describe('rutas', () => {
  it('reconoce el inicio y el identificador de la llamada', () => {
    assert.deepEqual(readRoute('/'), { name: 'home' })
    assert.deepEqual(readRoute('/calls/abc'), { name: 'call', callId: 'abc' })
    assert.deepEqual(readRoute('/calls/a%2Fb'), { name: 'call', callId: 'a/b' })
    assert.deepEqual(readRoute('/otra'), { name: 'home' })
  })
})
