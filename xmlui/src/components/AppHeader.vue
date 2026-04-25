<template>
  <header class="app-header">
    <div class="logo">
      <span class="logo-icon">⚙</span>
      <span class="logo-text">XML Processor</span>
    </div>
    <div class="tagline">Filter · Transform · Export</div>
  </header>
</template>

<style scoped>
.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 28px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.logo {
  display: flex;
  align-items: center;
  gap: 10px;
}
.logo-icon {
  font-size: 1.3rem;
  color: var(--accent);
}
.logo-text {
  font-size: 1.1rem;
  font-weight: 700;
  color: var(--text);
  letter-spacing: 0.02em;
}
.tagline {
  font-size: 0.78rem;
  color: var(--muted);
  letter-spacing: 0.05em;
}
</style>
