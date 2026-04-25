<template>
  <div class="step-card pipeline-step-card"
       :class="{ 'is-src': isDraggingSource, 'is-filter': step.type === 'filter', 'is-select': step.type === 'select', 'is-map': step.type === 'map', 'is-sort': step.type === 'sort' }"
       :draggable="isDraggable"
       @dragstart="$emit('drag-start', $event)"
       @dragend="$emit('drag-end')"
  >
    <!-- Card header -->
    <div class="step-header" @click="$emit('toggle')">
      <span
        class="drag-handle"
        @mousedown.stop="$emit('set-draggable', true)"
        @mouseup.stop="$emit('set-draggable', false)"
      >⠿</span>

      <span class="type-badge" :class="step.type">{{ step.type }}</span>

      <span class="step-summary">{{ summary }}</span>

      <div class="header-actions">
        <button class="btn-icon remove-btn" @click.stop="$emit('remove')" title="Remove step">×</button>
        <span class="chevron">{{ isExpanded ? '▲' : '▼' }}</span>
      </div>
    </div>

    <!-- Expanded body -->
    <div v-if="isExpanded" class="step-body">
      <!-- FILTER step -->
      <template v-if="step.type === 'filter'">
        <div class="field-group">
          <label class="field-label">Condition expression</label>
          <textarea
            class="expr-input"
            v-model="step.condition"
            placeholder='e.g. status === "active" or parseInt(age) > 25'
            rows="2"
          ></textarea>
        </div>
        <!-- Quick-paste field chips -->
        <div v-if="availableFields.length" class="field-chips">
          <span class="chips-label">Fields:</span>
          <button
            v-for="key in availableFields"
            :key="key"
            class="chip"
            @click="$emit('paste-key', step, key)"
            :title="'Insert .' + key"
          >{{ key }}</button>
        </div>
      </template>

      <!-- SELECT step -->
      <template v-if="step.type === 'select'">
        <div class="field-group">
          <label class="field-label">Include columns in output</label>
          <div class="col-checkboxes">
            <label
              v-for="key in availableFields"
              :key="key"
              class="col-check-item"
              :class="{ active: isColSelected(key) }"
            >
              <input
                type="checkbox"
                :checked="isColSelected(key)"
                @change="$emit('toggle-column', step, key)"
              />
              {{ key }}
            </label>
          </div>
          <div v-if="!availableFields.length" class="no-fields">No fields detected yet.</div>
        </div>
        <div class="field-group">
          <label class="field-label">Or type columns manually (comma-separated)</label>
          <input class="text-input" v-model="step.columnsRaw" placeholder="field1, field2, @attr" />
        </div>
      </template>

      <!-- MAP step -->
      <template v-if="step.type === 'map'">
        <div v-for="(rule, ri) in step.rules" :key="rule.id" class="rule-card">
          <div class="rule-header">
            <select class="select-sm" v-model="rule.type">
              <option value="add">Set / Add</option>
              <option value="delete">Delete</option>
              <option value="rename">Rename</option>
              <option value="conditional">Conditional</option>
            </select>
            <button class="btn-icon remove-btn" @click="$emit('remove-rule', step, ri)" title="Remove rule">×</button>
          </div>

          <!-- Field name -->
          <div class="rule-row">
            <label class="rule-label">Field</label>
            <input class="text-input" v-model="rule.column" :placeholder="rule.type === 'rename' ? 'original field' : 'field name'" list="field-datalist" />
            <datalist id="field-datalist">
              <option v-for="f in availableFields" :key="f" :value="f" />
            </datalist>
          </div>

          <!-- Rename target -->
          <div v-if="rule.type === 'rename'" class="rule-row">
            <label class="rule-label">New name</label>
            <input class="text-input" v-model="rule.toColumn" placeholder="new field name" />
          </div>

          <!-- Value expression (add / conditional) -->
          <template v-if="rule.type === 'add' || rule.type === 'conditional'">
            <div class="rule-row">
              <label class="rule-label">
                {{ rule.type === 'conditional' ? 'Then value' : 'Value' }}
              </label>
              <input class="text-input flex1" v-model="rule.value" placeholder='e.g. "active" or name + "_new"' />
              <select class="select-sm" v-model="rule.valueType">
                <option value="expr">expr</option>
                <option value="const">literal</option>
              </select>
            </div>
          </template>

          <!-- Condition + else (conditional) -->
          <template v-if="rule.type === 'conditional'">
            <div class="rule-row">
              <label class="rule-label">When</label>
              <input class="text-input" v-model="rule.condition" placeholder='e.g. status === "active"' />
            </div>
            <div class="rule-row">
              <label class="rule-label">Else</label>
              <input class="text-input" v-model="rule.elsValue" placeholder='e.g. "inactive"' />
            </div>
          </template>
        </div>

        <!-- Add rule -->
        <button class="btn-add-rule" @click="$emit('add-rule', step)">+ Add rule</button>

        <!-- Available fields for reference -->
        <div v-if="availableFields.length" class="field-chips">
          <span class="chips-label">Fields:</span>
          <button
            v-for="key in availableFields"
            :key="key"
            class="chip"
            @click="$emit('paste-key', step, key)"
            :title="'Insert .' + key"
          >{{ key }}</button>
        </div>
      </template>

      <!-- SORT step -->
      <template v-if="step.type === 'sort'">
        <div class="field-group">
          <label class="field-label">Sort by field</label>
          <div class="sort-step-row">
            <select class="select-input flex1" v-model="step.field">
              <option value="">— field —</option>
              <option v-for="f in availableFields" :key="f" :value="f">{{ f }}</option>
            </select>
            <button class="dir-btn" @click="step.dir = step.dir === 'asc' ? 'desc' : 'asc'">
              {{ step.dir === 'asc' ? '↑ Asc' : '↓ Desc' }}
            </button>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  step:           { type: Object,  required: true },
  isExpanded:     { type: Boolean, default: false },
  isDraggingSource: { type: Boolean, default: false },
  isDraggable:    { type: Boolean, default: false },
  availableFields: { type: Array,  default: () => [] },
  summary:        { type: String,  default: '' },
})

defineEmits([
  'toggle', 'remove', 'drag-start', 'drag-end', 'set-draggable',
  'paste-key', 'toggle-column', 'add-rule', 'remove-rule',
])

function isColSelected(key) {
  const cols = (props.step.columnsRaw || '').split(',').map(s => s.trim()).filter(Boolean)
  return cols.includes(key)
}
</script>

<style scoped>
.step-card {
  background: var(--surface2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  margin-bottom: 6px;
  overflow: hidden;
  transition: opacity 0.15s;
}
.step-card.is-src { opacity: 0.45; }
.step-card.is-filter { border-left: 3px solid var(--accent); }
.step-card.is-select { border-left: 3px solid var(--green); }
.step-card.is-map    { border-left: 3px solid var(--yellow); }
.step-card.is-sort   { border-left: 3px solid var(--attr-key); }

.step-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  cursor: pointer;
  user-select: none;
}
.step-header:hover { background: var(--hover-bg); }

.drag-handle {
  cursor: grab;
  color: var(--muted);
  font-size: 1rem;
  padding: 0 2px;
  user-select: none;
}
.drag-handle:hover { color: var(--text); }

.type-badge {
  font-size: 0.68rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  padding: 2px 7px;
  border-radius: 4px;
  flex-shrink: 0;
}
.type-badge.filter { background: rgba(247,137,106,0.2); color: var(--accent); }
.type-badge.select { background: var(--green-dim); color: var(--green); }
.type-badge.map    { background: var(--yellow-dim); color: var(--yellow); }
.type-badge.sort   { background: rgba(147,197,253,0.15); color: var(--attr-key); }

.step-summary {
  flex: 1;
  font-size: 0.78rem;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.btn-icon {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--muted);
  font-size: 1rem;
  line-height: 1;
  padding: 0 3px;
  border-radius: 3px;
}
.btn-icon:hover { color: var(--red); }

.remove-btn { font-size: 1.1rem; }

.chevron { font-size: 0.65rem; color: var(--muted); }

/* Body */
.step-body {
  padding: 10px 12px 12px;
  border-top: 1px solid var(--border);
}

.field-group { margin-bottom: 10px; }

.field-label {
  display: block;
  font-size: 0.72rem;
  color: var(--muted);
  margin-bottom: 4px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.expr-input, .text-input {
  width: 100%;
  background: var(--bg);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 6px 9px;
  font-size: 0.8rem;
  font-family: 'Fira Code', monospace;
  outline: none;
  transition: border-color 0.15s;
  resize: vertical;
}
.expr-input:focus, .text-input:focus { border-color: var(--accent); }

.text-input.flex1 { flex: 1; }

/* Column checkboxes */
.col-checkboxes {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  max-height: 120px;
  overflow-y: auto;
}

.col-check-item {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  border: 1px solid var(--border);
  border-radius: 4px;
  font-size: 0.75rem;
  cursor: pointer;
  transition: all 0.1s;
}
.col-check-item.active {
  border-color: var(--green);
  background: var(--green-dim);
  color: var(--green);
}
.col-check-item:hover { border-color: var(--text); }

.no-fields { font-size: 0.75rem; color: var(--muted); font-style: italic; }

/* Rule card */
.rule-card {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 8px 10px;
  margin-bottom: 8px;
}

.rule-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.rule-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.rule-row:last-child { margin-bottom: 0; }

.rule-label {
  font-size: 0.7rem;
  color: var(--muted);
  width: 44px;
  flex-shrink: 0;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.select-sm {
  background: var(--surface3);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 3px 7px;
  font-size: 0.75rem;
  outline: none;
  cursor: pointer;
}

.btn-add-rule {
  width: 100%;
  background: none;
  border: 1px dashed var(--border);
  border-radius: var(--radius-sm);
  color: var(--muted);
  padding: 6px;
  font-size: 0.78rem;
  cursor: pointer;
  transition: all 0.1s;
  margin-bottom: 8px;
}
.btn-add-rule:hover { border-color: var(--accent); color: var(--accent); }

/* Field chips */
.field-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}
.chips-label {
  font-size: 0.7rem;
  color: var(--muted);
  margin-right: 2px;
}
.chip {
  background: var(--surface3);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-node);
  padding: 2px 7px;
  font-size: 0.71rem;
  cursor: pointer;
  transition: all 0.1s;
  font-family: 'Fira Code', monospace;
}
.chip:hover { border-color: var(--accent); color: var(--accent); }

/* Sort step */
.sort-step-row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.select-input {
  background: var(--bg);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 5px 8px;
  font-size: 0.78rem;
  outline: none;
}
.select-input.flex1 { flex: 1; }
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
</style>
