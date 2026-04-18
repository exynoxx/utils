<template>
  <div class="left-panel">
    <div class="panel-header">JSON Input</div>

    <div
      class="drop-zone"
      :class="{ dragging: localDragging }"
      @dragover.prevent="localDragging = true"
      @dragleave.prevent="localDragging = false"
      @drop.prevent="handleDrop"
      @click="triggerFileInput"
    >
      <input
        ref="fileInputRef"
        type="file"
        accept=".json,application/json"
        @change="$emit('file-change', $event)"
        style="display:none"
      />
      <div class="drop-icon">📂</div>
      <div class="drop-label">
        <strong>Click to browse</strong> or drag &amp; drop<br />
        a <code style="color:var(--accent2)">.json</code> file here
      </div>
    </div>

    <div v-if="loadProgress !== null" class="load-progress-wrap">
      <div class="load-progress-bar">
        <div class="load-progress-fill" :style="{ width: loadProgress + '%' }"></div>
      </div>
      <div class="load-progress-label">
        <span class="spinner"></span>Loading&hellip; {{ loadProgress }}%
      </div>
    </div>

    <div class="or-divider">or paste JSON below</div>

    <div v-if="fileMeta && fileMeta.truncated" class="file-truncated-notice">
      📄 <strong>{{ fileMeta.name }}</strong> ({{ fileMeta.sizeMB }}&nbsp;MB) &mdash;
      showing first {{ displayChars.toLocaleString() }}&nbsp;chars in editor;
      <strong>full file used for extraction</strong>
    </div>

    <textarea
      class="json-textarea"
      :value="rawInput"
      @input="$emit('update:rawInput', $event.target.value); $emit('textarea-input')"
      :disabled="loadProgress !== null"
      placeholder='{ "example": [1, 2, 3], "nested": { "key": "value" } }'
      spellcheck="false"
    ></textarea>

    <div v-if="parseError" class="error-box">⚠ {{ parseError }}</div>
    <button class="parse-btn" @click="$emit('parse')" :disabled="loadProgress !== null">Parse JSON →</button>
  </div>
</template>

<script setup>
import { ref } from 'vue'

defineProps({
  rawInput:     { type: String, required: true },
  parseError:   { type: String, default: '' },
  loadProgress: { default: null },
  fileMeta:     { type: Object, default: null },
  displayChars: { type: Number, required: true },
})

const emit = defineEmits([
  'update:rawInput',
  'parse',
  'file-change',
  'drop',
  'textarea-input',
])

// isDragging is a UI-only concern — keep it local
const localDragging = ref(false)
const fileInputRef  = ref(null)

function triggerFileInput() {
  fileInputRef.value && fileInputRef.value.click()
}

function handleDrop(e) {
  localDragging.value = false
  emit('drop', e)
}
</script>

<style scoped>
.left-panel {
  width: 420px;
  min-width: 320px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--border);
  background: var(--surface);
}
.panel-header {
  padding: 16px 20px 12px;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--muted);
  border-bottom: 1px solid var(--border);
}
.drop-zone {
  margin: 16px;
  border: 2px dashed var(--border);
  border-radius: var(--radius);
  padding: 28px 20px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  transition: border-color 0.2s, background 0.2s;
  cursor: pointer;
  position: relative;
}
.drop-zone.dragging { border-color: var(--accent); background: var(--accent-glow); }
.drop-icon { font-size: 2rem; line-height: 1; }
.drop-label { font-size: 0.85rem; color: var(--muted); text-align: center; }
.drop-label strong { color: var(--accent2); cursor: pointer; }
.or-divider {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0 16px;
  color: var(--muted);
  font-size: 0.78rem;
}
.or-divider::before,
.or-divider::after { content: ''; flex: 1; height: 1px; background: var(--border); }
.json-textarea {
  flex: 1;
  margin: 12px 16px 16px;
  background: var(--surface2);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 14px;
  color: var(--text);
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
  font-size: 0.78rem;
  line-height: 1.6;
  resize: none;
  outline: none;
  transition: border-color 0.2s;
  min-height: 160px;
}
.json-textarea:focus { border-color: var(--accent); }
.json-textarea::placeholder { color: var(--muted); }
.parse-btn {
  margin: 0 16px 16px;
  padding: 11px;
  background: linear-gradient(135deg, var(--accent), var(--accent2));
  border: none;
  border-radius: var(--radius-sm);
  color: #fff;
  font-weight: 700;
  font-size: 0.88rem;
  cursor: pointer;
  letter-spacing: 0.03em;
  transition: opacity 0.2s, transform 0.1s;
}
.parse-btn:hover { opacity: 0.9; }
.parse-btn:active { transform: scale(0.98); }
.error-box {
  margin: 0 16px 12px;
  background: rgba(248,113,113,0.12);
  border: 1px solid rgba(248,113,113,0.35);
  border-radius: var(--radius-sm);
  padding: 10px 14px;
  font-size: 0.78rem;
  color: var(--red);
  font-family: monospace;
}
.load-progress-wrap { margin: 8px 16px 4px; display: flex; flex-direction: column; gap: 5px; }
.load-progress-bar { height: 5px; background: var(--surface3); border-radius: 99px; overflow: hidden; }
.load-progress-fill {
  height: 100%;
  border-radius: 99px;
  background: linear-gradient(90deg, var(--accent), var(--accent2));
  transition: width 0.12s ease;
}
.load-progress-label {
  font-size: 0.73rem;
  color: var(--muted);
  text-align: center;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
}
.file-truncated-notice {
  margin: 0 16px 4px;
  font-size: 0.71rem;
  color: var(--muted);
  background: var(--surface2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 5px 10px;
  line-height: 1.45;
}
.file-truncated-notice strong { color: var(--accent2); }
.spinner {
  display: inline-block;
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255,255,255,0.3);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
  vertical-align: middle;
  margin-right: 6px;
}
@keyframes spin { to { transform: rotate(360deg); } }
</style>
