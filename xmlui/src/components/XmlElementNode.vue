<template>
  <span class="xml-node">
    <!-- Element node -->
    <span v-if="node.tag" class="el-wrap">
      <!-- Opening tag -->
      <span class="el-open">
        <span
          v-if="hasContent"
          class="toggle"
          @click.stop="collapsed = !collapsed"
        >{{ collapsed ? '▶' : '▼' }}</span>
        <span class="tag-bracket">&lt;</span>
        <span class="tag-name">{{ node.tag }}</span>
        <template v-for="(v, k) in node.attrs" :key="k">
          <span class="attr-key"> {{ k }}</span>
          <span class="attr-eq">=</span>
          <span class="attr-val">"{{ v }}"</span>
        </template>
        <span class="tag-bracket">&gt;</span>
      </span>

      <!-- Children / text content -->
      <span v-if="!collapsed">
        <span v-if="node.text && !node.children.length" class="text-node">{{ node.text }}</span>
        <span v-else-if="node.children.length" class="children">
          <XmlElementNode
            v-for="(child, i) in node.children"
            :key="i"
            :node="child"
            :depth="depth + 1"
          />
        </span>
        <!-- Closing tag -->
        <span v-if="hasContent" class="el-close">
          <span class="tag-bracket">&lt;/</span>
          <span class="tag-name">{{ node.tag }}</span>
          <span class="tag-bracket">&gt;</span>
        </span>
      </span>
      <!-- Collapsed summary -->
      <span v-else class="collapsed-hint"> … <span class="tag-bracket">&lt;/</span><span class="tag-name">{{ node.tag }}</span><span class="tag-bracket">&gt;</span></span>
    </span>
  </span>
</template>

<script setup>
import { ref, computed } from 'vue'

const props = defineProps({
  node:  { type: Object, required: true },
  depth: { type: Number, default: 0 },
})

const collapsed = ref(props.depth >= 3)

const hasContent = computed(() =>
  (props.node.children && props.node.children.length > 0) || !!props.node.text
)
</script>

<style scoped>
.xml-node {
  display: block;
  font-family: 'Fira Code', 'Courier New', monospace;
  font-size: 0.78rem;
  line-height: 1.7;
}
.el-wrap {
  display: block;
  padding-left: 1.2em;
}
.el-open, .el-close { display: inline; }
.toggle {
  display: inline-block;
  width: 12px;
  margin-left: -14px;
  color: var(--muted);
  cursor: pointer;
  user-select: none;
  font-size: 0.65rem;
}
.toggle:hover { color: var(--accent); }
.tag-bracket { color: var(--muted); }
.tag-name    { color: var(--tag); }
.attr-key    { color: var(--attr-key); }
.attr-eq     { color: var(--muted); }
.attr-val    { color: var(--attr-val); }
.text-node   { color: var(--text-node); }
.children    { display: block; }
.collapsed-hint { color: var(--muted); font-size: 0.75rem; }
</style>
