import { ref, shallowRef, computed, watch, onScopeDispose } from 'vue'
import {
  DOCS_PER_PAGE, LARGE_FILE_ROWS, MAX_FIELD_SAMPLE,
  FILTER_CHUNK_SIZE, SEARCH_DEBOUNCE_MS, SEARCH_MIN_CHARS,
} from '@/constants'
import { getArrayPathSuggestions, evalXPath, elementToDoc } from '@/utils/xmlUtils'
import { applyStepJs, stepSummaryText, parseColumns } from '@/utils/pipelineEval'

let stepIdCounter = 0
let ruleIdCounter = 0

function newRule() {
  return {
    id: ++ruleIdCounter,
    type: 'add',
    column: '',
    toColumn: '',
    valueType: 'expr',
    value: '',
    condition: '',
    elsValue: 'null',
  }
}

/**
 * All XML pipeline state: array path, pipeline steps, document ordering,
 * inline edits, exclusions, selection, and drag-and-drop for both
 * step reordering and document row reordering.
 *
 * PERFORMANCE: Documents are extracted lazily — only the 50 docs visible on
 * the current page are materialised via elementToDoc(). A Map cache ensures
 * each doc is materialised at most once until the data changes.
 */
export function usePipeline(parsedData) {
  // ── Core pipeline state ────────────────────────────────────────────────────
  const arrayPath    = ref('')
  const pipelineSteps = ref([])
  const activeStepId  = ref(null)
  const pipelineEvalFlash = ref(false)
  const currentPage   = ref(1)
  const viewerSearch  = ref('')

  // ── Document state ─────────────────────────────────────────────────────────
  const excludedIds  = ref(new Set())
  const selectedIds  = ref(new Set())
  const lastSelectedId = ref(null)
  const docOrder     = ref(null)
  const docEdits     = ref({})
  const hiddenColumns = ref(new Set())
  const columnOrder   = ref(null)
  const sortConfig   = ref(null)

  // ── Step drag state ────────────────────────────────────────────────────────
  const stepDragSrcIndex    = ref(null)
  const stepDropInsertIndex = ref(null)
  const draggableStepIndex  = ref(null)

  // ── Doc row drag state ─────────────────────────────────────────────────────
  const docDragSrcIds      = ref([])
  const docDropInsertIndex = ref(null)
  const draggableDocId     = ref(null)

  // ── Async filter state ─────────────────────────────────────────────────────
  const filterPassMap        = shallowRef({})
  const filterComputeProgress = ref(null)   // null | 0-100
  let _filterCancelToken     = 0

  let _evalFlashTimer   = null
  let _debounceTimer    = null
  let _searchTimer      = null

  onScopeDispose(() => {
    clearTimeout(_evalFlashTimer)
    clearTimeout(_debounceTimer)
    clearTimeout(_searchTimer)
    ++_filterCancelToken
  })

  // ═══════════════════════════════════════════════════════════════════════════
  // PHASE 1 — Lazy Document Extraction
  // ═══════════════════════════════════════════════════════════════════════════

  /** Lightweight element references from evalXPath (no doc materialisation). */
  const baseElements = computed(() => {
    const ap = arrayPath.value.trim()
    if (!ap || !parsedData.value) return []
    return evalXPath(parsedData.value, ap)
  })

  const docCount = computed(() => baseElements.value.length)

  /** Whether dataset is large enough to gate expensive ops. */
  const isLargeFile = computed(() => docCount.value > LARGE_FILE_ROWS)

  // ── Doc cache ──────────────────────────────────────────────────────────────
  let _docCache = new Map()

  /** Lazily materialise a doc by ID (with cache). */
  function getDoc(id) {
    if (_docCache.has(id)) return _docCache.get(id)
    const el = baseElements.value[id]
    if (!el) return null
    const doc = { _id: id, ...elementToDoc(el) }
    _docCache.set(id, doc)
    return doc
  }

  /** The last segment of arrayPath — used as the XML element tag for output. */
  const docTag = computed(() => {
    const ap = arrayPath.value.trim()
    if (!ap) return 'item'
    const segs = ap.replace(/^\./, '').split('.').filter(Boolean)
    return segs[segs.length - 1] || 'item'
  })

  // ── Array path suggestions ─────────────────────────────────────────────────
  const arrayPathSuggestions = computed(() => getArrayPathSuggestions(parsedData.value))

  // ── Available fields ───────────────────────────────────────────────────────
  const availableFields = computed(() => {
    const n = Math.min(docCount.value, MAX_FIELD_SAMPLE)
    const fieldSet = new Set()
    for (let i = 0; i < n; i++) {
      const doc = getDoc(i)
      if (!doc) continue
      for (const key of Object.keys(doc)) {
        if (key !== '_id') fieldSet.add(key)
      }
    }
    return Array.from(fieldSet)
  })

  /** Columns in user's preferred order (custom if set, else natural). */
  const orderedColumns = computed(() => {
    if (columnOrder.value) {
      const orderSet = new Set(columnOrder.value)
      const existing = columnOrder.value.filter(f => availableFields.value.includes(f))
      const added    = availableFields.value.filter(f => !orderSet.has(f))
      return [...existing, ...added]
    }
    return availableFields.value
  })

  /** Columns visible in the viewer (all available minus hidden). */
  const visibleColumns = computed(() => {
    return orderedColumns.value.filter(f => !hiddenColumns.value.has(f))
  })

  // ── Ordering ───────────────────────────────────────────────────────────────

  /** Doc IDs in their current display order. */
  const orderedIds = computed(() => {
    const n = docCount.value
    if (!n) return []

    // Default: 0, 1, 2, … n-1  (no doc access needed)
    let ids = Array.from({ length: n }, (_, i) => i)

    if (docOrder.value) {
      const idSet = new Set(ids)
      const orderSet = new Set(docOrder.value)
      const existing = docOrder.value.filter(id => idSet.has(id))
      const added    = ids.filter(id => !orderSet.has(id))
      ids = [...existing, ...added]
    } else if (sortConfig.value) {
      const { field, dir } = sortConfig.value
      ids = ids.slice().sort((a, b) => {
        const va = getDoc(a)?.[field] ?? ''
        const vb = getDoc(b)?.[field] ?? ''
        const cmp = String(va).localeCompare(String(vb), undefined, { numeric: true })
        return dir === 'asc' ? cmp : -cmp
      })
    }

    return ids
  })

  // ── Docs with edits applied ────────────────────────────────────────────────

  function applyEdits(doc) {
    if (!doc) return doc
    const edits = docEdits.value[doc._id]
    return edits ? { ...doc, ...edits } : doc
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // PHASE 2 — Async Chunked Filter
  // ═══════════════════════════════════════════════════════════════════════════

  /** Pre-compile filter functions once per condition string. */
  function _compileFilterFns() {
    const filterSteps = pipelineSteps.value.filter(s => s.type === 'filter')
    const fns = []
    for (const step of filterSteps) {
      const cond = (step.condition || '').trim()
      if (!cond) continue
      try {
        const prepared = cond
          .replace(/^\./g, 'doc.')
          .replace(/(?<![a-zA-Z0-9_$\]])\./g, 'doc.')
        // eslint-disable-next-line no-new-func
        fns.push(new Function('doc', 'item', 'with(item){ return !!((' + prepared + ')) }'))
      } catch {
        // if compilation fails, push a function that always returns false
        fns.push(() => false)
      }
    }
    return fns
  }

  function _evalDocFilter(doc, fns) {
    const d = applyEdits(doc)
    for (const fn of fns) {
      try { if (!fn(d, d)) return false } catch { return false }
    }
    return true
  }

  function _runFilterComputation() {
    const token = ++_filterCancelToken
    const n = docCount.value
    const fns = _compileFilterFns()

    // No filter steps → all pass
    if (!fns.length) {
      filterPassMap.value = {}
      filterComputeProgress.value = null
      return
    }

    // Small files → synchronous
    if (n <= LARGE_FILE_ROWS) {
      const passMap = {}
      for (let i = 0; i < n; i++) {
        passMap[i] = _evalDocFilter(getDoc(i), fns)
      }
      filterPassMap.value = passMap
      filterComputeProgress.value = null
      return
    }

    // Large files → chunked async
    filterComputeProgress.value = 0
    const passMap = {}
    let cursor = 0

    function processChunk() {
      if (token !== _filterCancelToken) return  // stale
      const end = Math.min(cursor + FILTER_CHUNK_SIZE, n)
      for (let i = cursor; i < end; i++) {
        passMap[i] = _evalDocFilter(getDoc(i), fns)
      }
      cursor = end
      filterComputeProgress.value = Math.round((cursor / n) * 100)
      if (cursor < n) {
        setTimeout(processChunk, 0)
      } else {
        filterPassMap.value = passMap
        filterComputeProgress.value = null
      }
    }

    processChunk()
  }

  // Trigger filter recomputation when steps or data change
  watch(
    [() => pipelineSteps.value.filter(s => s.type === 'filter').map(s => s.condition).join('\0'), docCount],
    () => { _runFilterComputation() },
    { immediate: true }
  )

  // Also recompute when docEdits change (edits affect filter results)
  watch(docEdits, () => { _runFilterComputation() }, { deep: true })

  // ═══════════════════════════════════════════════════════════════════════════
  // PHASE 3 — Debounced Search
  // ═══════════════════════════════════════════════════════════════════════════

  const _debouncedSearch = ref('')

  watch(viewerSearch, (q) => {
    clearTimeout(_searchTimer)
    const trimmed = q.trim()
    if (!trimmed) {
      _debouncedSearch.value = ''
      return
    }
    _searchTimer = setTimeout(() => {
      _debouncedSearch.value = trimmed
    }, SEARCH_DEBOUNCE_MS)
  })

  const filteredIds = computed(() => {
    const q = _debouncedSearch.value.toLowerCase()

    // For large files, require minimum search chars
    if (!q || (isLargeFile.value && q.length < SEARCH_MIN_CHARS)) {
      return orderedIds.value
    }

    return orderedIds.value.filter(id => {
      const doc = getDoc(id)
      if (!doc) return false
      const editsDoc = applyEdits(doc)
      return Object.values(editsDoc).some(v =>
        v != null && String(v).toLowerCase().includes(q)
      )
    })
  })

  // ── Current page docs ──────────────────────────────────────────────────────

  const totalDocs  = computed(() => filteredIds.value.length)
  const totalPages = computed(() => Math.max(1, Math.ceil(totalDocs.value / DOCS_PER_PAGE)))

  const pageDocIds = computed(() => {
    const start = (currentPage.value - 1) * DOCS_PER_PAGE
    return filteredIds.value.slice(start, start + DOCS_PER_PAGE)
  })

  /** Enriched page documents with view-layer metadata. */
  const pageDocs = computed(() => {
    const passMap = filterPassMap.value
    return pageDocIds.value.map(id => {
      const doc = getDoc(id)
      if (!doc) return null
      const withEdits = applyEdits(doc)
      return {
        ...withEdits,
        _id: id,
        _excluded:    excludedIds.value.has(id),
        _filterPass:  passMap[id] !== false,
        _selected:    selectedIds.value.has(id),
      }
    }).filter(Boolean)
  })

  // ── Pipeline stats ─────────────────────────────────────────────────────────

  const pipelineStats = computed(() => {
    const total    = docCount.value
    const excluded = excludedIds.value.size
    const passMap  = filterPassMap.value
    const passKeys = Object.keys(passMap)
    const filtered = passKeys.length ? passKeys.filter(k => passMap[k]).length : total
    const selected = selectedIds.value.size
    return { total, filtered, excluded, selected }
  })

  // ── Flash indicator ────────────────────────────────────────────────────────

  function flashEvalIndicator() {
    clearTimeout(_evalFlashTimer)
    pipelineEvalFlash.value = true
    _evalFlashTimer = setTimeout(() => { pipelineEvalFlash.value = false }, 350)
  }

  watch(pipelineSteps, () => {
    flashEvalIndicator()
  }, { deep: true })

  // ── Array path init ────────────────────────────────────────────────────────

  function initArrayPath() {
    if (!arrayPath.value && arrayPathSuggestions.value.length) {
      arrayPath.value = arrayPathSuggestions.value[0]
    }
  }

  // Reset pagination and order when data changes
  watch(baseElements, () => {
    _docCache = new Map()
    currentPage.value = 1
    docOrder.value = null
    docEdits.value = {}
    excludedIds.value = new Set()
    selectedIds.value = new Set()
    lastSelectedId.value = null
  })

  watch(arrayPath, () => {
    currentPage.value = 1
  })

  // ── Step CRUD ──────────────────────────────────────────────────────────────

  function addStep(type) {
    const step = { id: ++stepIdCounter, type }
    if (type === 'filter') step.condition = ''
    if (type === 'select') { step.columnsRaw = '' }
    if (type === 'map')    step.rules = [newRule()]
    if (type === 'sort')   { step.field = ''; step.dir = 'asc' }
    pipelineSteps.value.push(step)
    activeStepId.value = step.id
  }

  function removeStep(i) {
    const id = pipelineSteps.value[i]?.id
    pipelineSteps.value.splice(i, 1)
    if (activeStepId.value === id) activeStepId.value = null
  }

  function clearPipeline() {
    pipelineSteps.value = []
    activeStepId.value = null
  }

  function toggleStep(id) {
    activeStepId.value = activeStepId.value === id ? null : id
  }

  function addMapRule(step) {
    step.rules.push(newRule())
  }

  function removeMapRule(step, i) {
    step.rules.splice(i, 1)
  }

  function pasteFilterKey(step, key) {
    step.condition = (step.condition || '') + '(.' + key + ')'
  }

  function toggleSelectColumn(step, key) {
    const cols = parseColumns(step.columnsRaw)
    const idx = cols.indexOf(key)
    if (idx >= 0) cols.splice(idx, 1)
    else cols.push(key)
    step.columnsRaw = cols.join(', ')
  }

  function stepSummary(step) { return stepSummaryText(step) }

  // ── Document exclusion ─────────────────────────────────────────────────────

  function toggleExcluded(id) {
    const s = new Set(excludedIds.value)
    if (s.has(id)) s.delete(id)
    else s.add(id)
    excludedIds.value = s
  }

  function clearExclusions() {
    excludedIds.value = new Set()
  }

  // ── Document selection ─────────────────────────────────────────────────────

  function toggleSelected(id, event) {
    const s = new Set(selectedIds.value)

    if (event && event.shiftKey && lastSelectedId.value != null) {
      const ids = filteredIds.value
      const a = ids.indexOf(lastSelectedId.value)
      const b = ids.indexOf(id)
      if (a !== -1 && b !== -1) {
        const [lo, hi] = a < b ? [a, b] : [b, a]
        for (let i = lo; i <= hi; i++) s.add(ids[i])
      }
    } else if (event && (event.ctrlKey || event.metaKey)) {
      if (s.has(id)) s.delete(id)
      else s.add(id)
    } else {
      if (s.has(id)) s.delete(id)
      else s.add(id)
    }

    lastSelectedId.value = id
    selectedIds.value = s
  }

  function selectAll(checked) {
    if (checked) {
      selectedIds.value = new Set(filteredIds.value)
    } else {
      selectedIds.value = new Set()
    }
  }

  function clearSelection() {
    selectedIds.value = new Set()
  }

  // ── Inline edits ───────────────────────────────────────────────────────────

  function setDocEdit(id, field, value) {
    const e = { ...(docEdits.value[id] || {}), [field]: value }
    docEdits.value = { ...docEdits.value, [id]: e }
  }

  function clearAllEdits() {
    docEdits.value = {}
  }

  // ── Column visibility ──────────────────────────────────────────────────────

  function toggleColumnVisibility(field) {
    const s = new Set(hiddenColumns.value)
    if (s.has(field)) s.delete(field)
    else s.add(field)
    hiddenColumns.value = s
  }

  function showAllColumns() {
    hiddenColumns.value = new Set()
  }

  // ── Column order ───────────────────────────────────────────────────────────

  function setColumnOrder(order) { columnOrder.value = order }
  function resetColumnOrder()    { columnOrder.value = null }

  // ── Sorting ────────────────────────────────────────────────────────────────

  function sortAllDocs(field, dir) {
    sortConfig.value = { field, dir }
    docOrder.value = null
    currentPage.value = 1
    const existing = pipelineSteps.value.find(s => s.type === 'sort')
    if (existing) {
      existing.field = field
      existing.dir   = dir
    } else {
      const step = { id: ++stepIdCounter, type: 'sort', field, dir }
      pipelineSteps.value.push(step)
      activeStepId.value = step.id
    }
  }

  function resetOrder() {
    docOrder.value = null
    sortConfig.value = null
    pipelineSteps.value = pipelineSteps.value.filter(s => s.type !== 'sort')
    currentPage.value = 1
  }

  // ── Step drag-and-drop ─────────────────────────────────────────────────────

  function onStepDragStart(si, e) {
    stepDragSrcIndex.value    = si
    stepDropInsertIndex.value = si
    e.dataTransfer.effectAllowed = 'move'
  }

  function onStepContainerDragOver(e) {
    if (stepDragSrcIndex.value === null) return
    const cards = Array.from(e.currentTarget.querySelectorAll('.pipeline-step-card'))
    if (!cards.length) { stepDropInsertIndex.value = 0; return }
    let insertAt = cards.length
    for (let i = 0; i < cards.length; i++) {
      const rect = cards[i].getBoundingClientRect()
      if (e.clientY < rect.top + rect.height / 2) { insertAt = i; break }
    }
    stepDropInsertIndex.value = insertAt
  }

  function shouldShowStepGap(pos) {
    return stepDragSrcIndex.value !== null
      && stepDropInsertIndex.value === pos
      && pos !== stepDragSrcIndex.value
      && pos !== stepDragSrcIndex.value + 1
  }

  function executeStepDrop() {
    const from = stepDragSrcIndex.value
    const to   = stepDropInsertIndex.value
    if (from === null || to === null || from === to || from === to - 1) {
      _clearStepDrag(); return
    }
    const items = pipelineSteps.value.slice()
    const [moved] = items.splice(from, 1)
    items.splice(to > from ? to - 1 : to, 0, moved)
    pipelineSteps.value = items
    _clearStepDrag()
  }

  function onStepDragEnd() { _clearStepDrag() }

  function _clearStepDrag() {
    stepDragSrcIndex.value    = null
    stepDropInsertIndex.value = null
    draggableStepIndex.value  = null
  }

  // ── Doc row drag-and-drop ──────────────────────────────────────────────────

  function onDocDragStart(docId, event) {
    if (selectedIds.value.has(docId)) {
      docDragSrcIds.value = [...selectedIds.value]
    } else {
      docDragSrcIds.value = [docId]
    }
    event.dataTransfer.effectAllowed = 'move'
  }

  function onDocContainerDragOver(event) {
    if (!docDragSrcIds.value.length) return
    const rows = Array.from(event.currentTarget.querySelectorAll('.doc-row[data-doc-id]'))
    let insertAt = rows.length
    for (let i = 0; i < rows.length; i++) {
      const rect = rows[i].getBoundingClientRect()
      if (event.clientY < rect.top + rect.height / 2) { insertAt = i; break }
    }
    docDropInsertIndex.value = insertAt
  }

  function shouldShowDocGap(rowIndex) {
    return docDragSrcIds.value.length > 0 && docDropInsertIndex.value === rowIndex
  }

  function executeDocDrop() {
    const srcIds  = docDragSrcIds.value.slice()
    const insertAt = docDropInsertIndex.value
    if (!srcIds.length || insertAt === null) { _clearDocDrag(); return }

    const pageStart    = (currentPage.value - 1) * DOCS_PER_PAGE
    const absoluteInsert = pageStart + insertAt

    const order   = orderedIds.value.slice()
    const filtered = order.filter(id => !srcIds.includes(id))
    const removedBefore = order.slice(0, absoluteInsert).filter(id => srcIds.includes(id)).length
    const insertPos = Math.max(0, Math.min(absoluteInsert - removedBefore, filtered.length))
    filtered.splice(insertPos, 0, ...srcIds)
    docOrder.value = filtered
    _clearDocDrag()
  }

  function onDocDragEnd() { _clearDocDrag() }

  function _clearDocDrag() {
    docDragSrcIds.value      = []
    docDropInsertIndex.value = null
    draggableDocId.value     = null
  }

  // ── Output docs (for download) ─────────────────────────────────────────────

  /**
   * Build the final processed document array for XML output.
   * Uses getDoc() for lazy materialisation with cache.
   */
  function getOutputDocs() {
    if (!docCount.value) return null

    const order = orderedIds.value

    // Build docs in order, applying edits, skipping excluded
    let docs = []
    for (const id of order) {
      if (excludedIds.value.has(id)) continue
      const doc = getDoc(id)
      if (!doc) continue
      const { _id, ...rest } = applyEdits(doc)
      docs.push(rest)
    }

    // Apply pipeline steps
    for (const step of pipelineSteps.value) {
      docs = applyStepJs(step, docs)
    }

    // Apply column order (lazy — only at output time)
    if (columnOrder.value) {
      const colOrder = orderedColumns.value
      docs = docs.map(doc => {
        const out = {}
        for (const key of colOrder) {
          if (key in doc) out[key] = doc[key]
        }
        for (const key of Object.keys(doc)) {
          if (!(key in out)) out[key] = doc[key]
        }
        return out
      })
    }

    // Apply hidden columns (lazy — only at output time)
    if (hiddenColumns.value.size) {
      docs = docs.map(doc => {
        const out = {}
        for (const key of Object.keys(doc)) {
          if (!hiddenColumns.value.has(key)) out[key] = doc[key]
        }
        return out
      })
    }

    return docs
  }

  return {
    // State
    arrayPath, pipelineSteps, activeStepId, pipelineEvalFlash,
    currentPage, totalPages, totalDocs, viewerSearch,
    excludedIds, selectedIds, docOrder, sortConfig,
    hiddenColumns, columnOrder,

    // Computed
    arrayPathSuggestions, docTag,
    availableFields, orderedColumns, visibleColumns,
    orderedIds, pageDocs, pageDocIds, filterPassMap,
    pipelineStats, isLargeFile,
    filterComputeProgress,

    // Step ops
    addStep, removeStep, clearPipeline, toggleStep,
    addMapRule, removeMapRule, pasteFilterKey,
    toggleSelectColumn, stepSummary,

    // Step drag
    stepDragSrcIndex, stepDropInsertIndex, draggableStepIndex,
    onStepDragStart, onStepContainerDragOver, shouldShowStepGap,
    executeStepDrop, onStepDragEnd,

    // Doc exclusion
    toggleExcluded, clearExclusions,

    // Doc selection
    toggleSelected, selectAll, clearSelection,

    // Inline edits
    setDocEdit, clearAllEdits,

    // Column visibility
    toggleColumnVisibility, showAllColumns,

    // Column order
    columnOrder, setColumnOrder, resetColumnOrder,

    // Sort / order
    sortAllDocs, resetOrder,

    // Doc drag
    docDragSrcIds, docDropInsertIndex, draggableDocId,
    onDocDragStart, onDocContainerDragOver, shouldShowDocGap,
    executeDocDrop, onDocDragEnd,

    // Init / output
    initArrayPath, getOutputDocs,
  }
}
