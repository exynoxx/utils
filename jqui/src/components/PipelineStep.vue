<template>
  <div
    class="pipeline-step-card"
    :class="{ expanded: isExpanded, 'dragging-source': isDraggingSource }"
    :draggable="isDraggable"
    @dragstart="$emit('drag-start', $event)"
    @dragend="$emit('drag-end')"
  >
    <!-- Card header -->
    <div class="pipeline-step-card-header" @click="$emit('toggle')">
      <span
        class="drag-handle"
        @mousedown.stop="$emit('set-draggable', true)"
        @mouseup.stop="$emit('set-draggable', false)"
        title="Drag to reorder"
      >⠿</span>
      <span class="step-type-badge" :class="step.type">
        {{ step.type === 'map' ? 'MODIFY' : step.type.toUpperCase() }}
      </span>
      <span class="step-summary" :class="{ empty: !summary }">
        {{ summary || 'click to configure…' }}
      </span>
      <span class="step-expand-icon">{{ isExpanded ? '▲' : '▼' }}</span>
      <button class="step-remove-btn" @click.stop="$emit('remove')" title="Remove step">✕</button>
    </div>

    <!-- Expanded edit area -->
    <div v-if="isExpanded" class="step-edit-area">

      <!-- FILTER -->
      <template v-if="step.type === 'filter'">
        <div v-if="availableKeys.length" style="display:flex;flex-wrap:wrap;gap:5px;">
          <button
            v-for="k in availableKeys"
            :key="k"
            class="array-path-chip"
            @click="$emit('paste-key', k)"
            style="font-size:0.68rem;"
            :title="'Paste (.' + k + ') into condition'"
          >{{ k }}</button>
        </div>
        <div>
          <div class="step-field-label">Condition</div>
          <input
            class="step-input"
            v-model="step.condition"
            placeholder=".price > 1000  or  .status == &quot;active&quot;"
            spellcheck="false"
          />
        </div>
        <div class="step-hint">Use <code>.</code> to reference the current element.<br/>e.g. <code>.age > 18</code> &nbsp; <code>.name != ""</code></div>
        <div v-if="jqPreview" class="step-jq-preview">{{ jqPreview }}</div>
      </template>

      <!-- SELECT -->
      <template v-else-if="step.type === 'select'">
        <div>
          <div class="step-field-label">Columns (comma-separated)</div>
          <input
            class="step-input"
            v-model="step.columnsRaw"
            placeholder="id, name, price"
            spellcheck="false"
          />
        </div>
        <div v-if="availableKeys.length" style="display:flex;flex-wrap:wrap;gap:5px;">
          <button
            v-for="k in availableKeys"
            :key="k"
            class="array-path-chip"
            :class="{ active: isColumnActive(k) }"
            @click="$emit('toggle-column', k)"
            style="font-size:0.68rem;"
          >{{ k }}</button>
        </div>
        <div class="step-hint">Keep only the listed fields per row.</div>
        <label
          v-if="parsedColumns.length === 1"
          style="display:flex;align-items:center;gap:8px;cursor:pointer;font-size:0.78rem;color:var(--muted);user-select:none;"
        >
          <input type="checkbox" v-model="step.valuesOnly" style="accent-color:var(--accent);width:14px;height:14px;cursor:pointer;" />
          Return array of values
          <span style="font-family:'JetBrains Mono','Fira Code',monospace;font-size:0.7rem;color:var(--muted);">(e.g. [1, 2, 3])</span>
        </label>
        <div v-if="jqPreview" class="step-jq-preview">{{ jqPreview }}</div>
      </template>

      <!-- MAP -->
      <template v-else-if="step.type === 'map'">
        <div style="align-self:flex-start;">
          <div class="rule-value-type-toggle">
            <button :class="{ active: step.rules[0].type === 'add' }"         @click="step.rules[0].type = 'add'">Add / Update</button>
            <button :class="{ active: step.rules[0].type === 'conditional' }" @click="step.rules[0].type = 'conditional'">Conditional</button>
          </div>
        </div>

        <!-- Add/Update -->
        <template v-if="step.rules[0].type === 'add'">
          <div class="rule-fields">
            <div class="rule-field-row">
              <span class="rule-field-label">Column</span>
              <input class="rule-input" v-model="step.rules[0].column" placeholder="new_or_existing_key" spellcheck="false" />
            </div>
            <div class="rule-field-row">
              <span class="rule-field-label">Value</span>
              <div class="rule-value-type-toggle">
                <button :class="{ active: step.rules[0].valueType === 'expr' }"  @click="step.rules[0].valueType = 'expr'">Expr</button>
                <button :class="{ active: step.rules[0].valueType === 'const' }" @click="step.rules[0].valueType = 'const'">Const</button>
              </div>
              <input
                class="rule-input"
                v-model="step.rules[0].value"
                :placeholder="step.rules[0].valueType === 'expr' ? '.price * 1.2' : '&quot;hello&quot;  or  42'"
                spellcheck="false"
              />
            </div>
            <div class="rule-cond-hint" v-if="step.rules[0].valueType === 'expr'">
              e.g. <code>.price * 1.1</code> &nbsp; <code>.first + " " + .last</code>
            </div>
          </div>
        </template>

        <!-- Conditional -->
        <template v-else-if="step.rules[0].type === 'conditional'">
          <div class="rule-fields">
            <div class="rule-field-row">
              <span class="rule-field-label">Column</span>
              <input class="rule-input" v-model="step.rules[0].column" placeholder="key_to_set" spellcheck="false" />
            </div>
            <div class="rule-field-row">
              <span class="rule-field-label">Condition</span>
              <input class="rule-input" v-model="step.rules[0].condition" placeholder=".status == &quot;active&quot;" spellcheck="false" />
            </div>
            <div class="rule-field-row">
              <span class="rule-field-label">If true</span>
              <div class="rule-value-type-toggle">
                <button :class="{ active: step.rules[0].valueType === 'expr' }"  @click="step.rules[0].valueType = 'expr'">Expr</button>
                <button :class="{ active: step.rules[0].valueType === 'const' }" @click="step.rules[0].valueType = 'const'">Const</button>
              </div>
              <input
                class="rule-input"
                v-model="step.rules[0].value"
                :placeholder="step.rules[0].valueType === 'expr' ? '.price * 0.9' : '&quot;yes&quot;'"
                spellcheck="false"
              />
            </div>
            <div class="rule-field-row">
              <span class="rule-field-label">If false</span>
              <input
                class="rule-input"
                v-model="step.rules[0].elsValue"
                :placeholder="step.rules[0].valueType === 'expr' ? '.price' : '&quot;no&quot;'"
                spellcheck="false"
              />
            </div>
            <div class="rule-cond-hint"><code>if (condition) then true_val else false_val end</code></div>
          </div>
        </template>

        <div v-if="jqPreview" class="step-jq-preview">{{ jqPreview }}</div>
      </template>

    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { stepToJq, parseColumns } from '@/utils/pipelineEval'

const props = defineProps({
  step:            { type: Object,  required: true },
  isExpanded:      { type: Boolean, default: false },
  isDraggingSource:{ type: Boolean, default: false },
  isDraggable:     { type: Boolean, default: false },
  availableKeys:   { type: Array,   default: () => [] },
  summary:         { type: String,  default: '' },
})

defineEmits(['toggle', 'remove', 'drag-start', 'drag-end', 'set-draggable', 'paste-key', 'toggle-column'])

const jqPreview    = computed(() => stepToJq(props.step))
const parsedColumns = computed(() => parseColumns(props.step.columnsRaw))
function isColumnActive(key) { return parsedColumns.value.includes(key) }
</script>

<style scoped>
.pipeline-step-card { border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; background: var(--surface2); cursor: default; }
.pipeline-step-card.dragging-source { opacity: 0.35; }
.drag-handle {
  display: inline-flex; align-items: center; justify-content: center;
  color: var(--muted); font-size: 0.8rem; cursor: grab; padding: 0 4px 0 0;
  flex-shrink: 0; user-select: none;
}
.drag-handle:active { cursor: grabbing; }
.pipeline-step-card-header {
  display: flex; align-items: center; gap: 8px; padding: 9px 12px;
  cursor: pointer; transition: background 0.12s; user-select: none;
}
.pipeline-step-card-header:hover { background: var(--hover-bg); }
.pipeline-step-card.expanded .pipeline-step-card-header { border-bottom: 1px solid var(--border); }
.step-type-badge {
  font-size: 0.62rem; font-weight: 700; letter-spacing: 0.06em;
  text-transform: uppercase; padding: 2px 7px; border-radius: 99px; flex-shrink: 0;
}
.step-type-badge.filter { background: var(--yellow-dim); color: var(--yellow); border: 1px solid rgba(251,191,36,0.4); }
.step-type-badge.select { background: var(--green-dim);  color: var(--green);  border: 1px solid rgba(52,211,153,0.4); }
.step-type-badge.map    { background: rgba(124,106,247,0.15); color: var(--accent2); border: 1px solid rgba(124,106,247,0.4); }
.step-summary {
  flex: 1; font-size: 0.75rem; color: var(--text);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
}
.step-summary.empty { color: var(--muted); font-style: italic; font-family: sans-serif; font-size: 0.72rem; }
.step-expand-icon  { color: var(--muted); font-size: 0.65rem; flex-shrink: 0; }
.step-remove-btn {
  background: none; border: none; color: var(--muted);
  cursor: pointer; font-size: 0.95rem; padding: 0 2px;
  transition: color 0.15s; flex-shrink: 0;
}
.step-remove-btn:hover { color: var(--red); }
.step-edit-area { padding: 12px 12px 10px; display: flex; flex-direction: column; gap: 10px; }
.step-field-label {
  font-size: 0.68rem; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.07em; color: var(--muted); margin-bottom: 4px;
}
.step-input {
  width: 100%; background: var(--surface3); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 6px 10px;
  color: var(--text); font-family: 'JetBrains Mono','Fira Code',monospace;
  font-size: 0.75rem; outline: none; transition: border-color 0.15s;
}
.step-input:focus { border-color: var(--accent); }
.step-input::placeholder { color: var(--muted); }
.step-hint {
  font-size: 0.7rem; color: var(--muted); line-height: 1.5;
  font-family: 'JetBrains Mono','Fira Code',monospace;
  background: var(--surface3); border-radius: var(--radius-sm); padding: 4px 8px;
}
.step-jq-preview {
  font-family: 'JetBrains Mono','Fira Code',monospace;
  font-size: 0.7rem; color: var(--accent2);
  background: var(--surface3); border-radius: var(--radius-sm);
  padding: 4px 8px; word-break: break-all; opacity: 0.8;
}
.rule-value-type-toggle { display: flex; gap: 0; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; flex-shrink: 0; }
.rule-value-type-toggle button {
  background: var(--surface2); border: none; padding: 3px 8px;
  font-size: 0.68rem; color: var(--muted); cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.rule-value-type-toggle button.active { background: var(--accent); color: #fff; }
.rule-fields  { display: grid; gap: 7px; }
.rule-field-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.rule-field-label { font-size: 0.68rem; color: var(--muted); white-space: nowrap; min-width: 60px; }
.rule-input {
  flex: 1; min-width: 100px;
  background: var(--surface2); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 4px 9px;
  color: var(--text); font-family: 'JetBrains Mono','Fira Code',monospace;
  font-size: 0.74rem; outline: none; transition: border-color 0.15s;
}
.rule-input:focus { border-color: var(--accent); }
.rule-input::placeholder { color: var(--muted); }
.rule-cond-hint {
  font-size: 0.68rem; color: var(--muted); line-height: 1.5;
  font-family: 'JetBrains Mono','Fira Code',monospace;
  background: var(--surface2); border-radius: var(--radius-sm); padding: 4px 8px;
}
.array-path-chip {
  background: rgba(124,106,247,0.12); border: 1px solid rgba(124,106,247,0.35);
  border-radius: var(--radius-sm); padding: 3px 10px;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 0.72rem; color: var(--accent2); cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
}
.array-path-chip.active { background: var(--accent); border-color: var(--accent); color: #fff; }
.array-path-chip:not(.active):hover { border-color: var(--accent2); background: rgba(124,106,247,0.22); }
</style>
