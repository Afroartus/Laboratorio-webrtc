import { mount } from 'svelte'
import App from '../presentation/App.svelte'

/**
 * Punto de composición del frontend: aquí se ensamblarán gateways HTTP,
 * adaptadores de medios/dispositivos, casos de uso y presenters, y se
 * entregarán a la presentación. Ninguna otra capa construye dependencias.
 */
export function bootstrap(target: HTMLElement) {
  return mount(App, { target })
}
