<template>
  <div class="doc-viewer">
    <!-- Panel header -->
    <div class="panel-header">
      <div class="header-left">
        <span class="panel-title">Documents</span>
        <span class="doc-count-badge">{{ totalDocs }}</span>
      </div>
      <div class="header-right">
        <input
          class="search-input"
          :value="searchQuery"
          placeholder="Search…"
          @input="$emit('update:searchQuery', $event.target.value)"
        />
      </div>
    </div>

    <!-- Table container -->
    <div
      class="table-container"
      ref="tableRef"
      @dragover.prevent="$emit('container-dragover', $event)"
      @drop.prevent="$emit('execute-drop')"
    >
      <table class="doc-table">
        <thead>
          <tr>
            <th class="col-check">
              <input type="checkbox" :checked="allPageSelected" @change="$emit('select-all', $event.target.checked)" title="Select page" />
            </th>
            <th class="col-drag"></th>
            <th
              v-for="col in columns"
              :key="col"
              class="col-field"
            >
              <span class="col-name" @click="$emit('sort-by', col)" :title="'Sort by ' + col">
                {{ col }}
                <span v-if="sortField === col" class="sort-arrow">{{ sortDir === 'asc' ? '↑' : '↓' }}</span>
              </span>
              <button class="col-hide-btn" @click.stop="$emit('hide-column', col)" title="Hide column">×</button>
            </th>
            <th class="col-actions"></th>
          </tr>
        </thead>
        <tbody>
          <!-- Insert gap before first row -->
          <tr v-if="shouldShowDocGap && shouldShowDocGap(0)" class="drop-gap-row">
            <td :colspan="columns.length + 3"><div class="drop-line"></div></td>
          </tr>

          <template v-for="(doc, rowIdx) in docs" :key="doc._id">
            <tr
              class="doc-row"
              :class="{
                excluded:    doc._excluded,
                'filter-fail': !doc._filterPass,
                selected:    doc._selected,
                dragging:    dragSrcIds && dragSrcIds.includes(doc._id),
              }"
              :data-doc-id="doc._id"
              :draggable="draggableDocId === doc._id"
              @dragstart="$emit('doc-drag-start', doc._id, $event)"
              @dragend="$emit('doc-drag-end')"
            >
              <!-- Checkbox -->
              <td class="col-check">
                <input
                  type="checkbox"
                  :checked="doc._selected"
                  @click.stop
                  @change="$emit('toggle-selected', doc._id, $event)"
                />
              </td>

              <!-- Drag handle -->
              <td class="col-drag">
                <span
                  class="drag-handle"
                  @mousedown.stop="$emit('set-draggable', doc._id, true)"
                  @mouseup.stop="$emit('set-draggable', doc._id, false)"
                  title="Drag to reorder"
                >⠿</span>
              </td>

              <!-- Field value cells -->
              <td
                v-for="col in columns"
                :key="col"
                class="col-value"
                @click="startEdit(doc._id, col, doc[col])"
              >
                <template v-if="editingCell && editingCell.docId === doc._id && editingCell.field === col">
                  <input
                    class="cell-input"
                    :value="editingCell.value"
                    @input="editingCell.value = $event.target.value"
                    @blur="commitEdit"
                    @keydown.enter.prevent="commitEdit"
                    @keydown.escape.prevent="cancelEdit"
                    @click.stop
                    ref="cellInputRef"
                    autofocus
                  />
                </template>
                <template v-else>
                  <span class="cell-text" :title="doc[col]">{{ doc[col] ?? '' }}</span>
                </template>
              </td>

              <!-- Exclude toggle -->
              <td class="col-actions">
                <button
                  class="exclude-btn"
                  :class="{ active: doc._excluded }"
                  @click.stop="$emit('toggle-excluded', doc._id)"
                  :title="doc._excluded ? 'Include in output' : 'Exclude from output'"
                >{{ doc._excluded ? '+' : '−' }}</button>
              </td>
            </tr>

            <!-- Insert gap after each row -->
            <tr v-if="shouldShowDocGap && shouldShowDocGap(rowIdx + 1)" class="drop-gap-row">
              <td :colspan="columns.length + 3"><div class="drop-line"></div></td>
            </tr>
          </template>

          <!-- Empty state -->
          <tr v-if="!docs.length">
            <td :colspan="columns.length + 3" class="empty-cell">
              {{ totalDocs === 0 ? 'No documents found at the specified path.' : 'No documents match the search.' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Pagination -->
    <div class="pagination">
      <button class="page-btn" @click="$emit('page-change', currentPage - 1)" :disabled="currentPage <= 1">‹</button>
      <span class="page-info">{{ currentPage }} / {{ totalPages }}</span>
      <button class="page-btn" @click="$emit('page-change', currentPage + 1)" :disabled="currentPage >= totalPages">›</button>
      <span class="page-total">{{ totalDocs.toLocaleString() }} total</span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, nextTick } from 'vue'

const props = defineProps({
  docs:           { type: Array,    default: () => [] },
  columns:        { type: Array,    default: () => [] },
  currentPage:    { type: Number,   default: 1 },
  totalPages:     { type: Number,   default: 1 },
  totalDocs:      { type: Number,   default: 0 },
  searchQuery:    { type: String,   default: '' },
  dragSrcIds:     { type: Array,    default: () => [] },
  docDropIndex:   { type: Number,   default: null },
  draggableDocId: { type: Number,   default: null },
  shouldShowDocGap: { type: Function, default: null },
  sortField:      { type: String,   default: null },
  sortDir:        { type: String,   default: 'asc' },
})

const emit = defineEmits([
  'toggle-selected', 'select-all', 'toggle-excluded',
  'cell-edit', 'set-draggable',
  'doc-drag-start', 'doc-drag-end',
  'container-dragover', 'execute-drop',
  'page-change', 'sort-by', 'hide-column',
  'update:searchQuery',
])

const tableRef    = ref(null)
const cellInputRef = ref(null)

// Inline editing state
const editingCell = ref(null)  // { docId, field, value }

function startEdit(docId, field, currentValue) {
  if (editingCell.value) commitEdit()
  editingCell.value = { docId, field, value: currentValue ?? '' }
  nextTick(() => {
    if (cellInputRef.value) {
      const el = Array.isArray(cellInputRef.value) ? cellInputRef.value[0] : cellInputRef.value
      el?.focus()
      el?.select()
    }
  })
}

function commitEdit() {
  if (!editingCell.value) return
  const { docId, field, value } = editingCell.value
  emit('cell-edit', docId, field, value)
  editingCell.value = null
}

function cancelEdit() {
  editingCell.value = null
}

const allPageSelected = computed(() =>
  props.docs.length > 0 && props.docs.every(d => d._selected)
)
</script>

<style scoped>
.doc-viewer {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--bg);
  border-right: 1px solid var(--border);
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
  flex-shrink: 0;
  gap: 12px;
}

.header-left { display: flex; align-items: center; gap: 8px; }
.header-right { display: flex; align-items: center; }

.panel-title {
  font-size: 0.78rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--muted);
}

.doc-count-badge {
  background: var(--surface3);
  color: var(--text);
  border-radius: 10px;
  padding: 1px 8px;
  font-size: 0.72rem;
  font-weight: 600;
}

.search-input {
  background: var(--bg);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 5px 10px;
  font-size: 0.78rem;
  outline: none;
  width: 180px;
  transition: border-color 0.15s;
}
.search-input:focus { border-color: var(--accent); }

.table-container {
  flex: 1;
  overflow: auto;
  min-height: 0;
}

.doc-table {
  width: 100%;
  border-collapse: collapse;
  table-layout: auto;
}

.doc-table thead tr {
  background: var(--surface);
  position: sticky;
  top: 0;
  z-index: 2;
}

.doc-table th {
  padding: 8px 10px;
  font-size: 0.72rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--muted);
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}

.col-check  { width: 32px; }
.col-drag   { width: 28px; }
.col-actions { width: 36px; }

.col-field {
  min-width: 90px;
  max-width: 220px;
}

.col-name {
  cursor: pointer;
  user-select: none;
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.col-name:hover { color: var(--text); }

.sort-arrow { color: var(--accent); font-size: 0.8em; }

.col-hide-btn {
  background: none;
  border: none;
  color: var(--muted);
  cursor: pointer;
  padding: 0 2px;
  font-size: 0.85rem;
  line-height: 1;
  opacity: 0.5;
  vertical-align: middle;
}
.col-hide-btn:hover { opacity: 1; color: var(--red); }

/* Rows */
.doc-row td {
  padding: 6px 10px;
  font-size: 0.8rem;
  border-bottom: 1px solid rgba(46, 51, 82, 0.5);
  vertical-align: middle;
}
.doc-row:hover { background: var(--hover-bg); }

.doc-row.selected { background: rgba(247, 137, 106, 0.08); }
.doc-row.selected:hover { background: rgba(247, 137, 106, 0.14); }

.doc-row.excluded td { opacity: 0.35; text-decoration: line-through; }
.doc-row.excluded:hover { background: var(--red-dim); }

.doc-row.filter-fail:not(.excluded) td { color: var(--muted); }

.doc-row.dragging { opacity: 0.35; }

/* Drop gap */
.drop-gap-row td { padding: 0; }
.drop-line {
  height: 2px;
  background: var(--accent);
  border-radius: 1px;
  margin: 0 4px;
}

/* Drag handle */
.drag-handle {
  cursor: grab;
  color: var(--muted);
  font-size: 1rem;
  line-height: 1;
  display: inline-block;
  padding: 0 2px;
  user-select: none;
}
.drag-handle:hover { color: var(--text); }
.drag-handle:active { cursor: grabbing; }

/* Cell text */
.col-value { cursor: pointer; }
.cell-text {
  display: block;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cell-input {
  width: 100%;
  background: var(--surface2);
  color: var(--text);
  border: 1px solid var(--accent);
  border-radius: 3px;
  padding: 2px 6px;
  font-size: 0.8rem;
  outline: none;
}

/* Exclude button */
.exclude-btn {
  background: none;
  border: 1px solid var(--border);
  border-radius: 3px;
  color: var(--muted);
  cursor: pointer;
  font-size: 0.85rem;
  width: 22px;
  height: 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: all 0.1s;
}
.exclude-btn:hover { border-color: var(--red); color: var(--red); }
.exclude-btn.active { border-color: var(--green); color: var(--green); }

/* Empty cell */
.empty-cell {
  text-align: center;
  padding: 40px;
  color: var(--muted);
  font-size: 0.85rem;
}

/* Pagination */
.pagination {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  border-top: 1px solid var(--border);
  background: var(--surface);
  flex-shrink: 0;
}

.page-btn {
  background: var(--surface3);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 4px 10px;
  font-size: 0.85rem;
  cursor: pointer;
  transition: background 0.1s;
}
.page-btn:hover:not(:disabled) { background: var(--surface2); }
.page-btn:disabled { opacity: 0.3; cursor: default; }

.page-info {
  font-size: 0.8rem;
  color: var(--text);
  min-width: 60px;
  text-align: center;
}

.page-total {
  margin-left: auto;
  font-size: 0.75rem;
  color: var(--muted);
}
</style>
