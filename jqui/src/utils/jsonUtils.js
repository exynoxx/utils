/**
 * Evaluates a simple dot-path jq-style filter against a JS object.
 * Handles: `.foo`, `.foo.bar`, `.foo[0]`, `.foo["key"]`
 * Does NOT handle complex jq expressions — use jq-web for those.
 */
export function evalSimpleJqPath(filter, data) {
  let current = data
  if (filter === '.' || filter === '') return current
  const stripped = filter.startsWith('.') ? filter.slice(1) : filter
  if (!stripped) return current
  const tokens = []
  let tok = ''
  for (let i = 0; i < stripped.length; i++) {
    const ch = stripped[i]
    if (ch === '[') {
      if (tok) { tokens.push({ type: 'key', val: tok }); tok = '' }
      let idx = ''; i++
      while (i < stripped.length && stripped[i] !== ']') idx += stripped[i++]
      tokens.push({ type: 'index', val: isNaN(idx) ? idx.replace(/^["']|["']$/g, '') : parseInt(idx, 10) })
    } else if (ch === '.') {
      if (tok) { tokens.push({ type: 'key', val: tok }); tok = '' }
    } else {
      tok += ch
    }
  }
  if (tok) tokens.push({ type: 'key', val: tok })
  for (const t of tokens) {
    if (current == null) return null
    current = current[t.val]
  }
  return current
}

/**
 * Trigger a browser download of `data` as a pretty-printed JSON file.
 */
export function downloadJson(data, filename) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = filename
  a.click()
  URL.revokeObjectURL(a.href)
}
