import { mount } from 'svelte'
import { LabSession } from '../../application/usecases/session.ts'
import App from '../../presentation/App.svelte'
import { FakeAuth, FakeCalls, FakeDevices, FakeMedia, FakeTrackBinder } from './fakes.ts'
import { createSimulation } from './simulation.ts'

export function mountDevApp(target: HTMLElement): void {
  const params = new URLSearchParams(window.location.search)
  const media = new FakeMedia()
  const devices = new FakeDevices(params.get('denyDevices') === '1')
  const calls = new FakeCalls()
  const session = new LabSession({
    auth: new FakeAuth(),
    calls,
    media,
    devices,
    origin: window.location.origin,
    secureContext: window.isSecureContext,
    forceRelay: params.get('relay') === '1',
  })
  mount(App, {
    target,
    props: {
      session,
      preview: devices,
      binder: new FakeTrackBinder(media),
      simulated: true,
      simulation: createSimulation(session, media, calls, devices),
    },
  })
}
