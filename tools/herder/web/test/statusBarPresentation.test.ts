import assert from 'node:assert/strict'
import test from 'node:test'

import { statusBarHealth } from '../src/shared/statusBarPresentation.ts'

test('health ticks stay red with distinct waiting hover until each signal is proven', () => {
  assert.deepEqual(statusBarHealth({
    problems: { stream: 'Connecting to live fleet…' }, substrateProof: { herdr: false, hcom: false }, lastEventLabel: '—',
  }), [
    { label: 'herdr', healthy: false, title: 'Herdr not yet proven — waiting for the first fleet snapshot.' },
    { label: 'hcom', healthy: false, title: 'hcom not yet proven — waiting for bus health.' },
    { label: 'SSE', healthy: false, title: 'Connecting to live fleet…' },
  ])
})

test('proven substrates and SSE render green with exact health meaning', () => {
  assert.deepEqual(statusBarHealth({
    problems: {}, substrateProof: { herdr: true, hcom: true }, lastEventLabel: '12:34:56 PM',
  }), [
    { label: 'herdr', healthy: true, title: 'Herdr reachable — latest fleet snapshot succeeded.' },
    { label: 'hcom', healthy: true, title: 'hcom healthy — roster and bus event subscription are reachable.' },
    { label: 'SSE', healthy: true, title: 'SSE connected — last activity 12:34:56 PM.' },
  ])
})

test('source faults beat transport loss while unobservable substrates turn red with SSE', () => {
  assert.deepEqual(statusBarHealth({
    problems: { herdr: 'socket refused', stream: 'Live stream disconnected; reconnecting…' },
    substrateProof: { herdr: true, hcom: true }, lastEventLabel: '12:34:56 PM',
  }), [
    { label: 'herdr', healthy: false, title: 'Herdr unavailable — socket refused' },
    { label: 'hcom', healthy: false, title: 'hcom health unavailable while SSE reconnects.' },
    { label: 'SSE', healthy: false, title: 'Live stream disconnected; reconnecting…' },
  ])
})
