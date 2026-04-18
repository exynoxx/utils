/**
 * Pure JS fallback pipeline evaluator — mirrors jq semantics for
 * filter / select / map steps without requiring jq-web at runtime.
 */

/**
 * Evaluates a jq-style expression against `item`.
 * `isConst` = treat as JSON literal instead of an expression.
 */
export function evaluateExpr(expr, item, isConst) {
  if (isConst) {
    try { return JSON.parse(expr) } catch {}
    return expr
  }
  // Replace leading `.` with `item.` and bare `.key` references
  const fn = new Function(
    'item',
    'with(item){return(' +
      expr
        .replace(/^\./g, 'item.')
        .replace(/(?<![a-zA-Z0-9_$\]])\./g, 'item.') +
      ')}'
  )
  return fn(item)
}

/**
 * Converts a rule descriptor to a jq fragment string.
 * Returns null when the rule is incomplete.
 */
export function ruleToJq(rule) {
  if (rule.type === 'delete') {
    const col = rule.column.trim()
    if (!col) return null
    return 'del(.' + col + ')'
  }
  if (rule.type === 'add') {
    const col = rule.column.trim()
    if (!col) return null
    const val = rule.value.trim() || 'null'
    return '. + {' + JSON.stringify(col) + ': (' + val + ')}'
  }
  if (rule.type === 'conditional') {
    const col = rule.column.trim()
    const cond = rule.condition.trim() || 'true'
    const val = rule.value.trim() || 'null'
    const els = rule.elsValue.trim() !== '' ? rule.elsValue.trim() : 'null'
    if (!col) return null
    return '. + {' + JSON.stringify(col) + ': (if (' + cond + ') then (' + val + ') else (' + els + ') end)}'
  }
  return null
}

/** Apply a single map rule to one item using pure JS. */
export function applyRuleJs(rule, item) {
  if (item == null || typeof item !== 'object') return item
  if (rule.type === 'delete') {
    const col = rule.column.trim()
    if (!col) return item
    const copy = { ...item }
    delete copy[col]
    return copy
  }
  if (rule.type === 'add') {
    const col = rule.column.trim()
    if (!col) return item
    let val
    try { val = evaluateExpr(rule.value.trim() || 'null', item, rule.valueType === 'const') } catch { val = null }
    return { ...item, [col]: val }
  }
  if (rule.type === 'conditional') {
    const col = rule.column.trim()
    if (!col) return item
    let condMet = false
    try { condMet = !!evaluateExpr(rule.condition.trim() || 'true', item, false) } catch {}
    let val
    try {
      val = evaluateExpr(
        condMet ? (rule.value.trim() || 'null') : (rule.elsValue.trim() || 'null'),
        item,
        rule.valueType === 'const' && condMet
      )
    } catch { val = null }
    return { ...item, [col]: val }
  }
  return item
}

/** Parse a comma-separated column string into an array of names. */
export function parseColumns(raw) {
  return (raw || '').split(',').map(s => s.trim()).filter(Boolean)
}

/** Build the jq fragment for a single pipeline step. */
export function stepToJq(step) {
  if (step.type === 'filter') {
    const c = step.condition.trim()
    if (!c) return ''
    return 'map(select(' + c + '))'
  }
  if (step.type === 'select') {
    const cols = parseColumns(step.columnsRaw)
    if (!cols.length) return ''
    if (step.valuesOnly && cols.length === 1) return 'map(.' + cols[0] + ')'
    return 'map({' + cols.map(c => JSON.stringify(c) + ': .' + c).join(', ') + '})'
  }
  if (step.type === 'map') {
    const valid = step.rules.map(ruleToJq).filter(Boolean)
    if (!valid.length) return ''
    return 'map(' + valid.join(' | ') + ')'
  }
  return ''
}

/** Apply one pipeline step to an array using pure JS. */
export function applyStepJs(step, arr) {
  if (!Array.isArray(arr)) return arr
  if (step.type === 'filter') {
    const cond = step.condition.trim()
    if (!cond) return arr
    return arr.filter(item => {
      try {
        const fn = new Function(
          'item',
          'with(item){return !!((' +
            cond.replace(/^\./g, 'item.').replace(/(?<![a-zA-Z0-9_$\]])\./g, 'item.') +
          '))}'
        )
        return fn(item)
      } catch { return false }
    })
  }
  if (step.type === 'select') {
    const cols = parseColumns(step.columnsRaw)
    if (!cols.length) return arr
    if (step.valuesOnly && cols.length === 1) {
      return arr.map(item => (item == null || typeof item !== 'object') ? item : item[cols[0]])
    }
    return arr.map(item => {
      if (item == null || typeof item !== 'object') return item
      const out = {}
      cols.forEach(c => { out[c] = item[c] })
      return out
    })
  }
  if (step.type === 'map') {
    const validRules = step.rules.filter(r => ruleToJq(r) !== null)
    return arr.map(item => {
      let out = item
      for (const rule of validRules) { try { out = applyRuleJs(rule, out) } catch {} }
      return out
    })
  }
  return arr
}
