<template>
  <div class="right-panel">
    <!-- Panel header -->
    <div class="panel-header">
      <span class="eval-indicator" :class="{ flash: pipelineEvalFlash }" title="Evaluation indicator"></span>
      Transform Pipeline
      <span v-if="pipelineSteps.length" class="badge">{{ pipelineSteps.length }}</span>
      <button
        v-if="pipelineSteps.length"
        @click="$emit('clear')"
        class="clear-btn"
      >clear</button>
    </div>

    <!-- Pipeline area -->
    <div class="pipeline-area" @dragover.prevent="$emit('container-dragover', $event)" @drop.prevent="$emit('execute-drop')">

      <!-- Array path selector -->
      <div v-if="parsedData !== null" class="array-path-section">
        <div class="array-path-label">Array path</div>
        <div v-if="suggestions.length" class="array-path-chips">
          <button
            v-for="sug in suggestions"
            :key="sug"
            class="array-path-chip"
            :class="{ active: arrayPath === sug }"
            @click="$emit('update:arrayPath', sug)"
          >{{ sug || '.' }}</button>
        </div>
        <input
          :value="arrayPath"
          @input="$emit('update:arrayPath', $event.target.value)"
          class="array-path-input"
          placeholder=".items  or  ."
          spellcheck="false"
        />
      </div>

      <!-- Steps list -->
      <template v-if="parsedData !== null">
        <div v-if="!pipelineSteps.length" class="pipeline-empty">
          <div class="pipeline-empty-icon">🔧</div>
          <div class="pipeline-empty-text">No steps yet.<br/>Use the buttons below to begin.</div>
        </div>

        <div v-for="(step, si) in pipelineSteps" :key="step.id">
          <div class="drop-gap" v-if="shouldShowGap(si)"></div>
          <PipelineStep
            :step="step"
            :is-expanded="activeStepId === step.id"
            :is-dragging-source="dragSrcIndex === si"
            :is-draggable="draggableIndex === si"
            :available-keys="availableKeys"
            :summary="stepSummary(step)"
            @toggle="$emit('toggle-step', step.id)"
            @remove="$emit('remove-step', si)"
            @drag-start="$emit('drag-start', si, $event)"
            @drag-end="$emit('drag-end')"
            @set-draggable="$emit('set-draggable', si, $event)"
            @paste-key="$emit('paste-key', step, $event)"
            @toggle-column="$emit('toggle-column', step, $event)"
          />
        </div>
        <div class="drop-gap" v-if="shouldShowGap(pipelineSteps.length)"></div>

        <!-- Add step buttons -->
        <div class="add-steps-row">
          <button class="add-step-segment" @click="$emit('add-step', 'filter')">
            <span class="step-type-badge filter no-ptr">FILTER</span>
          </button>
          <button class="add-step-segment" @click="$emit('add-step', 'select')">
            <span class="step-type-badge select no-ptr">SELECT</span>
          </button>
          <button class="add-step-segment" @click="$emit('add-step', 'map')">
            <span class="step-type-badge map no-ptr">MODIFY</span>
          </button>
        </div>
      </template>
    </div>

    <!-- jq command display -->
    <div v-if="parsedData !== null" class="jq-command-box">
      <div class="jq-command-label">jq command</div>
      <div class="jq-command-field" :class="{ empty: !jqFilter }">
        {{ jqFilter || "jq '.' input.json" }}
      </div>
    </div>

    <!-- Footer / download -->
    <div class="right-footer">
      <button
        class="download-btn"
        :disabled="parsedData === null || pipelineRunning"
        @click="$emit('download')"
      >
        <span v-if="pipelineRunning"><span class="spinner"></span>Running…</span>
        <span v-else>▶ Download</span>
      </button>
    </div>
  </div>
</template>

<script setup>
import PipelineStep from './PipelineStep.vue'

defineProps({
  parsedData:        { default: null },
  pipelineSteps:     { type: Array,   required: true },
  activeStepId:      { default: null },
  arrayPath:         { type: String,  default: '' },
  suggestions:       { type: Array,   default: () => [] },
  availableKeys:     { type: Array,   default: () => [] },
  jqFilter:          { type: String,  default: '' },
  pipelineRunning:   { type: Boolean, default: false },
  pipelineEvalFlash: { type: Boolean, default: false },
  dragSrcIndex:      { default: null },
  dropInsertIndex:   { default: null },
  draggableIndex:    { default: null },
  stepSummary:       { type: Function, required: true },
  shouldShowGap:     { type: Function, required: true },
})

defineEmits([
  'update:arrayPath',
  'add-step', 'remove-step', 'clear', 'toggle-step',
  'drag-start', 'drag-end', 'set-draggable',
  'container-dragover', 'execute-drop',
  'paste-key', 'toggle-column',
  'download',
])
</script>

<style scoped>
.right-panel {
  width: 380px; min-width: 300px; flex-shrink: 0;
  display: flex; flex-direction: column;
  border-left: 1px solid var(--border); background: var(--surface);
}
.panel-header {
  padding: 16px 20px 12px;
  font-size: 0.75rem; font-weight: 600;
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--muted); border-bottom: 1px solid var(--border);
  display: flex; align-items: center; gap: 8px;
}
.clear-btn {
  margin-left: auto; background: none; border: none;
  color: var(--muted); cursor: pointer; font-size: 0.75rem; text-decoration: underline;
}
.eval-indicator {
  width: 8px; height: 8px; border-radius: 50%;
  background: var(--surface3); border: 1px solid var(--border);
  transition: background 0.12s, box-shadow 0.12s; flex-shrink: 0;
}
.eval-indicator.flash { background: var(--green); box-shadow: 0 0 7px var(--green); border-color: var(--green); }
.badge {
  display: inline-flex; align-items: center; justify-content: center;
  background: var(--accent); color: #fff; font-size: 0.65rem; font-weight: 700;
  border-radius: 99px; padding: 1px 7px; min-width: 18px; height: 18px;
}
.pipeline-area { flex: 1; overflow: auto; padding: 12px 14px; display: flex; flex-direction: column; gap: 10px; }
.array-path-section {
  background: var(--surface2); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 10px 12px;
  display: flex; flex-direction: column; gap: 8px;
}
.array-path-label { font-size: 0.68rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); }
.array-path-chips { display: flex; flex-wrap: wrap; gap: 5px; }
.array-path-chip {
  background: rgba(124,106,247,0.12); border: 1px solid rgba(124,106,247,0.35);
  border-radius: var(--radius-sm); padding: 3px 10px;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 0.72rem; color: var(--accent2); cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
}
.array-path-chip.active { background: var(--accent); border-color: var(--accent); color: #fff; }
.array-path-chip:not(.active):hover { border-color: var(--accent2); background: rgba(124,106,247,0.22); }
.array-path-input {
  flex: 1; background: var(--surface3); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 5px 10px;
  color: var(--text); font-family: 'JetBrains Mono','Fira Code',monospace;
  font-size: 0.75rem; outline: none; transition: border-color 0.15s; width: 100%;
}
.array-path-input:focus { border-color: var(--accent); }
.array-path-input::placeholder { color: var(--muted); }
.pipeline-empty {
  display: flex; flex-direction: column; align-items: center;
  padding: 20px 10px; gap: 8px; color: var(--muted);
  border: 1px dashed var(--border); border-radius: var(--radius-sm);
}
.pipeline-empty-icon { font-size: 1.6rem; }
.pipeline-empty-text { font-size: 0.8rem; text-align: center; line-height: 1.5; }
.drop-gap {
  height: 34px; border-radius: var(--radius-sm);
  border: 2px dashed var(--accent); background: var(--accent-glow);
  flex-shrink: 0; pointer-events: none;
}
.add-steps-row {
  display: flex; flex-shrink: 0;
  border: 1px dashed var(--accent); border-radius: var(--radius-sm); overflow: hidden;
}
.add-step-segment {
  flex: 1; padding: 8px 6px;
  background: var(--surface2); border: none; border-right: 1px solid var(--border);
  color: var(--accent2); font-size: 0.75rem; font-weight: 600;
  cursor: pointer; transition: background 0.15s;
  display: flex; align-items: center; justify-content: center;
}
.add-step-segment:last-child { border-right: none; }
.add-step-segment:hover { background: var(--accent-glow); }
.step-type-badge {
  font-size: 0.62rem; font-weight: 700; letter-spacing: 0.06em;
  text-transform: uppercase; padding: 2px 7px; border-radius: 99px; flex-shrink: 0;
}
.step-type-badge.filter { background: var(--yellow-dim); color: var(--yellow); border: 1px solid rgba(251,191,36,0.4); }
.step-type-badge.select { background: var(--green-dim);  color: var(--green);  border: 1px solid rgba(52,211,153,0.4); }
.step-type-badge.map    { background: rgba(124,106,247,0.15); color: var(--accent2); border: 1px solid rgba(124,106,247,0.4); }
.no-ptr { pointer-events: none; }
.jq-command-box { padding: 10px 14px 0; }
.jq-command-label {
  font-size: 0.65rem; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.08em; color: var(--muted); margin-bottom: 5px;
}
.jq-command-field {
  width: 100%; background: var(--surface2); border: 1px solid var(--border);
  border-radius: var(--radius-sm); padding: 7px 10px;
  color: var(--accent2); font-family: 'JetBrains Mono','Fira Code',monospace;
  font-size: 0.72rem; word-break: break-all; white-space: pre-wrap;
  line-height: 1.5; min-height: 36px; user-select: text;
}
.jq-command-field.empty { color: var(--muted); font-style: italic; font-family: sans-serif; font-size: 0.72rem; }
.right-footer { padding: 12px 14px; border-top: 1px solid var(--border); display: flex; gap: 8px; }
.download-btn {
  padding: 10px 14px; flex: 1;
  background: linear-gradient(135deg, var(--green), #059669);
  border: none; border-radius: var(--radius-sm);
  color: #fff; font-weight: 700; font-size: 0.82rem; cursor: pointer;
  transition: opacity 0.2s, transform 0.1s;
  display: flex; align-items: center; justify-content: center; gap: 6px; white-space: nowrap;
}
.download-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.download-btn:not(:disabled):hover { opacity: 0.9; }
.download-btn:not(:disabled):active { transform: scale(0.98); }
.spinner {
  display: inline-block; width: 14px; height: 14px;
  border: 2px solid rgba(255,255,255,0.3); border-top-color: #fff;
  border-radius: 50%; animation: spin 0.7s linear infinite;
  vertical-align: middle; margin-right: 6px;
}
@keyframes spin { to { transform: rotate(360deg); } }
</style>
