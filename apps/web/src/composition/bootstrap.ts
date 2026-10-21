/**
 * Único punto que construye la aplicación.
 * Siempre monta el perfil simulado: ni `vite dev` ni la build abren la API ni LiveKit.
 * Los adaptadores HTTP y LiveKit siguen en el repositorio, sin que este ensamblado los importe.
 */
export async function bootstrap(target: HTMLElement): Promise<void> {
  const { mountDevApp } = await import('./dev/mount-dev.ts')
  mountDevApp(target)
}
