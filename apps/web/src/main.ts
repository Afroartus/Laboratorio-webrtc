import { bootstrap } from './composition/bootstrap'

const target = document.getElementById('app')
if (!target) {
  throw new Error('No existe el contenedor #app en index.html')
}

bootstrap(target)
