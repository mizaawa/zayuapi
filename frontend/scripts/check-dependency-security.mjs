import assert from 'node:assert/strict'
import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)
const fromTailwind = createRequire(require.resolve('tailwindcss'))
const fromGlob = createRequire(fromTailwind.resolve('fast-glob'))
const fromMatcher = createRequire(fromGlob.resolve('micromatch'))
const braces = fromMatcher('braces')

assert.deepEqual(braces.expand('src/{views,components}/*.{ts,vue}'), [
  'src/views/*.ts', 'src/views/*.vue', 'src/components/*.ts', 'src/components/*.vue'
])
assert.equal(braces.compile('a/{b,c}/d'), 'a/(b|c)/d')
assert.equal(braces.stringify(braces.parse('a/{b,c}/d')), 'a/{b,c}/d')
assert.doesNotThrow(() => braces.compile('\\{'.repeat(1000)))

const nested = '{'.repeat(4000) + 'a' + '}'.repeat(4000)
for (const method of ['parse', 'compile', 'expand', 'stringify']) {
  assert.throws(() => braces[method](nested), {
    name: 'SyntaxError', message: 'Brace pattern nesting exceeds the depth limit'
  })
}
for (const method of ['compile', 'expand', 'stringify']) {
  let ast = { type: 'root', nodes: [] }
  for (let index = 0; index < 1000; index++) ast = { type: 'root', nodes: [ast] }
  assert.throws(() => braces[method](ast), {
    name: 'SyntaxError', message: 'Brace pattern nesting exceeds the depth limit'
  })
}
console.log('Dependency security checks passed: normal globs preserved; nested strings and ASTs bounded.')
