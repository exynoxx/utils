<template>
  <div class="mid-panel">
    <div class="mid-header">
      <div class="mid-header-left">
        <div class="mid-title">JSON Explorer</div>
        <template v-if="parsedData !== null">
          <span v-if="!hasSteps" class="mid-status-badge raw">Raw JSON</span>
          <span v-else class="mid-status-badge active">
            ⚡ Pipeline active — {{ stepCount }} step{{ stepCount !== 1 ? 's' : '' }}
          </span>
        </template>
        <span
          v-if="isLargeFile"
          title="Large file: preview capped at 50 rows. Press Enter to evaluate."
          class="large-badge"
          style="cursor:pointer"
          @click="$emit('eval')"
        >⚡ Large — Enter to eval</span>
        <span v-if="pipelineStats !== null" class="stats-label">
          <span class="stats-value">{{ pipelineStats.result.toLocaleString() }}</span>
          <span> of {{ pipelineStats.total.toLocaleString() }}</span>
        </span>
      </div>
      <div v-if="parsedData !== null && hasSteps" class="mid-view-toggle">
        <button :class="{ active: treeView === 'transformed' }" @click="$emit('set-view', 'transformed')">Transformed</button>
        <button :class="{ active: treeView === 'original' }"    @click="$emit('set-view', 'original')">Original</button>
      </div>
    </div>

    <div class="tree-scroll" v-if="parsedData !== null">
      <div class="tree-node-root">
        <TreeNode
          :data="displayData"
          :path="''"
          :collapsed-paths="collapsedPaths"
          @toggle="$emit('toggle', $event)"
        />
      </div>
    </div>

    <div class="empty-state" v-else>
      <div class="empty-icon">🔍</div>
      <div class="empty-label">Paste or upload JSON on the left,<br />then build a pipeline on the right.</div>
    </div>
  </div>
</template>

<script setup>
import TreeNode from './TreeNode.vue'

defineProps({
  parsedData:     { default: null },
  displayData:    { default: null },
  collapsedPaths: { required: true },
  treeView:       { type: String,  required: true },
  hasSteps:       { type: Boolean, default: false },
  stepCount:      { type: Number,  default: 0 },
  isLargeFile:    { type: Boolean, default: false },
  pipelineStats:  { type: Object,  default: null },
})
defineEmits(['toggle', 'set-view', 'eval'])
</script>

<style scoped>
.mid-panel { flex: 1; display: flex; flex-direction: column; overflow: hidden; background: var(--bg); }
.mid-header {
  padding: 14px 20px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--surface);
  gap: 12px;
  flex-wrap: wrap;
}
.mid-header-left { display: flex; align-items: center; gap: 12px; }
.mid-title { font-size: 0.75rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); }
.mid-status-badge {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 3px 10px; border-radius: 99px;
  font-size: 0.7rem; font-weight: 600; border: 1px solid;
}
.mid-status-badge.raw    { background: rgba(107,114,128,0.15); border-color: rgba(107,114,128,0.4);  color: var(--muted);   }
.mid-status-badge.active { background: rgba(124,106,247,0.15); border-color: rgba(124,106,247,0.5); color: var(--accent2); }
.large-badge {
  font-size: 0.65rem; font-weight: 600; letter-spacing: 0.05em;
  background: rgba(251,191,36,0.15); border: 1px solid rgba(251,191,36,0.4);
  color: var(--yellow); padding: 2px 7px; border-radius: 99px; text-transform: uppercase;
}
.large-badge:hover { background: rgba(251,191,36,0.28); }
.stats-label { font-size: 0.75rem; font-weight: 600; color: var(--muted); }
.stats-value { color: var(--green); }
.mid-view-toggle { display: flex; gap: 0; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
.mid-view-toggle button {
  background: var(--surface3); border: none; padding: 4px 10px;
  font-size: 0.7rem; color: var(--muted); cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.mid-view-toggle button.active { background: var(--accent); color: #fff; }
.tree-scroll { flex: 1; overflow: auto; padding: 14px 8px 14px 14px; }
.tree-node-root { font-family: 'JetBrains Mono', 'Fira Code', monospace; font-size: 0.8rem; }
.empty-state {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  flex: 1; gap: 14px; color: var(--muted); padding: 40px 20px; text-align: center;
}
.empty-icon  { font-size: 3rem; }
.empty-label { font-size: 0.88rem; line-height: 1.6; }
</style>
