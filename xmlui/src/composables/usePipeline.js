import { ref, computed, watch, onScopeDispose } from 'vue'
import { DOCS_PER_PAGE, LARGE_FILE_ROWS, MAX_FIELD_SAMPLE } from '@/constants'
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
  const excludedIds  = ref(new Set())   // doc IDs excluded from output
  const selectedIds  = ref(new Set())   // doc IDs selected in viewer
  const lastSelectedId = ref(null)      // for shift+click range
  const docOrder     = ref(null)        // number[] | null (null = natural order)
  const docEdits     = ref({})          // { [id]: { [field]: value } }
  const hiddenColumns = ref(new Set())  // field names hidden from viewer

  // Sort config: applied when docOrder is null
  const sortConfig   = ref(null)        // { field: string, dir: 'asc'|'desc' } | null

  // ── Step drag state ────────────────────────────────────────────────────────
  const stepDragSrcIndex    = ref(null)
  const stepDropInsertIndex = ref(null)
  const draggableStepIndex  = ref(null)

  // ── Doc row drag state ─────────────────────────────────────────────────────
  const docDragSrcIds      = ref([])
  const docDropInsertIndex = ref(null)
  const draggableDocId     = ref(null)

  let _evalFlashTimer   = null
  let _debounceTimer    = null

  onScopeDispose(() => {
    clearTimeout(_evalFlashTimer)
    clearTimeout(_debounceTimer)
  })

  // ── Base document extraction ───────────────────────────────────────────────

  /**
   * The raw documents extracted from the parsed XML tree using arrayPath.
   * Each doc gets a stable _id integer.
   */
  const baseDocuments = computed(() => {
    const ap = arrayPath.value.trim()
    if (!ap || !parsedData.value) return []
    const elements = evalXPath(parsedData.value, ap)
    return elements.map((el, i) => ({ _id: i, ...elementToDoc(el) }))
  })

  /** The last segment of arrayPath — used as the XML element tag for output. */
  const docTag = computed(() => {
    const ap = arrayPath.value.trim()
    if (!ap) return 'item'
    const segs = ap.replace(/^\./, '').split('.').filter(Boolean)
    return segs[segs.length - 1] || 'item'
  })

  /** Whether dataset is large enough to skip live preview. */
  const isLargeFile = computed(() => baseDocuments.value.length > LARGE_FILE_ROWS)

  // ── Array path suggestions ─────────────────────────────────────────────────

  const arrayPathSuggestions = computed(() => getArrayPathSuggestions(parsedData.value))

  // ── Available fields ───────────────────────────────────────────────────────

  const availableFields = computed(() => {
    const docs = baseDocuments.value.slice(0, MAX_FIELD_SAMPLE)
    const fieldSet = new Set()
    for (const doc of docs) {
      for (const key of Object.keys(doc)) {
        if (key !== '_id') fieldSet.add(key)
      }
    }
    return Array.from(fieldSet)
  })

  /** Columns visible in the viewer (all available minus hidden). */
  const visibleColumns = computed(() => {
    return availableFields.value.filter(f => !hiddenColumns.value.has(f))
  })

  // ── Ordering ───────────────────────────────────────────────────────────────

  /** Doc IDs in their current display order. */
  const orderedIds = computed(() => {
    const docs = baseDocuments.value
    if (!docs.length) return []

    let ids = docs.map(d => d._id)

    if (docOrder.value) {
      // Use custom order, ensuring we include any new IDs not yet in order
      const orderSet = new Set(docOrder.value)
      const existing = docOrder.value.filter(id => ids.includes(id))
      const added    = ids.filter(id => !orderSet.has(id))
      ids = [...existing, ...added]
    } else if (sortConfig.value) {
      const { field, dir } = sortConfig.value
      const docMap = Object.fromEntries(docs.map(d => [d._id, d]))
      ids = ids.slice().sort((a, b) => {
        const va = docMap[a]?.[field] ?? ''
        const vb = docMap[b]?.[field] ?? ''
        const cmp = String(va).localeCompare(String(vb), undefined, { numeric: true })
        return dir === 'asc' ? cmp : -cmp
      })
    }

    return ids
  })

  // ── Docs with edits applied ────────────────────────────────────────────────

  function applyEdits(doc) {
    const edits = docEdits.value[doc._id]
    return edits ? { ...doc, ...edits } : doc
  }

  // ── Filter pass map (which docs pass all filter steps) ────────────────────

  const filterPassMap = computed(() => {
    const filterSteps = pipelineSteps.value.filter(s => s.type === 'filter')
    const passMap = {}
    for (const doc of baseDocuments.value) {
      const d = applyEdits(doc)
      let pass = true
      for (const step of filterSteps) {
        const cond = (step.condition || '').trim()
        if (!cond) continue
        try {
          const prepared = cond
            .replace(/^\./g, 'doc.')
            .replace(/(?<![a-zA-Z0-9_$\]])\./g, 'doc.')
          // eslint-disable-next-line no-new-func
          const fn = new Function('doc', 'item', 'with(item){ return !!((' + prepared + ')) }')
          if (!fn(d, d)) { pass = false; break }
        } catch { pass = false; break }
      }
      passMap[doc._id] = pass
    }
    return passMap
  })

  // ── Current page docs ──────────────────────────────────────────────────────

  const filteredIds = computed(() => {
    const q = viewerSearch.value.trim().toLowerCase()
    if (!q) return orderedIds.value

    const docMap = Object.fromEntries(baseDocuments.value.map(d => [d._id, d]))
    return orderedIds.value.filter(id => {
      const doc = docMap[id]
      if (!doc) return false
      const editsDoc = applyEdits(doc)
      return Object.values(editsDoc).some(v =>
        v != null && String(v).toLowerCase().includes(q)
      )
    })
  })

  const totalDocs  = computed(() => filteredIds.value.length)
  const totalPages = computed(() => Math.max(1, Math.ceil(totalDocs.value / DOCS_PER_PAGE)))

  const pageDocIds = computed(() => {
    const start = (currentPage.value - 1) * DOCS_PER_PAGE
    return filteredIds.value.slice(start, start + DOCS_PER_PAGE)
  })

  /** Enriched page documents with view-layer metadata. */
  const pageDocs = computed(() => {
    const docMap = Object.fromEntries(baseDocuments.value.map(d => [d._id, d]))
    const passMap = filterPassMap.value
    return pageDocIds.value.map(id => {
      const doc = docMap[id]
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
    const total    = baseDocuments.value.length
    const excluded = excludedIds.value.size
    const filtered = Object.values(filterPassMap.value).filter(Boolean).length
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
  watch(baseDocuments, () => {
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
      // Range select between lastSelectedId and id
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
      // Simple toggle without modifier
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

  // ── Sorting ────────────────────────────────────────────────────────────────

  function sortAllDocs(field, dir) {
    sortConfig.value = { field, dir }
    docOrder.value = null
    currentPage.value = 1
  }

  function resetOrder() {
    docOrder.value = null
    sortConfig.value = null
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
    // Drag all selected docs if the dragged doc is among them; else just this one
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

    // Convert page-relative insert index to absolute in orderedIds
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
   * Applies: edits → exclusions → order → pipeline steps.
   * Strips the internal _id field.
   */
  function getOutputDocs() {
    if (!baseDocuments.value.length) return null

    // Apply inline edits
    let docs = baseDocuments.value.map(doc => applyEdits(doc))

    // Apply custom order
    const order = orderedIds.value
    const docMap = Object.fromEntries(docs.map(d => [d._id, d]))
    docs = order.map(id => docMap[id]).filter(Boolean)

    // Remove excluded docs
    docs = docs.filter(d => !excludedIds.value.has(d._id))

    // Strip internal _id
    docs = docs.map(({ _id, ...rest }) => rest)

    // Apply pipeline steps
    for (const step of pipelineSteps.value) {
      docs = applyStepJs(step, docs)
    }

    return docs
  }

  return {
    // State
    arrayPath, pipelineSteps, activeStepId, pipelineEvalFlash,
    currentPage, totalPages, totalDocs, viewerSearch,
    excludedIds, selectedIds, docOrder, sortConfig,
    hiddenColumns,

    // Computed
    arrayPathSuggestions, baseDocuments, docTag,
    availableFields, visibleColumns,
    orderedIds, pageDocs, pageDocIds, filterPassMap,
    pipelineStats, isLargeFile,

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
