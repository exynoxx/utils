<template>
  <AppHeader />

  <div class="main">
    <!-- LEFT: XML Input -->
    <XmlInputPanel
      :raw-input="rawInput"
      :parse-error="parseError"
      :load-progress="loadProgress"
      :file-meta="fileMeta"
      :display-chars="displayChars"
      @update:rawInput="rawInput = $event"
      @parse="parseXML"
      @drop="onDrop"
      @trigger-file="triggerFileInput"
      @textarea-input="onTextareaInput"
    />

    <!-- MIDDLE: Document Viewer -->
    <XmlDocViewer
      :docs="pageDocs"
      :columns="visibleColumns"
      :current-page="currentPage"
      :total-pages="totalPages"
      :total-docs="totalDocs"
      :search-query="viewerSearch"
      :drag-src-ids="docDragSrcIds"
      :doc-drop-index="docDropInsertIndex"
      :draggable-doc-id="draggableDocId"
      :should-show-doc-gap="shouldShowDocGap"
      :sort-field="sortConfig && sortConfig.field"
      :sort-dir="sortConfig && sortConfig.dir"
      @update:searchQuery="viewerSearch = $event"
      @toggle-selected="(id, e) => toggleSelected(id, e)"
      @select-all="selectAll"
      @toggle-excluded="toggleExcluded"
      @cell-edit="setDocEdit"
      @set-draggable="(id, v) => { draggableDocId = v ? id : null }"
      @doc-drag-start="onDocDragStart"
      @doc-drag-end="onDocDragEnd"
      @container-dragover="onDocContainerDragOver"
      @execute-drop="executeDocDrop"
      @page-change="onPageChange"
      @sort-by="onSortBy"
      @hide-column="toggleColumnVisibility"
      @reorder-columns="setColumnOrder"
    />

    <!-- RIGHT: Transform Pipeline -->
    <div class="pipeline-wrapper" :style="{ width: pipelineWidth + 'px' }">
      <div
        class="resize-handle"
        :class="{ resizing: isResizingPipeline }"
        @mousedown.prevent="onPipelineResizeStart"
      ></div>
      <TransformPipeline
        :parsed-data="parsedData"
        :pipeline-steps="pipelineSteps"
        :active-step-id="activeStepId"
        :array-path="arrayPath"
        :suggestions="arrayPathSuggestions"
        :available-fields="orderedColumns"
        :pipeline-eval-flash="pipelineEvalFlash"
        :pipeline-stats="pipelineStats"
        :sort-config="sortConfig"
        :hidden-columns="hiddenColumns"
        :selected-count="selectedIds.size"
        :doc-order-active="docOrder !== null"
        :column-order-active="columnOrder !== null"
        :step-drag-src-index="stepDragSrcIndex"
        :step-drop-insert-index="stepDropInsertIndex"
        :draggable-step-index="draggableStepIndex"
        :step-summary="stepSummary"
        :should-show-step-gap="shouldShowStepGap"
        @update:arrayPath="arrayPath = $event"
        @add-step="addStep"
        @remove-step="removeStep"
        @clear="clearPipeline"
        @toggle-step="toggleStep"
        @drag-start="(si, e) => onStepDragStart(si, e)"
        @drag-end="onStepDragEnd"
        @set-draggable="(si, v) => { draggableStepIndex = v ? si : null }"
        @container-dragover="onStepContainerDragOver"
        @execute-drop="executeStepDrop"
        @paste-key="pasteFilterKey"
        @toggle-column="toggleSelectColumn"
        @add-rule="addMapRule"
        @remove-rule="removeMapRule"
        @sort-all="sortAllDocs"
        @reset-order="resetOrder"
        @toggle-column-visibility="toggleColumnVisibility"
        @show-all-columns="showAllColumns"
        @reorder-columns="setColumnOrder"
        @reset-column-order="resetColumnOrder"
        @clear-exclusions="clearExclusions"
        @clear-selection="clearSelection"
        @exclude-selected="excludeSelected"
        @download="downloadOutput"
      />
    </div>
  </div>

  <!-- Hidden file input -->
  <input
    ref="fileInput"
    type="file"
    accept=".xml,text/xml,application/xml"
    style="display:none"
    @change="onFileChange"
  />

  <!-- Toast -->
  <transition name="toast">
    <div v-if="toast.visible" class="toast" :class="toast.type">{{ toast.msg }}</div>
  </transition>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import AppHeader         from '@/components/AppHeader.vue'
import XmlInputPanel     from '@/components/XmlInputPanel.vue'
import XmlDocViewer      from '@/components/XmlDocViewer.vue'
import TransformPipeline from '@/components/TransformPipeline.vue'

import { useXmlFile }  from '@/composables/useXmlFile'
import { usePipeline } from '@/composables/usePipeline'
import { useToast }    from '@/composables/useToast'
import { rebuildTree, treeToXml, downloadXml } from '@/utils/xmlUtils'

// ── File state ───────────────────────────────────────────────────────────────
const {
  rawInput, parsedData, parseError,
  loadProgress, fileMeta, displayChars,
  fileInput,
  parseXML, triggerFileInput, onFileChange, onDrop,
  onTextareaInput, onParsed,
} = useXmlFile()

const { toast, showToast } = useToast()

// ── Pipeline state ────────────────────────────────────────────────────────────
const {
  arrayPath, pipelineSteps, activeStepId, pipelineEvalFlash,
  currentPage, totalPages, totalDocs, viewerSearch,
  excludedIds, selectedIds, docOrder, sortConfig, hiddenColumns,

  arrayPathSuggestions, docTag,
  availableFields, orderedColumns, visibleColumns,
  pageDocs, pipelineStats, isLargeFile,

  addStep, removeStep, clearPipeline, toggleStep,
  addMapRule, removeMapRule, pasteFilterKey,
  toggleSelectColumn, stepSummary,

  stepDragSrcIndex, stepDropInsertIndex, draggableStepIndex,
  onStepDragStart, onStepContainerDragOver, shouldShowStepGap,
  executeStepDrop, onStepDragEnd,

  toggleExcluded, clearExclusions,
  toggleSelected, selectAll, clearSelection,
  setDocEdit,
  toggleColumnVisibility, showAllColumns,
  sortAllDocs, resetOrder,

  columnOrder, setColumnOrder, resetColumnOrder,

  docDragSrcIds, docDropInsertIndex, draggableDocId,
  onDocDragStart, onDocContainerDragOver, shouldShowDocGap,
  executeDocDrop, onDocDragEnd,

  initArrayPath, getOutputDocs,
} = usePipeline(parsedData)

// ── Reset on new data ─────────────────────────────────────────────────────────
onParsed(() => {
  initArrayPath()
})

// ── Pagination ────────────────────────────────────────────────────────────────
function onPageChange(page) {
  const p = usePipeline(parsedData)   // access totalPages via prop binding
  if (page >= 1 && page <= totalPages.value) {
    currentPage.value = page
  }
}

// ── Sort via column header click ──────────────────────────────────────────────
function onSortBy(field) {
  const current = sortConfig.value
  if (current && current.field === field) {
    sortAllDocs(field, current.dir === 'asc' ? 'desc' : 'asc')
  } else {
    sortAllDocs(field, 'asc')
  }
}

// ── Exclude all selected ──────────────────────────────────────────────────────
function excludeSelected() {
  const s = new Set(excludedIds.value)
  for (const id of selectedIds.value) s.add(id)
  excludedIds.value = s
  clearSelection()
  showToast(`Excluded ${s.size} document${s.size !== 1 ? 's' : ''}`)
}

// ── Download ──────────────────────────────────────────────────────────────────
function downloadOutput() {
  if (!parsedData.value) return

  let outputDocs
  try {
    outputDocs = getOutputDocs()
  } catch (e) {
    showToast('Error building output: ' + e.message, 'error', 5000)
    return
  }

  if (!outputDocs || !outputDocs.length) {
    showToast('Nothing to download — no documents in output.', 'error', 4000)
    return
  }

  const ap = arrayPath.value.trim()
  let xmlString
  try {
    const newTree = rebuildTree(parsedData.value, ap, outputDocs, docTag.value)
    xmlString = '<?xml version="1.0" encoding="UTF-8"?>\n' + treeToXml(newTree)
  } catch (e) {
    showToast('Serialization error: ' + e.message, 'error', 5000)
    return
  }

  const filename = (fileMeta.value?.name?.replace(/\.[^.]+$/, '') || 'output') + '-processed.xml'
  downloadXml(xmlString, filename)
  showToast(`✓ Downloaded ${filename} (${outputDocs.length} doc${outputDocs.length !== 1 ? 's' : ''})`)
}

// ── Keyboard shortcut ─────────────────────────────────────────────────────────
function _onKeydown(e) {
  if (e.key === 'Escape') {
    clearSelection()
  }
}
onMounted(() => document.addEventListener('keydown', _onKeydown))
onUnmounted(() => document.removeEventListener('keydown', _onKeydown))

// ── Resizable pipeline panel ──────────────────────────────────────────────────
const pipelineWidth      = ref(380)
const isResizingPipeline = ref(false)

function onPipelineResizeStart(e) {
  isResizingPipeline.value = true
  const startX     = e.clientX
  const startWidth = pipelineWidth.value

  function onMove(ev) {
    const delta = startX - ev.clientX   // drag left → wider
    pipelineWidth.value = Math.max(220, Math.min(700, startWidth + delta))
  }
  function onUp() {
    isResizingPipeline.value = false
    document.removeEventListener('mousemove', onMove)
    document.removeEventListener('mouseup', onUp)
  }
  document.addEventListener('mousemove', onMove)
  document.addEventListener('mouseup', onUp)
}
</script>

<style scoped>
.main {
  display: flex;
  flex: 1;
  overflow: hidden;
  height: calc(100vh - 53px);
}

.pipeline-wrapper {
  display: flex;
  flex-direction: row;
  flex-shrink: 0;
  min-width: 220px;
  max-width: 700px;
  overflow: hidden;
}

.resize-handle {
  width: 5px;
  cursor: col-resize;
  background: transparent;
  flex-shrink: 0;
  transition: background 0.15s;
}
.resize-handle:hover,
.resize-handle.resizing {
  background: var(--accent);
}

.toast {
  position: fixed;
  bottom: 28px;
  left: 50%;
  transform: translateX(-50%);
  padding: 11px 22px;
  border-radius: var(--radius-sm);
  font-size: 0.85rem;
  font-weight: 600;
  box-shadow: 0 8px 32px rgba(0,0,0,0.45);
  z-index: 200;
  pointer-events: none;
}
.toast.success { background: #064e3b; border: 1px solid var(--green); color: var(--green); }
.toast.error   { background: #450a0a; border: 1px solid var(--red);   color: var(--red); }
.toast-enter-active, .toast-leave-active { transition: opacity 0.3s; }
.toast-enter-from, .toast-leave-to { opacity: 0; }
</style>
