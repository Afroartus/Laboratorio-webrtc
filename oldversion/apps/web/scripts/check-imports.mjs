import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..', 'src')
const errors = []

for (const file of walk(root)) {
  const source = readFileSync(file, 'utf8')
  const layer = layerOf(relative(root, file))
  if (!layer) continue
  for (const specifier of importsOf(source)) {
    const problem = forbiddenImport(layer, specifier)
    if (problem) errors.push(`${relative(root, file)}: ${problem} (${specifier})`)
  }
  if (layer === 'core' || layer === 'presentation') {
    const code = strip(source)
    if (layer === 'core') {
      for (const word of ['fetch', 'window', 'document', 'navigator']) {
        if (new RegExp(`\\b${word}\\b`).test(code)) {
          errors.push(`${relative(root, file)}: el núcleo no puede usar ${word}`)
        }
      }
    } else if (/\bfetch\s*\(/.test(code)) {
      errors.push(`${relative(root, file)}: la presentación no puede llamar a fetch`)
    }
  }
}

if (errors.length > 0) {
  console.error(errors.join('\n'))
  process.exit(1)
}

function walk(directory) {
  const files = []
  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry)
    if (statSync(path).isDirectory()) files.push(...walk(path))
    else if (/\.(ts|svelte)$/.test(entry)) files.push(path)
  }
  return files
}

function layerOf(path) {
  if (path.startsWith('domain/') || path.startsWith('application/')) return 'core'
  if (path.startsWith('presentation/')) return 'presentation'
  return ''
}

function importsOf(source) {
  const specifiers = []
  const pattern = /from\s+['"]([^'"]+)['"]|import\s*\(\s*['"]([^'"]+)['"]\s*\)|import\s+['"]([^'"]+)['"]/g
  for (const match of source.matchAll(pattern)) {
    const specifier = match[1] || match[2] || match[3]
    if (specifier) specifiers.push(specifier)
  }
  return specifiers
}

function forbiddenImport(layer, specifier) {
  const lowered = specifier.toLowerCase()
  if (layer === 'core') {
    if (lowered === 'svelte' || lowered.startsWith('svelte/') || lowered.includes('livekit-client')) {
      return 'el núcleo no puede importar Svelte ni livekit-client'
    }
    if (/(^|\/)adapters(\/|$)/.test(specifier) || specifier.includes('/adapters/')) {
      return 'el núcleo no puede importar adaptadores'
    }
    if (specifier.includes('/presentation/') || specifier.includes('/composition/')) {
      return 'el núcleo no puede importar presentación ni composición'
    }
  }
  if (layer === 'presentation') {
    if (lowered.includes('livekit-client')) return 'la presentación no puede importar livekit-client'
    if (/(^|\/)adapters(\/|$)/.test(specifier) || specifier.includes('/adapters/')) {
      return 'la presentación no puede importar adaptadores'
    }
  }
  return ''
}

function strip(source) {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|[^:])\/\/.*$/gm, '$1')
    .replace(/`(?:\\.|[^`\\])*`/g, '``')
    .replace(/'(?:\\.|[^'\\])*'/g, "''")
    .replace(/"(?:\\.|[^"\\])*"/g, '""')
}
