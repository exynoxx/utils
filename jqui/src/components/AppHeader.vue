<template>
  <header>
    <div class="logo">jq<span>UI</span></div>
    <div class="subtitle">Transform Pipeline — build ordered filter / select / map steps with live tree preview</div>
  </header>
</template>

<style scoped>
header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 18px 32px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.logo {
  font-size: 1.5rem;
  font-weight: 800;
  letter-spacing: -0.5px;
  color: var(--accent2);
}
.logo span { color: var(--text); }
.subtitle { color: var(--muted); font-size: 0.82rem; }
</style>
