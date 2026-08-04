import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'

const ROOT = join(dirname(new URL(import.meta.url).pathname), '..')
const REFERENCE_EXPORT = join(ROOT, '.ai/shortcut_source_tool.json')

const RENDERER_ACTION = 246
const PAGE_SHELL_ACTION = 254

const actions = JSON.parse(readFileSync(REFERENCE_EXPORT, 'utf8')).WFWorkflowActions

const rendererProgram = actions[RENDERER_ACTION].WFWorkflowActionParameters.WFTextActionText
if (typeof rendererProgram !== 'string') {
  throw new Error(`action ${RENDERER_ACTION} is not a plain text action; the reference export changed`)
}

function sliceObjectLiteral(source, constName) {
  const declaration = source.indexOf(`const ${constName}`)
  if (declaration < 0) throw new Error(`${constName} not found in the renderer program`)
  const open = source.indexOf('{', declaration)
  if (open < 0) throw new Error(`${constName} has no object literal`)

  let depth = 0
  let quote = null
  for (let i = open; i < source.length; i++) {
    const char = source[i]
    if (quote) {
      if (char === '\\') i++
      else if (char === quote) quote = null
      continue
    }
    if (char === '"' || char === "'" || char === '`') {
      quote = char
      continue
    }
    if (char === '/' && source[i + 1] === '/') {
      i = source.indexOf('\n', i)
      if (i < 0) break
      continue
    }
    if (char === '{') depth++
    else if (char === '}' && --depth === 0) return source.slice(open, i + 1)
  }
  throw new Error(`unterminated object literal for ${constName}`)
}

const readTable = (constName) =>
  new Function(`return (${sliceObjectLiteral(rendererProgram, constName)});`)()

const idDict = readTable('idDict')
const condDict = readTable('condDict')
const filterDict = readTable('filterDict')
const unitDict = readTable('unitDict')

let stateCount = 0
for (const [identifier, states] of Object.entries(idDict)) {
  if (!Array.isArray(states)) throw new Error(`${identifier}: expected an array of states`)
  if (states.length < 1 || states.length > 3) throw new Error(`${identifier}: ${states.length} states`)
  for (const state of states) {
    if (typeof state !== 'object' || state === null) throw new Error(`${identifier}: non-object state`)
    if (Object.keys(state).length > 0 && typeof state.name !== 'string') {
      throw new Error(`${identifier}: state without a name`)
    }
    stateCount++
  }
}

const groupCounts = {}
for (const states of Object.values(idDict)) {
  for (const state of states) {
    if (state.group) groupCounts[state.group] = (groupCounts[state.group] ?? 0) + 1
  }
}

const sortKeys = (o) => Object.fromEntries(Object.keys(o).sort().map((k) => [k, o[k]]))

function write(relativePath, data) {
  const target = join(ROOT, relativePath)
  mkdirSync(dirname(target), { recursive: true })
  writeFileSync(target, typeof data === 'string' ? data : JSON.stringify(data, null, 2) + '\n')
}

write('internal/catalog/data/actions.json', sortKeys(idDict))
write('internal/catalog/data/conditions.json', condDict)
write('internal/catalog/data/filters.json', sortKeys(filterDict))
write('internal/catalog/data/units.json', unitDict)

const pageShell = actions[PAGE_SHELL_ACTION].WFWorkflowActionParameters.WFTextActionText?.Value?.string
if (typeof pageShell !== 'string') {
  throw new Error(`action ${PAGE_SHELL_ACTION} is not a token text action; the reference export changed`)
}
const styleBlock = pageShell.match(/<style>([\s\S]*?)<\/style>/)
if (!styleBlock) throw new Error('no <style> block in the page shell')
write('internal/render/assets/shortcut-source-tool.css', styleBlock[1].trim() + '\n')

const identifiers = Object.keys(idDict)
const isAppleIntent = (k) => k.startsWith('com.apple.')
const isLegacyAction = (k) => k.startsWith('is.workflow.actions.')

console.log(`actions.json     ${identifiers.length} identifiers, ${stateCount} control-flow states`)
console.log(`  apple intents  ${identifiers.filter(isAppleIntent).length}`)
console.log(`  legacy WF      ${identifiers.filter(isLegacyAction).length}`)
console.log(`  third party    ${identifiers.filter((k) => !isAppleIntent(k) && !isLegacyAction(k)).length}`)
console.log(`  colour groups  ${JSON.stringify(groupCounts)}`)
console.log(`conditions.json  ${Object.keys(condDict).length} operators`)
console.log(`filters.json     ${Object.keys(filterDict).length} filter properties`)
console.log(`units.json       ${Object.keys(unitDict).length} unit enumerations`)
console.log(`css              ${styleBlock[1].trim().length} bytes`)
