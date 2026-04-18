<template>
  <div class="tree-node">
    <span class="tree-row">
      <span v-if="!isPrimitive" class="toggle-btn" @click="onToggle">{{ isCollapsed ? '▶' : '▼' }}</span>
      <span v-else class="no-toggle"></span>
      <span v-if="keyName !== null" class="jkey">"{{ keyName }}"</span>
      <span v-if="arrayIndex !== null" class="array-idx">{{ arrayIndex }}</span>
      <span v-if="keyName !== null || arrayIndex !== null" class="colon">:</span>
      <template v-if="isPrimitive">
        <span :class="'j' + valueType">{{ displayValue }}</span>
      </template>
      <template v-else-if="isObject">
        <span class="jbrace">{{'{'}}</span>
        <span v-if="isCollapsed" class="collapsed-hint">{{ childCount }} keys…</span>
        <span v-else class="jbrace" style="opacity:0.35">…</span>
      </template>
      <template v-else>
        <span class="jbrace">[</span>
        <span v-if="isCollapsed" class="collapsed-hint">{{ childCount }} items…</span>
        <span v-else class="jbrace" style="opacity:0.35">…</span>
      </template>
    </span>

    <template v-if="!isPrimitive && !isCollapsed">
      <div class="tree-indent">
        <template v-if="isObject">
          <TreeNode
            v-for="[key, val] in visibleItems"
            :key="key"
            :data="val"
            :path="childPath(key)"
            :key-name="String(key)"
            :collapsed-paths="collapsedPaths"
            @toggle="$emit('toggle', $event)"
          />
        </template>
        <template v-else>
          <TreeNode
            v-for="(val, idx) in visibleItems"
            :key="idx"
            :data="val"
            :path="childPath(idx)"
            :array-index="idx"
            :collapsed-paths="collapsedPaths"
            @toggle="$emit('toggle', $event)"
          />
        </template>
        <button v-if="hiddenCount > 0" class="show-more-btn" @click.stop="showMore">
          ▼ show {{ Math.min(hiddenCount, 100) }} more
          <span style="color:var(--muted)">({{ hiddenCount }} remaining)</span>
        </button>
      </div>
      <span class="jbrace" style="margin-left:3px">{{ isObject ? '}' : ']' }}</span>
    </template>
    <template v-else-if="!isPrimitive && isCollapsed">
      <span class="jbrace" style="margin-left:3px">{{ isObject ? '}' : ']' }}</span>
    </template>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { RENDER_LIMIT_STEP } from '@/constants'

const props = defineProps({
  data:           { required: true },
  path:           { type: String, required: true },
  collapsedPaths: { type: Array, required: true },
  keyName:        { type: String, default: null },
  arrayIndex:     { type: Number, default: null },
})
const emit = defineEmits(['toggle'])

const isObject    = computed(() => props.data !== null && typeof props.data === 'object' && !Array.isArray(props.data))
const isArray     = computed(() => Array.isArray(props.data))
const isPrimitive = computed(() => !isObject.value && !isArray.value)
const isCollapsed = computed(() => props.collapsedPaths.includes(props.path))

const childCount = computed(() => {
  if (isObject.value) return Object.keys(props.data).length
  if (isArray.value)  return props.data.length
  return 0
})

const valueType = computed(() => {
  if (props.data === null)             return 'null'
  if (typeof props.data === 'string')  return 'string'
  if (typeof props.data === 'number')  return 'number'
  if (typeof props.data === 'boolean') return 'boolean'
  return null
})

const displayValue = computed(() => {
  if (props.data === null) return 'null'
  if (typeof props.data === 'string') return JSON.stringify(props.data)
  return String(props.data)
})

const visibleCount = ref(RENDER_LIMIT_STEP)

const visibleItems = computed(() => {
  if (isArray.value)  return props.data.slice(0, visibleCount.value)
  if (isObject.value) return Object.entries(props.data).slice(0, visibleCount.value)
  return []
})

const hiddenCount = computed(() => Math.max(0, childCount.value - visibleCount.value))

function showMore() { visibleCount.value += RENDER_LIMIT_STEP }

function onToggle(e) { e.stopPropagation(); emit('toggle', props.path) }

function childPath(key) {
  if (isArray.value) return props.path + '[' + key + ']'
  if (/^[a-zA-Z_$][a-zA-Z0-9_$]*$/.test(String(key))) return props.path + '.' + key
  return props.path + '["' + String(key).replace(/"/g, '\\"') + '"]'
}
</script>

<style scoped>
.tree-node { font-family: 'JetBrains Mono', 'Fira Code', monospace; font-size: 0.8rem; line-height: 1.8; user-select: none; }
.tree-row {
  display: inline-flex; align-items: baseline;
  border-radius: 4px; padding: 0 3px; cursor: default;
  transition: background 0.12s; white-space: nowrap;
}
.tree-row:hover { background: var(--hover-bg); }
.tree-indent { display: inline-block; padding-left: 22px; }
.toggle-btn {
  display: inline-flex; align-items: center; justify-content: center;
  width: 16px; height: 16px; margin-right: 4px;
  border-radius: 3px; color: var(--muted); font-size: 0.65rem;
  flex-shrink: 0; background: var(--surface3); cursor: pointer;
  transition: background 0.12s;
}
.toggle-btn:hover { background: var(--accent); color: #fff; }
.no-toggle { display: inline-block; width: 20px; }
.jkey    { color: var(--key); }
.colon   { color: var(--muted); margin: 0 3px; }
.jstring { color: var(--string); }
.jnumber { color: var(--number); }
.jbool   { color: var(--bool); }
.jnull   { color: var(--null); }
.jbrace  { color: var(--muted); }
.collapsed-hint { color: var(--muted); font-style: italic; font-size: 0.72rem; margin-left: 4px; }
.array-idx { color: var(--muted); font-size: 0.7rem; margin-right: 4px; }
.show-more-btn {
  display: inline-flex; align-items: center; gap: 6px; margin: 2px 0 2px 2px;
  padding: 2px 10px; background: var(--surface3); border: 1px solid var(--border);
  border-radius: var(--radius-sm); color: var(--muted);
  font-family: 'JetBrains Mono','Fira Code',monospace; font-size: 0.72rem; cursor: pointer;
  transition: border-color 0.15s, color 0.15s;
}
.show-more-btn:hover { border-color: var(--accent); color: var(--accent2); }
</style>
