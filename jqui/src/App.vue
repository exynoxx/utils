<template>
  <AppHeader />

  <div class="main">
    <!-- LEFT: JSON Input -->
    <JsonInputPanel
      :raw-input="rawInput"
      :parse-error="parseError"
      :load-progress="loadProgress"
      :file-meta="fileMeta"
      :display-chars="displayChars"
      @update:rawInput="rawInput = $event"
      @parse="parseJSON"
      @file-change="onFileChange"
      @drop="onDrop"
      @textarea-input="onTextareaInput"
    />

    <!-- MIDDLE: Tree Viewer -->
    <JsonTreeViewer
      :parsed-data="parsedData"
      :display-data="displayData"
      :collapsed-paths="collapsedPaths"
      :tree-view="treeView"
      :has-steps="pipelineSteps.length > 0"
      :step-count="pipelineSteps.length"
      :is-large-file="isLargeFile"
      :pipeline-stats="pipelineStats"
      @toggle="onToggle"
      @set-view="treeView = $event"
      @eval="triggerPipelineEval"
    />

    <!-- RIGHT: Transform Pipeline -->
    <TransformPipeline
      :parsed-data="parsedData"
      :pipeline-steps="pipelineSteps"
      :active-step-id="activeStepId"
      :array-path="pipelineArrayPath"
      :suggestions="pipelineArrayPathSuggestions"
      :available-keys="availableKeys"
      :jq-filter="pipelineJqFilter"
      :pipeline-running="pipelineRunning"
      :pipeline-eval-flash="pipelineEvalFlash"
      :drag-src-index="dragSrcIndex"
      :drop-insert-index="dropInsertIndex"
      :draggable-index="draggableIndex"
      :step-summary="stepSummary"
      :should-show-gap="shouldShowGap"
      @update:arrayPath="pipelineArrayPath = $event"
      @add-step="addStep"
      @remove-step="removeStep"
      @clear="clearPipeline"
      @toggle-step="toggleStep"
      @drag-start="(si, e) => onDragStart(si, e)"
      @drag-end="onDragEnd"
      @set-draggable="(si, val) => { draggableIndex = val ? si : null }"
      @container-dragover="onContainerDragOver"
      @execute-drop="executeDrop"
      @paste-key="(step, key) => pasteFilterKey(step, key)"
      @toggle-column="(step, key) => toggleSelectColumn(step, key)"
      @download="applyPipeline"
    />
  </div>

  <transition name="toast">
    <div v-if="toast.visible" class="toast" :class="toast.type">{{ toast.msg }}</div>
  </transition>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import AppHeader        from '@/components/AppHeader.vue'
import JsonInputPanel   from '@/components/JsonInputPanel.vue'
import JsonTreeViewer   from '@/components/JsonTreeViewer.vue'
import TransformPipeline from '@/components/TransformPipeline.vue'

import { useJsonFile }  from '@/composables/useJsonFile'
import { usePipeline }  from '@/composables/usePipeline'
import { useToast }     from '@/composables/useToast'
import { loadJq }       from '@/utils/jqLoader'
import { downloadJson } from '@/utils/jsonUtils'
import { applyStepJs }  from '@/utils/pipelineEval'

// ── Core state ───────────────────────────────────────────────────────────────
const {
  rawInput, parsedData, parseError,
  loadProgress, fileMeta, displayChars,
  parseJSON, onFileChange, onDrop,
  onTextareaInput, onParsed,
} = useJsonFile()

const { toast, showToast } = useToast()

const {
  pipelineArrayPath, pipelineSteps, activeStepId,
  pipelineRunning, treeView, pipelineEvalFlash,
  pipelineResult, pipelineStats, pipelineJqFilter,
  pipelineArrayPathSuggestions, availableKeys, isLargeFile,
  addStep, removeStep, clearPipeline, toggleStep, pasteFilterKey,
  toggleSelectColumn, stepSummary,
  dragSrcIndex, dropInsertIndex, draggableIndex,
  onDragStart, onContainerDragOver, shouldShowGap, executeDrop, onDragEnd,
  triggerPipelineEval, initArrayPath,
  getArrayForPath,
} = usePipeline(parsedData)

// ── Collapsed paths (tree viewer) ────────────────────────────────────────────
const collapsedPaths = ref(new Set())

function onToggle(path) {
  if (collapsedPaths.value.has(path)) collapsedPaths.value.delete(path)
  else collapsedPaths.value.add(path)
}

// Reset collapsed paths when new data is parsed
onParsed(() => {
  collapsedPaths.value = new Set()
  initArrayPath()
})

// ── Display data ─────────────────────────────────────────────────────────────
const displayData = computed(() => {
  if (treeView.value === 'original' || pipelineResult.value === null) return parsedData.value
  return pipelineResult.value
})

// ── Global keyboard shortcut (Enter → evaluate) ───────────────────────────────
function _onKeydown(e) {
  if (e.key !== 'Enter') return
  const tag = e.target.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return
  triggerPipelineEval()
}
onMounted(() => document.addEventListener('keydown', _onKeydown))
onUnmounted(() => document.removeEventListener('keydown', _onKeydown))

// ── Download / apply pipeline ─────────────────────────────────────────────────
async function applyPipeline() {
  if (!parsedData.value) return
  pipelineRunning.value = true
  const ap     = pipelineArrayPath.value.trim()
  const filter = pipelineJqFilter.value
  let result

  try {
    let jqFn = null
    if (filter) {
      try {
        const jqApi = await loadJq()
        if (typeof jqApi === 'function') {
          jqFn = async (f, d) => { const r = await jqApi(d, f); return JSON.stringify(r, null, 2) }
        } else if (jqApi && typeof jqApi.json === 'function') {
          jqFn = async (f, d) => { const r = await jqApi.json(d, f); return JSON.stringify(r, null, 2) }
        }
      } catch (e) { console.warn('jq-web unavailable, using fallback:', e.message) }
    }

    if (jqFn && filter) {
      result = await jqFn(filter, parsedData.value)
    } else {
      if (!ap) {
        result = JSON.stringify(parsedData.value, null, 2)
      } else {
        let arr = getArrayForPath(ap)
        if (!arr) throw new Error('No array found at path: ' + ap)
        for (const step of pipelineSteps.value) arr = applyStepJs(step, arr)
        if (ap === '.' || ap === '') {
          result = JSON.stringify(arr, null, 2)
        } else {
          const segments = ap.replace(/^\./, '').split('.')
          let rebuilt = arr
          for (let i = segments.length - 1; i > 0; i--) rebuilt = { [segments[i]]: rebuilt }
          result = JSON.stringify({ ...parsedData.value, [segments[0]]: rebuilt }, null, 2)
        }
      }
    }
  } catch (e) {
    pipelineRunning.value = false
    showToast('Pipeline error: ' + e.message, 'error', 5000)
    return
  }

  pipelineRunning.value = false
  let parsed
  try { parsed = JSON.parse(result) } catch { parsed = result }
  downloadJson(parsed, 'pipeline-output.json')
  const rowCount = Array.isArray(parsed) ? parsed.length : 1
  showToast('✓ Downloaded pipeline-output.json (' + rowCount + ' item' + (rowCount !== 1 ? 's' : '') + ')')
}
</script>

<style scoped>
.main {
  display: flex;
  flex: 1;
  overflow: hidden;
  height: calc(100vh - 61px);
}
.toast {
  position: fixed; bottom: 28px; left: 50%; transform: translateX(-50%);
  padding: 11px 22px; border-radius: var(--radius-sm);
  font-size: 0.85rem; font-weight: 600; box-shadow: 0 8px 32px rgba(0,0,0,0.45);
  z-index: 200; pointer-events: none; transition: opacity 0.3s;
}
.toast.success { background: #064e3b; border: 1px solid var(--green); color: var(--green); }
.toast.error   { background: #450a0a; border: 1px solid var(--red);   color: var(--red); }
.toast-enter-active, .toast-leave-active { transition: opacity 0.3s; }
.toast-enter-from, .toast-leave-to { opacity: 0; }
</style>
