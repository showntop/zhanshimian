import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const app = JSON.parse(fs.readFileSync(path.join(root, 'app.json'), 'utf8'))
const errors = []

function exists(relative) {
  return fs.existsSync(path.join(root, relative.replace(/^\//, '')))
}

for (const page of app.pages) {
  for (const extension of ['.js', '.json', '.wxml', '.wxss']) {
    if (!exists(page + extension)) errors.push(`missing ${page + extension}`)
  }
  const pageJSON = JSON.parse(fs.readFileSync(path.join(root, page + '.json'), 'utf8'))
  for (const component of Object.values(pageJSON.usingComponents || {})) {
    for (const extension of ['.js', '.json', '.wxml', '.wxss']) {
      if (!exists(component + extension)) errors.push(`missing component ${component + extension}`)
    }
  }
  const wxml = fs.readFileSync(path.join(root, page + '.wxml'), 'utf8')
  if (/\.includes\(|\.map\(|\.filter\(/.test(wxml)) errors.push(`unsupported method call in ${page}.wxml`)
  for (const match of wxml.matchAll(/src="(\/assets\/[^"]+)"/g)) {
    if (!exists(match[1])) errors.push(`missing local asset ${match[1]} in ${page}.wxml`)
  }
}

if (errors.length) {
  console.error(errors.join('\n'))
  process.exit(1)
}
console.log(`miniapp validation passed: ${app.pages.length} pages`)
