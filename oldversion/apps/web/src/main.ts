import { bootstrap } from './composition/bootstrap'

const target = document.getElementById('app')
if (!target) {
  throw new Error('No existe el contenedor #app en index.html')
}

void bootstrap(target).catch((error: unknown) => {
  const message = error instanceof Error ? error.message : 'No se pudo iniciar la aplicación'
  target.replaceChildren(document.createTextNode(message))
})
