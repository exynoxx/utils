import { ref, computed, watch, onScopeDispose } from 'vue'
import { LARGE_FILE_ROWS, PREVIEW_CAP } from '@/constants'
import { stepToJq, applyStepJs, parseColumns, ruleToJq } from '@/utils/pipelineEval'
import { evalSimpleJqPath } from '@/utils/jsonUtils'

let stepIdCounter = 0
let ruleIdCounter = 0

function newRule() {
  return {
    id: ++ruleIdCounter,
    type: 'add',
    column: '',
    valueType: 'expr',
    value: '',
    condition: '',
    elsValue: 'null',
  }
}

/**
 * All pipeline state and logic: steps, array path, evaluation, drag-reorder.
 */
export function usePipeline(parsedData) {
  const pipelineArrayPath = ref('')
  const pipelineSteps     = ref([])
  const activeStepId      = ref(null)
  const pipelineRunning   = ref(false)
  const treeView          = ref('transformed')
  const pipelineResult    = ref(null)
  const pipelineStats     = ref(null)
  const pipelineEvalFlash = ref(false)

  let _evalFlashTimer        = null
  let _pipelineDebounceTimer = null

  onScopeDispose(() => {
    clearTimeout(_evalFlashTimer)
    clearTimeout(_pipelineDebounceTimer)
  })

  // ── Suggestions ─────────────────────────────────────────────────────────────
  const pipelineArrayPathSuggestions = computed(() => {
    const suggestions = new Set()
    if (parsedData.value !== null) {
      if (Array.isArray(parsedData.value)) {
        suggestions.add('.')
      } else if (parsedData.value && typeof parsedData.value === 'object') {
        Object.entries(parsedData.value).forEach(([k, v]) => {
          if (Array.isArray(v)) suggestions.add('.' + k)
        })
      }
    }
    return Array.from(suggestions)
  })

  const availableKeys = computed(() => {
    const ap = pipelineArrayPath.value.trim()
    if (!ap || !parsedData.value) return []
    const arr = getArrayForPath(ap)
    if (!arr) return []
    const keySet = new Set()
    arr.slice(0, 200).forEach(item => {
      if (item && typeof item === 'object' && !Array.isArray(item))
        Object.keys(item).forEach(k => keySet.add(k))
    })
    return Array.from(keySet)
  })

  // ── jq filter expression ─────────────────────────────────────────────────
  const pipelineJqFilter = computed(() => {
    const ap = pipelineArrayPath.value.trim()
    if (!ap) return ''
    const stepFilters = pipelineSteps.value.map(stepToJq).filter(Boolean)
    const arrayExpr = '(' + (ap === '.' ? '.' : ap) + ')'
    if (!stepFilters.length) return arrayExpr
    return arrayExpr + ' | ' + stepFilters.join(' | ')
  })

  // ── Large-file flag ──────────────────────────────────────────────────────
  const isLargeFile = computed(() => {
    const ap = pipelineArrayPath.value.trim()
    if (!ap || !parsedData.value) return false
    const arr = getArrayForPath(ap)
    return Array.isArray(arr) && arr.length > LARGE_FILE_ROWS
  })

  // ── Step CRUD ────────────────────────────────────────────────────────────
  function addStep(type) {
    const step = { id: ++stepIdCounter, type }
    if (type === 'filter') step.condition = ''
    if (type === 'select') { step.columnsRaw = ''; step.valuesOnly = true }
    if (type === 'map')    step.rules = [newRule()]
    pipelineSteps.value.push(step)
    activeStepId.value = step.id
  }

  function removeStep(i) {
    const id = pipelineSteps.value[i].id
    pipelineSteps.value.splice(i, 1)
    if (activeStepId.value === id) activeStepId.value = null
  }

  function clearPipeline() { pipelineSteps.value = []; activeStepId.value = null }

  function toggleStep(id) {
    activeStepId.value = activeStepId.value === id ? null : id
  }

  function pasteFilterKey(step, key) {
    step.condition = (step.condition || '') + '(.' + key + ')'
  }

  // ── Select column helpers ────────────────────────────────────────────────
  function selectColumnActive(step, key) {
    return parseColumns(step.columnsRaw).includes(key)
  }

  function toggleSelectColumn(step, key) {
    const cols = parseColumns(step.columnsRaw)
    const idx = cols.indexOf(key)
    if (idx >= 0) cols.splice(idx, 1)
    else cols.push(key)
    step.columnsRaw = cols.join(', ')
  }

  // ── Step summary text ────────────────────────────────────────────────────
  function stepSummary(step) {
    if (step.type === 'filter') {
      const c = step.condition.trim()
      if (!c) return ''
      return 'where ' + (c.length > 32 ? c.slice(0, 32) + '…' : c)
    }
    if (step.type === 'select') {
      const cols = parseColumns(step.columnsRaw)
      if (!cols.length) return ''
      const preview = cols.slice(0, 4).join(', ')
      const suffix = cols.length > 4 ? ', …' : ''
      return (step.valuesOnly && cols.length === 1 ? 'values of ' : 'keep ') + preview + suffix
    }
    if (step.type === 'map') {
      const rule = step.rules[0]
      const col = rule.column.trim()
      if (!col) return ''
      return (rule.type === 'conditional' ? 'if … → ' : '') + col
    }
    return ''
  }

  // ── Drag & drop reorder ──────────────────────────────────────────────────
  const dragSrcIndex    = ref(null)
  const dropInsertIndex = ref(null)
  const draggableIndex  = ref(null)

  function onDragStart(si, e) {
    dragSrcIndex.value    = si
    dropInsertIndex.value = si
    e.dataTransfer.effectAllowed = 'move'
  }

  function onContainerDragOver(e) {
    if (dragSrcIndex.value === null) return
    const cards = Array.from(e.currentTarget.querySelectorAll('.pipeline-step-card'))
    if (!cards.length) { dropInsertIndex.value = 0; return }
    let insertAt = cards.length
    for (let i = 0; i < cards.length; i++) {
      const rect = cards[i].getBoundingClientRect()
      if (e.clientY < rect.top + rect.height / 2) { insertAt = i; break }
    }
    dropInsertIndex.value = insertAt
  }

  function shouldShowGap(pos) {
    return dragSrcIndex.value !== null
      && dropInsertIndex.value === pos
      && pos !== dragSrcIndex.value
      && pos !== dragSrcIndex.value + 1
  }

  function executeDrop() {
    const from = dragSrcIndex.value
    const to   = dropInsertIndex.value
    if (from === null || to === null || from === to || from === to - 1) { onDragEnd(); return }
    const items = pipelineSteps.value.slice()
    const [moved] = items.splice(from, 1)
    items.splice(to > from ? to - 1 : to, 0, moved)
    pipelineSteps.value   = items
    dragSrcIndex.value    = null
    dropInsertIndex.value = null
  }

  function onDragEnd() {
    dragSrcIndex.value    = null
    dropInsertIndex.value = null
    draggableIndex.value  = null
  }

  // ── Evaluation ───────────────────────────────────────────────────────────
  function getArrayForPath(ap) {
    if (!parsedData.value) return null
    if (ap === '.' || ap === '') return Array.isArray(parsedData.value) ? parsedData.value : null
    const jqPath = ap.startsWith('.') ? ap.slice(1) : ap
    const result = evalSimpleJqPath(jqPath, parsedData.value)
    return Array.isArray(result) ? result : null
  }

  function flashEvalIndicator() {
    clearTimeout(_evalFlashTimer)
    pipelineEvalFlash.value = true
    _evalFlashTimer = setTimeout(() => { pipelineEvalFlash.value = false }, 350)
  }

  function computePipelineResult() {
    flashEvalIndicator()
    const ap = pipelineArrayPath.value.trim()
    if (!ap || !parsedData.value || !pipelineSteps.value.length) {
      pipelineResult.value = null
      pipelineStats.value  = null
      return
    }
    try {
      let arr = getArrayForPath(ap)
      if (!arr) { pipelineResult.value = null; pipelineStats.value = null; return }
      const total = arr.length

      // Apply filter steps on full array for accurate counts
      let filtered = arr
      for (const step of pipelineSteps.value) {
        if (step.type === 'filter') filtered = applyStepJs(step, filtered)
      }
      pipelineStats.value = { result: filtered.length, total }

      // Cap to PREVIEW_CAP for display performance
      const preview = filtered.length > PREVIEW_CAP ? filtered.slice(0, PREVIEW_CAP) : filtered

      // Apply select/map on the small preview slice
      let out = preview
      for (const step of pipelineSteps.value) {
        if (step.type !== 'filter') out = applyStepJs(step, out)
      }
      if (ap === '.' || ap === '') { pipelineResult.value = out; return }
      const segments = ap.replace(/^\./, '').split('.')
      let rebuilt = out
      for (let i = segments.length - 1; i > 0; i--) rebuilt = { [segments[i]]: rebuilt }
      pipelineResult.value = { ...parsedData.value, [segments[0]]: rebuilt }
    } catch {
      pipelineResult.value = null
      pipelineStats.value  = null
    }
  }

  function schedulePipelineUpdate() {
    if (isLargeFile.value) {
      pipelineResult.value = null
      pipelineStats.value  = null
      return
    }
    clearTimeout(_pipelineDebounceTimer)
    _pipelineDebounceTimer = setTimeout(computePipelineResult, 400)
  }

  watch([pipelineArrayPath, parsedData], () => {
    if (!isLargeFile.value) computePipelineResult()
    else { pipelineResult.value = null; pipelineStats.value = null }
  })

  watch(pipelineSteps, () => {
    treeView.value = 'transformed'
    schedulePipelineUpdate()
  }, { deep: true })

  // Auto-suggest array path when data is first loaded
  function initArrayPath() {
    if (!pipelineArrayPath.value && pipelineArrayPathSuggestions.value.length) {
      pipelineArrayPath.value = pipelineArrayPathSuggestions.value[0]
    }
  }

  return {
    pipelineArrayPath, pipelineSteps, activeStepId,
    pipelineRunning, treeView, pipelineEvalFlash,
    pipelineResult, pipelineStats, pipelineJqFilter,
    pipelineArrayPathSuggestions, availableKeys, isLargeFile,
    // step ops
    addStep, removeStep, clearPipeline, toggleStep, pasteFilterKey,
    toggleSelectColumn, stepSummary,
    // drag
    dragSrcIndex, dropInsertIndex, draggableIndex,
    onDragStart, onContainerDragOver, shouldShowGap, executeDrop, onDragEnd,
    // eval
    triggerPipelineEval: computePipelineResult, initArrayPath,
    getArrayForPath,
  }
}
