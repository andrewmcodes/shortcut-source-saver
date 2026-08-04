// Regenerates the action catalog and page CSS from gluebyte's "Shortcut Source Tool"
// (RoutineHub #5256), whose export lives at .ai/shortcut_source_tool.json.
//
// That shortcut embeds its entire web-review renderer as a ~123KB JavaScript program
// inside a single Text action. The first ~100KB of that program is four lookup tables
// that map Shortcuts action identifiers to human-readable names, colour groups, and
// parameter slots. There is no public API for this data and macOS ships no equivalent
// catalog, so we lift the tables once and commit the result.
//
// The reference JSON is gitignored, so this script only runs on a machine that has it.
// Its output (internal/catalog/data/*.json, internal/render/assets/style.css) is
// committed, which is what the build actually depends on.
//
// Usage: mise run extract-catalog

import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'

const ROOT = join(dirname(new URL(import.meta.url).pathname), '..')
const SRC = join(ROOT, '.ai/shortcut_source_tool.json')

const RENDERER_ACTION = 246 // Text action holding the JS renderer
const SHELL_ACTION = 254 // Text action holding the HTML/CSS page shell

const actions = JSON.parse(readFileSync(SRC, 'utf8')).WFWorkflowActions
const program = actions[RENDERER_ACTION].WFWorkflowActionParameters.WFTextActionText
if (typeof program !== 'string') {
  throw new Error(`action ${RENDERER_ACTION} is not a plain text action; the reference file changed`)
}

// Walk forward from `const <name> = {` to its matching close brace, skipping over
// string literals so that braces inside values cannot throw off the depth count.
function sliceObjectLiteral(src, name) {
  const start = src.indexOf(`const ${name}`)
  if (start < 0) throw new Error(`${name} not found in renderer program`)
  const open = src.indexOf('{', start)
  if (open < 0) throw new Error(`${name} has no object literal`)

  let depth = 0
  let quote = null
  for (let i = open; i < src.length; i++) {
    const c = src[i]
    if (quote) {
      if (c === '\\') i++
      else if (c === quote) quote = null
      continue
    }
    if (c === '"' || c === "'" || c === '`') { quote = c; continue }
    if (c === '/' && src[i + 1] === '/') { i = src.indexOf('\n', i); if (i < 0) break; continue }
    if (c === '{') depth++
    else if (c === '}' && --depth === 0) return src.slice(open, i + 1)
  }
  throw new Error(`unterminated object literal for ${name}`)
}

// The tables are JS object literals with `// <version>` trailing comments, so they are
// not valid JSON. Evaluating is safe here: the input is a vendored constant we read,
// not untrusted data, and it contains only literals.
const evalLiteral = (name) => new Function(`return (${sliceObjectLiteral(program, name)});`)()

const idDict = evalLiteral('idDict')
const condDict = evalLiteral('condDict')
const filterDict = evalLiteral('filterDict')
const unitDict = evalLiteral('unitDict')

// Sanity-check the shape before writing: every value must be an array of control-flow
// states, and every state either empty or carrying a name.
let states = 0
for (const [id, entry] of Object.entries(idDict)) {
  if (!Array.isArray(entry)) throw new Error(`${id}: expected an array of states`)
  if (entry.length < 1 || entry.length > 3) throw new Error(`${id}: ${entry.length} states`)
  for (const s of entry) {
    if (typeof s !== 'object' || s === null) throw new Error(`${id}: non-object state`)
    if (Object.keys(s).length > 0 && typeof s.name !== 'string') throw new Error(`${id}: state without a name`)
    states++
  }
}

const groups = {}
for (const entry of Object.values(idDict)) {
  for (const s of entry) if (s.group) groups[s.group] = (groups[s.group] ?? 0) + 1
}

const write = (relPath, data) => {
  const p = join(ROOT, relPath)
  mkdirSync(dirname(p), { recursive: true })
  writeFileSync(p, typeof data === 'string' ? data : JSON.stringify(data, null, 2) + '\n')
  return p
}

write('internal/catalog/data/actions.json', sortKeys(idDict))
write('internal/catalog/data/conditions.json', condDict)
write('internal/catalog/data/filters.json', sortKeys(filterDict))
write('internal/catalog/data/units.json', unitDict)

// Lift the stylesheet out of the page shell so the generated site can ship real CSS
// rather than an inline <style> blob.
const shell = actions[SHELL_ACTION].WFWorkflowActionParameters.WFTextActionText?.Value?.string
if (typeof shell !== 'string') throw new Error(`action ${SHELL_ACTION} is not a token text action`)
const css = shell.match(/<style>([\s\S]*?)<\/style>/)
if (!css) throw new Error('no <style> block in the page shell')
write('internal/render/assets/shortcut-source-tool.css', css[1].trim() + '\n')

function sortKeys(o) {
  return Object.fromEntries(Object.keys(o).sort().map((k) => [k, o[k]]))
}

const ids = Object.keys(idDict)
console.log(`actions.json     ${ids.length} identifiers, ${states} control-flow states`)
console.log(`  apple intents  ${ids.filter((k) => k.startsWith('com.apple.')).length}`)
console.log(`  legacy WF      ${ids.filter((k) => k.startsWith('is.workflow.actions.')).length}`)
console.log(`  third party    ${ids.filter((k) => !/^(com\.apple\.|is\.workflow\.actions\.)/.test(k)).length}`)
console.log(`  colour groups  ${JSON.stringify(groups)}`)
console.log(`conditions.json  ${Object.keys(condDict).length} operators`)
console.log(`filters.json     ${Object.keys(filterDict).length} filter properties`)
console.log(`units.json       ${Object.keys(unitDict).length} unit enumerations`)
console.log(`css              ${css[1].trim().length} bytes`)
