/**
 * XML pipeline evaluation: filter / select / map steps using pure JS.
 *
 * Documents are flat objects where field names follow the xmlUtils convention:
 *   @attrName  — XML attribute
 *   fieldName  — child element text
 *   parent.child — nested child element text (one level deep)
 */

/**
 * Evaluate an expression string against a document object.
 * `isConst` — treat as JSON literal instead of an expression.
 */
export function evaluateExpr(expr, doc, isConst) {
  if (isConst) {
    try { return JSON.parse(expr) } catch {}
    return expr
  }
  const prepared = expr
    .replace(/^\./g, 'doc.')
    .replace(/(?<![a-zA-Z0-9_$\]])\./g, 'doc.')
  // eslint-disable-next-line no-new-func
  const fn = new Function('doc', 'item', 'with(item){ return (' + prepared + ') }')
  return fn(doc, doc)
}

/** Parse a comma-separated column string into an array of names. */
export function parseColumns(raw) {
  return (raw || '').split(',').map(s => s.trim()).filter(Boolean)
}

// ── Map rules ────────────────────────────────────────────────────────────────

/**
 * Apply a single map rule to one document and return the modified copy.
 * Rule types: 'add' | 'delete' | 'rename' | 'conditional'
 */
export function applyMapRule(rule, doc) {
  if (!doc || typeof doc !== 'object') return doc
  const copy = { ...doc }

  if (rule.type === 'delete') {
    const col = rule.column.trim()
    if (!col) return doc
    delete copy[col]
    return copy
  }

  if (rule.type === 'rename') {
    const from = rule.column.trim()
    const to   = (rule.toColumn || '').trim()
    if (!from || !to) return doc
    copy[to] = copy[from]
    delete copy[from]
    return copy
  }

  if (rule.type === 'add') {
    const col = rule.column.trim()
    if (!col) return doc
    try {
      copy[col] = evaluateExpr(rule.value.trim() || 'null', doc, rule.valueType === 'const')
    } catch {
      copy[col] = null
    }
    return copy
  }

  if (rule.type === 'conditional') {
    const col = rule.column.trim()
    if (!col) return doc
    let condMet = false
    try { condMet = !!evaluateExpr(rule.condition.trim() || 'true', doc, false) } catch {}
    const valExpr = condMet ? (rule.value.trim() || 'null') : (rule.elsValue.trim() || 'null')
    try {
      copy[col] = evaluateExpr(valExpr, doc, rule.valueType === 'const')
    } catch {
      copy[col] = null
    }
    return copy
  }

  return doc
}

// ── Step application ──────────────────────────────────────────────────────────

/** Apply one pipeline step to an array of documents. */
export function applyStepJs(step, docs) {
  if (!Array.isArray(docs)) return docs

  if (step.type === 'filter') {
    const cond = (step.condition || '').trim()
    if (!cond) return docs
    let fn
    try {
      const prepared = cond
        .replace(/^\./g, 'doc.')
        .replace(/(?<![a-zA-Z0-9_$\]])\./g, 'doc.')
      // eslint-disable-next-line no-new-func
      fn = new Function('doc', 'item', 'with(item){ return !!((' + prepared + ')) }')
    } catch { return docs }
    return docs.filter(doc => { try { return fn(doc, doc) } catch { return false } })
  }

  if (step.type === 'select') {
    const cols = parseColumns(step.columnsRaw)
    if (!cols.length) return docs
    return docs.map(doc => {
      if (!doc || typeof doc !== 'object') return doc
      const out = {}
      cols.forEach(c => { out[c] = doc[c] })
      return out
    })
  }

  if (step.type === 'map') {
    const validRules = (step.rules || []).filter(r => r.column && r.column.trim())
    if (!validRules.length) return docs
    return docs.map(doc => {
      let out = doc
      for (const rule of validRules) {
        try { out = applyMapRule(rule, out) } catch {}
      }
      return out
    })
  }

  return docs
}

/**
 * Build a human-readable summary for a step (used in collapsed step headers).
 */
export function stepSummaryText(step) {
  if (step.type === 'filter') {
    const c = (step.condition || '').trim()
    if (!c) return ''
    return 'where ' + (c.length > 36 ? c.slice(0, 36) + '…' : c)
  }
  if (step.type === 'select') {
    const cols = parseColumns(step.columnsRaw)
    if (!cols.length) return ''
    const preview = cols.slice(0, 4).join(', ')
    return 'keep ' + preview + (cols.length > 4 ? ', …' : '')
  }
  if (step.type === 'map') {
    const rule = step.rules[0]
    if (!rule) return ''
    const col = (rule.column || '').trim()
    if (!col) return ''
    if (rule.type === 'delete') return 'delete ' + col
    if (rule.type === 'rename') return 'rename ' + col
    if (rule.type === 'conditional') return 'if … → ' + col
    return 'set ' + col
  }
  return ''
}
