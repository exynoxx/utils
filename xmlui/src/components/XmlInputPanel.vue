<template>
  <div class="input-panel">
    <div class="panel-header">
      <span class="panel-title">Input</span>
      <div class="file-actions">
        <button class="btn-accent" @click="$emit('trigger-file')">Open File</button>
        <button class="btn-parse" @click="$emit('parse')" :disabled="!rawInput.trim()">Parse XML</button>
      </div>
    </div>

    <!-- Drop zone / file info -->
    <div
      class="drop-zone"
      :class="{ dragging: localDragging }"
      @dragover.prevent="localDragging = true"
      @dragleave.prevent="localDragging = false"
      @drop.prevent="handleDrop"
      @click="$emit('trigger-file')"
    >
      <template v-if="fileMeta">
        <span class="file-name">{{ fileMeta.name }}</span>
        <span class="file-size">{{ fileMeta.sizeMB }} MB<span v-if="fileMeta.truncated" class="truncated-badge"> · preview</span></span>
      </template>
      <template v-else>
        <span class="drop-hint">Drop an XML file here or click to browse</span>
      </template>
    </div>

    <!-- Load progress -->
    <div v-if="loadProgress !== null" class="load-progress">
      <div class="load-progress-fill" :style="{ width: loadProgress + '%' }"></div>
    </div>

    <!-- Parse error -->
    <div v-if="parseError" class="parse-error">{{ parseError }}</div>

    <!-- Raw XML textarea -->
    <div class="textarea-wrap">
      <textarea
        class="xml-textarea"
        :value="rawInput"
        placeholder="Paste XML here or drop a file…"
        spellcheck="false"
        @input="handleTextareaInput"
      ></textarea>
      <div v-if="fileMeta && fileMeta.truncated" class="truncate-note">
        Showing first {{ displayChars.toLocaleString() }} characters
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const props = defineProps({
  rawInput:     { type: String,  default: '' },
  parseError:   { type: String,  default: '' },
  loadProgress: { type: Number,  default: null },
  fileMeta:     { type: Object,  default: null },
  displayChars: { type: Number,  default: 8000 },
})

const emit = defineEmits(['update:rawInput', 'parse', 'drop', 'trigger-file', 'textarea-input'])

const localDragging = ref(false)

function handleDrop(e) {
  localDragging.value = false
  emit('drop', e)
}

function handleTextareaInput(e) {
  emit('update:rawInput', e.target.value)
  emit('textarea-input')
}
</script>

<style scoped>
.input-panel {
  width: 400px;
  min-width: 300px;
  display: flex;
  flex-direction: column;
  background: var(--surface);
  border-right: 1px solid var(--border);
  flex-shrink: 0;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
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

.file-actions {
  display: flex;
  gap: 8px;
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
.btn-accent:hover { opacity: 0.85; }

.btn-parse {
  background: var(--surface3);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 5px 12px;
  font-size: 0.78rem;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s;
}
.btn-parse:hover:not(:disabled) { background: var(--surface2); }
.btn-parse:disabled { opacity: 0.4; cursor: default; }

.drop-zone {
  margin: 10px 16px 0;
  padding: 10px 14px;
  border: 1px dashed var(--border);
  border-radius: var(--radius-sm);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 40px;
  transition: border-color 0.15s, background 0.15s;
}
.drop-zone:hover, .drop-zone.dragging {
  border-color: var(--accent);
  background: var(--accent-glow);
}

.drop-hint { color: var(--muted); font-size: 0.8rem; }
.file-name { font-size: 0.82rem; color: var(--text); font-weight: 500; }
.file-size { font-size: 0.75rem; color: var(--muted); }
.truncated-badge { color: var(--yellow); }

.load-progress {
  height: 3px;
  background: var(--surface3);
  margin: 6px 16px 0;
  border-radius: 2px;
  overflow: hidden;
}
.load-progress-fill {
  height: 100%;
  background: var(--accent);
  transition: width 0.1s linear;
}

.parse-error {
  margin: 8px 16px 0;
  padding: 8px 12px;
  background: var(--red-dim);
  border: 1px solid var(--red);
  border-radius: var(--radius-sm);
  color: var(--red);
  font-size: 0.78rem;
  font-family: monospace;
}

.textarea-wrap {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: 10px 16px 16px;
}
.xml-textarea {
  flex: 1;
  width: 100%;
  background: var(--bg);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 10px;
  font-family: 'Fira Code', 'Courier New', monospace;
  font-size: 0.76rem;
  line-height: 1.55;
  resize: none;
  outline: none;
  transition: border-color 0.15s;
}
.xml-textarea:focus { border-color: var(--accent); }
.truncate-note {
  margin-top: 4px;
  font-size: 0.72rem;
  color: var(--muted);
  text-align: right;
}
</style>
