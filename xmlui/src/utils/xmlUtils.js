/**
 * XML utilities: parsing, path evaluation, serialization, and download.
 *
 * Document model: each XML element is flattened to a plain object where
 *   - attributes become  @attrName keys
 *   - child element text becomes  fieldName keys
 *   - nested child elements become  parent.child keys (one level deep)
 */

// ── Parsing ──────────────────────────────────────────────────────────────────

/**
 * Parse an XML string into a plain tree: { tag, attrs, children, text }
 * Throws an Error with the parser message on invalid XML.
 */
export function parseXMLText(text) {
  const parser = new DOMParser()
  const doc = parser.parseFromString(text, 'text/xml')
  const err = doc.querySelector('parsererror')
  if (err) {
    const msg = err.textContent.replace(/\s+/g, ' ').trim().slice(0, 300)
    throw new Error(msg)
  }
  return domToTree(doc.documentElement)
}

/**
 * Recursively convert a DOM Element to a plain tree node.
 */
export function domToTree(el) {
  const node = { tag: el.tagName, attrs: {}, children: [], text: '' }
  for (const attr of el.attributes) {
    node.attrs[attr.name] = attr.value
  }
  for (const child of el.childNodes) {
    if (child.nodeType === Node.TEXT_NODE) {
      const t = child.textContent.trim()
      if (t) node.text += t
    } else if (child.nodeType === Node.ELEMENT_NODE) {
      node.children.push(domToTree(child))
    }
  }
  return node
}

// ── Path evaluation ───────────────────────────────────────────────────────────

/**
 * Walk a tree by a dot-notation path and return all matching elements.
 * Path examples: '.items.item', 'root.items.item', '.item'
 * The root element itself is NOT included in the path — e.g. for a tree
 * rooted at <root>, path '.items.item' navigates root→items→[item elements].
 */
export function evalXPath(tree, pathExpr) {
  if (!pathExpr || !tree) return []
  const segments = pathExpr.replace(/^\./, '').split('.').filter(Boolean)
  if (!segments.length) return [tree]

  function walk(node, segs) {
    if (!segs.length) return [node]
    const [head, ...rest] = segs
    if (!node.children) return []
    const matches = node.children.filter(c => c.tag === head)
    if (!rest.length) return matches
    return matches.flatMap(m => walk(m, rest))
  }

  return walk(tree, segments)
}

/**
 * Scan a tree and return dot-notation path suggestions for repeating elements.
 * Returns strings like '.items.item', '.records.record', etc.
 */
export function getArrayPathSuggestions(tree) {
  if (!tree) return []
  const suggestions = []

  function walk(node, pathSoFar) {
    if (!node.children || !node.children.length) return
    const tagCount = {}
    for (const child of node.children) {
      tagCount[child.tag] = (tagCount[child.tag] || 0) + 1
    }
    for (const [tag, count] of Object.entries(tagCount)) {
      if (count > 1) {
        const p = pathSoFar ? `${pathSoFar}.${tag}` : `.${tag}`
        suggestions.push(p)
      }
    }
    const seen = new Set()
    for (const child of node.children) {
      if (!seen.has(child.tag)) {
        seen.add(child.tag)
        const p = pathSoFar ? `${pathSoFar}.${child.tag}` : child.tag
        walk(child, p)
      }
    }
  }

  // Walk starting from root's direct children (root tag not part of the path)
  walk(tree, '')
  return [...new Set(suggestions)]
}

// ── Flat doc extraction ───────────────────────────────────────────────────────

/**
 * Flatten a single XML element node into a plain object.
 *   - Attributes → @attrName
 *   - Child element text → fieldName
 *   - Nested child elements (one level deep) → parent.child
 */
export function elementToDoc(el) {
  const doc = {}
  for (const [k, v] of Object.entries(el.attrs || {})) {
    doc['@' + k] = v
  }
  for (const child of el.children || []) {
    if (child.children && child.children.length > 0) {
      // Flatten one level of nesting
      for (const grandchild of child.children) {
        if (!grandchild.children || grandchild.children.length === 0) {
          doc[child.tag + '.' + grandchild.tag] = grandchild.text || ''
        }
      }
      if (child.text) doc[child.tag] = child.text
    } else {
      doc[child.tag] = child.text || ''
    }
  }
  return doc
}

// ── Doc → Element ─────────────────────────────────────────────────────────────

/**
 * Convert a flat doc object back to a plain tree element node.
 * @attrName keys become attributes; other keys become child elements.
 * Dot-notation keys (parent.child) create nested elements.
 * The _id internal field is ignored.
 */
export function docToElement(doc, tag) {
  const attrs = {}
  const childMap = {}      // parent tag → {tag, attrs, children, text}

  for (const [key, val] of Object.entries(doc)) {
    if (key === '_id') continue
    const strVal = val == null ? '' : String(val)

    if (key.startsWith('@')) {
      attrs[key.slice(1)] = strVal
    } else if (key.includes('.')) {
      const dotIdx = key.indexOf('.')
      const parentTag = key.slice(0, dotIdx)
      const childTag  = key.slice(dotIdx + 1)
      if (!childMap[parentTag]) {
        childMap[parentTag] = { tag: parentTag, attrs: {}, children: [], text: '' }
      }
      childMap[parentTag].children.push({ tag: childTag, attrs: {}, children: [], text: strVal })
    } else {
      childMap[key] = { tag: key, attrs: {}, children: [], text: strVal }
    }
  }

  return { tag, attrs, children: Object.values(childMap), text: '' }
}

// ── Serialization ─────────────────────────────────────────────────────────────

function escXml(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/**
 * Serialize a plain tree node back to an indented XML string.
 */
export function treeToXml(node, level = 0) {
  const sp = '  '.repeat(level)
  const attrsStr = Object.entries(node.attrs || {})
    .map(([k, v]) => ` ${k}="${escXml(v)}"`)
    .join('')
  const { tag } = node

  if (!node.children.length && !node.text) {
    return `${sp}<${tag}${attrsStr}/>`
  }
  if (!node.children.length) {
    return `${sp}<${tag}${attrsStr}>${escXml(node.text)}</${tag}>`
  }
  const inner = node.children.map(c => treeToXml(c, level + 1)).join('\n')
  return `${sp}<${tag}${attrsStr}>\n${inner}\n${sp}</${tag}>`
}

// ── Tree rebuilding ───────────────────────────────────────────────────────────

function deepClone(node) {
  return {
    tag: node.tag,
    attrs: { ...node.attrs },
    text: node.text,
    children: node.children.map(deepClone),
  }
}

/**
 * Deep-clone the original tree and replace the elements at `pathExpr` with
 * `newDocs` converted to elements using `docTag` as their tag name.
 *
 * Example: pathExpr='.items.item', docTag='item'
 *   → navigates to the <items> node and replaces all <item> children.
 */
export function rebuildTree(originalTree, pathExpr, newDocs, docTag) {
  if (!originalTree || !pathExpr) return originalTree
  const segments = pathExpr.replace(/^\./, '').split('.').filter(Boolean)
  if (!segments.length) return originalTree

  const tree = deepClone(originalTree)
  const parentSegs = segments.slice(0, -1)

  function findParent(node, segs) {
    if (!segs.length) return node
    const [head, ...rest] = segs
    const child = node.children.find(c => c.tag === head)
    if (!child) return null
    return findParent(child, rest)
  }

  const parent = parentSegs.length ? findParent(tree, parentSegs) : tree
  if (!parent) return tree

  const otherChildren = parent.children.filter(c => c.tag !== docTag)
  const newElements   = newDocs.map(doc => docToElement(doc, docTag))
  parent.children = [...otherChildren, ...newElements]

  return tree
}

// ── Download ──────────────────────────────────────────────────────────────────

/**
 * Trigger a browser download of an XML string.
 */
export function downloadXml(xmlString, filename) {
  const blob = new Blob([xmlString], { type: 'application/xml' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = filename
  a.click()
  URL.revokeObjectURL(a.href)
}
