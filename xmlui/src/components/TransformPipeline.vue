<template>
  <div class="pipeline-panel">
    <!-- Panel header -->
    <div class="panel-header">
      <span class="panel-title">Pipeline</span>
      <div class="header-actions">
        <span v-if="pipelineEvalFlash" class="eval-dot" title="Evaluated"></span>
        <button v-if="pipelineSteps.length" class="btn-sm btn-ghost" @click="$emit('clear')">Clear</button>
        <button class="btn-accent" @click="$emit('download')" :disabled="!parsedData">
          ↓ Download XML
        </button>
      </div>
    </div>

    <div class="panel-body">
      <!-- Array path expression -->
      <section class="section">
        <div class="section-title">Data array path</div>
        <div class="path-input-wrap">
          <input
            class="path-input"
            :value="arrayPath"
            @input="$emit('update:arrayPath', $event.target.value)"
            placeholder='.items.item'
            spellcheck="false"
          />
        </div>
        <div v-if="suggestions.length" class="suggestion-chips">
          <span class="chips-label">Suggestions:</span>
          <button
            v-for="s in suggestions"
            :key="s"
            class="chip"
            :class="{ active: s === arrayPath }"
            @click="$emit('update:arrayPath', s)"
          >{{ s }}</button>
        </div>
        <div v-if="parsedData && !arrayPath" class="hint-text">
          Enter a dot-notation path to the repeating element, e.g. <code>.items.item</code>
        </div>
      </section>

      <!-- Stats -->
      <section class="section stats-section" v-if="pipelineStats && pipelineStats.total">
        <div class="stat-row">
          <span class="stat-label">Total</span>
          <span class="stat-value">{{ pipelineStats.total }}</span>
          <span class="stat-label" style="margin-left:14px">Passing filter</span>
          <span class="stat-value accent">{{ pipelineStats.filtered }}</span>
        </div>
        <div class="stat-row">
          <span class="stat-label">Excluded</span>
          <span class="stat-value red">{{ pipelineStats.excluded }}</span>
          <span class="stat-label" style="margin-left:14px">Selected</span>
          <span class="stat-value yellow">{{ pipelineStats.selected }}</span>
        </div>
        <div v-if="pipelineStats.excluded" class="stat-action">
          <button class="btn-sm btn-ghost" @click="$emit('clear-exclusions')">Clear exclusions</button>
        </div>
      </section>

      <!-- Sort -->
      <section class="section">
        <div class="section-title">Sort documents</div>
        <div class="sort-row">
          <select class="select-input" v-model="localSortField">
            <option value="">— field —</option>
            <option v-for="f in availableFields" :key="f" :value="f">{{ f }}</option>
          </select>
          <button
            class="dir-btn"
            @click="localSortDir = localSortDir === 'asc' ? 'desc' : 'asc'"
          >{{ localSortDir === 'asc' ? '↑ Asc' : '↓ Desc' }}</button>
          <button class="btn-accent-sm" @click="doSort" :disabled="!localSortField">Sort All</button>
          <button v-if="sortConfig" class="btn-sm btn-ghost" @click="$emit('reset-order')" title="Reset order">↺</button>
        </div>
      </section>

      <!-- Column visibility -->
      <section class="section" v-if="availableFields.length">
        <div class="section-title">
          Visible columns
          <div style="display:flex;gap:4px;margin-left:auto">
            <button v-if="columnOrderActive" class="btn-sm btn-ghost" @click="$emit('reset-column-order')" title="Reset column order">↺ Order</button>
            <button v-if="hiddenColumns.size" class="btn-sm btn-ghost" @click="$emit('show-all-columns')">Show all</button>
          </div>
        </div>
        <div
          class="col-chips"
          @dragover.prevent="onColContainerDragOver"
          @drop.prevent="onColDrop"
          @dragleave="colDropIndex = null"
        >
          <template v-for="(f, i) in availableFields" :key="f">
            <div class="col-drop-indicator" v-if="shouldShowColGap(i)"></div>
            <button
              class="col-chip"
              :class="{ hidden: hiddenColumns.has(f), 'col-dragging': colDragSrc === i }"
              draggable="true"
              @dragstart="onColDragStart(i, $event)"
              @dragend="onColDragEnd"
              @click="$emit('toggle-column-visibility', f)"
              :title="hiddenColumns.has(f) ? 'Show ' + f : 'Hide ' + f"
            >☸ {{ f }}</button>
          </template>
          <div class="col-drop-indicator" v-if="shouldShowColGap(availableFields.length)"></div>
        </div>
      </section>

      <!-- Pipeline steps -->
      <section class="section">
        <div class="section-title">
          Steps
          <span class="step-count" v-if="pipelineSteps.length">({{ pipelineSteps.length }})</span>
        </div>

        <!-- Add step buttons -->
        <div class="add-step-row">
          <button class="btn-step filter" @click="$emit('add-step', 'filter')">+ Filter</button>
          <button class="btn-step select" @click="$emit('add-step', 'select')">+ Select columns</button>
          <button class="btn-step map"    @click="$emit('add-step', 'map')">+ Map fields</button>
          <button class="btn-step sort"   @click="$emit('add-step', 'sort')">+ Sort</button>
        </div>

        <!-- Steps list (draggable) -->
        <div
          class="steps-list"
          @dragover.prevent="$emit('container-dragover', $event)"
          @drop.prevent="$emit('execute-drop')"
        >
          <!-- Gap before first step -->
          <div class="step-gap" v-if="shouldShowStepGap && shouldShowStepGap(0)"></div>

          <template v-for="(step, si) in pipelineSteps" :key="step.id">
            <PipelineStep
              :step="step"
              :is-expanded="activeStepId === step.id"
              :is-dragging-source="stepDragSrcIndex === si"
              :is-draggable="draggableStepIndex === si"
              :available-fields="availableFields"
              :summary="stepSummary(step)"
              @toggle="$emit('toggle-step', step.id)"
              @remove="$emit('remove-step', si)"
              @drag-start="$emit('drag-start', si, $event)"
              @drag-end="$emit('drag-end')"
              @set-draggable="(v) => $emit('set-draggable', si, v)"
              @paste-key="(st, key) => $emit('paste-key', st, key)"
              @toggle-column="(st, key) => $emit('toggle-column', st, key)"
              @add-rule="(st) => $emit('add-rule', st)"
              @remove-rule="(st, ri) => $emit('remove-rule', st, ri)"
            />
            <!-- Gap after each step -->
            <div class="step-gap" v-if="shouldShowStepGap && shouldShowStepGap(si + 1)"></div>
          </template>

          <div v-if="!pipelineSteps.length" class="no-steps">
            No steps yet. Add a filter, select, or map step above.
          </div>
        </div>
      </section>

      <!-- Doc reorder info -->
      <section class="section" v-if="docOrderActive">
        <div class="section-title">
          Document order
          <button class="btn-sm btn-ghost" @click="$emit('reset-order')">Reset</button>
        </div>
        <div class="hint-text">Custom order is active. Reset to restore original order.</div>
      </section>

      <!-- Selection actions -->
      <section class="section" v-if="selectedCount > 0">
        <div class="section-title">Selection ({{ selectedCount }} docs)</div>
        <div class="sel-actions">
          <button class="btn-sm btn-ghost" @click="$emit('exclude-selected')">Exclude selected</button>
          <button class="btn-sm btn-ghost" @click="$emit('clear-selection')">Clear selection</button>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import PipelineStep from './PipelineStep.vue'

const props = defineProps({
  parsedData:        { default: null },
  pipelineSteps:     { type: Array,    default: () => [] },
  activeStepId:      { default: null },
  arrayPath:         { type: String,   default: '' },
  suggestions:       { type: Array,    default: () => [] },
  availableFields:   { type: Array,    default: () => [] },
  pipelineEvalFlash: { type: Boolean,  default: false },
  pipelineStats:     { type: Object,   default: null },
  sortConfig:        { type: Object,   default: null },
  hiddenColumns:     { type: Object,   default: () => new Set() }, // Set
  columnOrderActive: { type: Boolean,  default: false },
  selectedCount:     { type: Number,   default: 0 },
  docOrderActive:    { type: Boolean,  default: false },
  stepDragSrcIndex:  { default: null },
  stepDropInsertIndex: { default: null },
  draggableStepIndex: { default: null },
  stepSummary:       { type: Function, required: true },
  shouldShowStepGap: { type: Function, default: null },
})

const emit = defineEmits([
  'update:arrayPath',
  'add-step', 'remove-step', 'clear', 'toggle-step',
  'drag-start', 'drag-end', 'set-draggable',
  'container-dragover', 'execute-drop',
  'paste-key', 'toggle-column', 'add-rule', 'remove-rule',
  'sort-all', 'reset-order',
  'toggle-column-visibility', 'show-all-columns',
  'reorder-columns', 'reset-column-order',
  'clear-exclusions', 'clear-selection', 'exclude-selected',
  'download',
])

const localSortField = ref('')
const localSortDir   = ref('asc')

// Sync sort controls with external sortConfig
watch(() => props.sortConfig, sc => {
  if (sc) { localSortField.value = sc.field; localSortDir.value = sc.dir }
  else    { localSortField.value = '' }
}, { immediate: true })

function doSort() {
  if (localSortField.value) {
    emit('sort-all', localSortField.value, localSortDir.value)
  }
}

// ── Column chip drag-and-drop ─────────────────────────────────────────

const colDragSrc   = ref(null)
const colDropIndex = ref(null)

function onColDragStart(i, e) {
  colDragSrc.value = i
  e.dataTransfer.effectAllowed = 'move'
}

function onColContainerDragOver(e) {
  if (colDragSrc.value === null) return
  const chips = Array.from(e.currentTarget.querySelectorAll('.col-chip'))
  if (!chips.length) { colDropIndex.value = 0; return }
  let insertAt = chips.length
  for (let i = 0; i < chips.length; i++) {
    const rect = chips[i].getBoundingClientRect()
    if (e.clientX < rect.left + rect.width / 2) { insertAt = i; break }
  }
  colDropIndex.value = insertAt
}

function shouldShowColGap(pos) {
  return colDragSrc.value !== null
    && colDropIndex.value === pos
    && pos !== colDragSrc.value
    && pos !== colDragSrc.value + 1
}

function onColDrop() {
  const from = colDragSrc.value
  const to   = colDropIndex.value
  colDragSrc.value = null
  colDropIndex.value = null
  if (from === null || to === null || from === to || from === to - 1) return
  const cols = props.availableFields.slice()
  const [moved] = cols.splice(from, 1)
  cols.splice(to > from ? to - 1 : to, 0, moved)
  emit('reorder-columns', cols)
}

function onColDragEnd() {
  colDragSrc.value = null
  colDropIndex.value = null
}
</script>

<style scoped>
.pipeline-panel {
  width: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: var(--surface);
  flex-shrink: 0;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.panel-title {
  font-size: 0.78rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--muted);
}

.header-actions { display: flex; align-items: center; gap: 8px; }

.eval-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--green);
  box-shadow: 0 0 6px var(--green);
  display: inline-block;
}

.btn-accent {
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: var(--radius-sm);
  padding: 5px 12px;
  font-size: 0.78rem;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s;
}
.btn-accent:hover:not(:disabled) { opacity: 0.85; }
.btn-accent:disabled { opacity: 0.4; cursor: default; }

.btn-accent-sm {
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: var(--radius-sm);
  padding: 4px 10px;
  font-size: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s;
}
.btn-accent-sm:hover:not(:disabled) { opacity: 0.85; }
.btn-accent-sm:disabled { opacity: 0.4; cursor: default; }

.btn-sm {
  background: none;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--muted);
  padding: 4px 9px;
  font-size: 0.72rem;
  cursor: pointer;
  transition: all 0.1s;
}
.btn-sm:hover { border-color: var(--text); color: var(--text); }
.btn-ghost { background: none; }

.panel-body {
  flex: 1;
  overflow: auto;
  padding: 0 0 16px;
}

.section {
  padding: 12px 14px 4px;
  border-bottom: 1px solid var(--border);
}
.section:last-child { border-bottom: none; }

.section-title {
  font-size: 0.7rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.07em;
  color: var(--muted);
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.step-count { color: var(--accent); font-weight: 600; }

/* Array path */
.path-input-wrap { margin-bottom: 6px; }
.path-input {
  width: 100%;
  background: var(--bg);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 7px 10px;
  font-size: 0.82rem;
  font-family: 'Fira Code', monospace;
  outline: none;
  transition: border-color 0.15s;
}
.path-input:focus { border-color: var(--accent); }

.suggestion-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
  margin-bottom: 6px;
}
.chips-label { font-size: 0.7rem; color: var(--muted); }

.chip {
  background: var(--surface3);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-node);
  padding: 2px 8px;
  font-size: 0.72rem;
  cursor: pointer;
  transition: all 0.1s;
  font-family: 'Fira Code', monospace;
}
.chip:hover, .chip.active { border-color: var(--accent); color: var(--accent); background: var(--accent-glow); }

.hint-text { font-size: 0.75rem; color: var(--muted); margin-bottom: 8px; }
.hint-text code { background: var(--surface3); padding: 1px 4px; border-radius: 3px; color: var(--accent); }

/* Stats */
.stats-section { padding-bottom: 10px; }
.stat-row { display: flex; align-items: center; gap: 4px; margin-bottom: 4px; }
.stat-label { font-size: 0.72rem; color: var(--muted); }
.stat-value { font-size: 0.82rem; font-weight: 700; color: var(--text); min-width: 28px; }
.stat-value.accent { color: var(--green); }
.stat-value.red    { color: var(--red); }
.stat-value.yellow { color: var(--yellow); }
.stat-action { margin-top: 4px; }

/* Sort */
.sort-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-bottom: 8px; }

.select-input {
  flex: 1;
  background: var(--bg);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 5px 8px;
  font-size: 0.78rem;
  outline: none;
}

.dir-btn {
  background: var(--surface3);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 5px 10px;
  font-size: 0.75rem;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.1s;
}
.dir-btn:hover { background: var(--surface2); }

/* Column chips */
.col-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-bottom: 8px;
}
.col-chip {
  background: var(--surface3);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--attr-key);
  padding: 2px 8px;
  font-size: 0.72rem;
  cursor: grab;
  transition: all 0.1s;
  font-family: 'Fira Code', monospace;
  user-select: none;
}
.col-chip:hover { border-color: var(--text); }
.col-chip.hidden { opacity: 0.35; text-decoration: line-through; }
.col-chip.col-dragging { opacity: 0.3; }

.col-drop-indicator {
  width: 2px;
  min-height: 20px;
  background: var(--accent);
  border-radius: 1px;
  flex-shrink: 0;
  align-self: stretch;
}

/* Add step buttons */
.add-step-row { display: flex; gap: 6px; margin-bottom: 10px; flex-wrap: wrap; }

.btn-step {
  flex: 1;
  border: none;
  border-radius: var(--radius-sm);
  padding: 6px 4px;
  font-size: 0.72rem;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s;
  min-width: 70px;
}
.btn-step.filter { background: rgba(247,137,106,0.2); color: var(--accent); }
.btn-step.select { background: var(--green-dim); color: var(--green); }
.btn-step.map    { background: var(--yellow-dim); color: var(--yellow); }
.btn-step.sort   { background: rgba(147,197,253,0.15); color: var(--attr-key); }
.btn-step:hover { opacity: 0.8; }

/* Steps list */
.steps-list { min-height: 10px; }
.step-gap {
  height: 3px;
  background: var(--accent);
  border-radius: 2px;
  margin: 3px 0;
}
.no-steps {
  font-size: 0.78rem;
  color: var(--muted);
  text-align: center;
  padding: 16px 0;
  font-style: italic;
}

/* Selection actions */
.sel-actions { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 8px; }
</style>
