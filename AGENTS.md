# Project Guidelines

## Build and Test

After making any changes to files in `jqui/`, always run:

```
cd jqui && npm run build
```

Verify the build exits with code 0 before considering the task complete.

## jqui — Project Overview

Vue 3 + Vite app for interactive JSON transformation via jq-like pipelines. Three-column layout: input (left), tree viewer (middle), pipeline editor (right).

### Build

- `npm run dev` / `npm run build` — standard Vite dev/build (`vite.config.js`)
- `npm run build:single` — single self-contained HTML via `vite-plugin-singlefile` (`vite.single.config.js`, output: `dist-single/`)
- `@` alias → `src/`

### Entry

- `index.html` → `src/main.js` → mounts `App.vue` + imports `styles/global.css`

### App.vue (root orchestrator)

Wires all three panels. Owns download logic: tries jq-web first, falls back to JS pipeline eval, rebuilds nested structure if array path ≠ root. Uses all three composables.

### Components

| Component | Panel | Role |
|---|---|---|
| `AppHeader.vue` | Top | Static banner, no props/emits |
| `JsonInputPanel.vue` | Left | File upload/drag-drop, paste textarea, parse button. Emits `update:rawInput`, `parse`, `file-change`, `drop`, `textarea-input` |
| `JsonTreeViewer.vue` | Middle | Renders original or transformed JSON via `TreeNode`. Toggles view, shows pipeline stats |
| `TreeNode.vue` | Middle (recursive) | Renders JSON nodes with collapse/expand + incremental child reveal (`RENDER_LIMIT_STEP=50`) |
| `TransformPipeline.vue` | Right | Pipeline controls, step list, jq command preview, download button. Wraps `PipelineStep` instances |
| `PipelineStep.vue` | Right (per-step) | Editable step card (filter/select/map), drag handle, jq preview snippet |

### Composables

| Composable | State Owned | Role |
|---|---|---|
| `useJsonFile.js` | `rawInput`, `parsedData`, `parseError`, `loadProgress`, `fileMeta` | File reading via FileReader, large-file truncation (`DISPLAY_SIZE_LIMIT=200KB`, `DISPLAY_CHARS=8000`), JSON parsing |
| `usePipeline.js` | `pipelineSteps`, `arrayPath`, `pipelineResult`, `pipelineStats`, drag state | Step CRUD, debounced evaluation, array-path suggestions, jq filter string assembly, preview capped to `PREVIEW_CAP=50` rows |
| `useToast.js` | `toast` (reactive) | Timed toast notifications |

### Utils

| Util | Exports | Role |
|---|---|---|
| `jqLoader.js` | `loadJq()` | Lazy singleton: injects jq-web script from CDN (`JQ_CDN` constant), resolves `window.jq` |
| `jsonUtils.js` | `evalSimpleJqPath()`, `downloadJson()` | Simple jq-like dot-path evaluation; browser JSON download via Blob/anchor |
| `pipelineEval.js` | `evaluateExpr`, `ruleToJq`, `applyRuleJs`, `parseColumns`, `stepToJq`, `applyStepJs` | Pure JS fallback evaluator for pipeline steps. Converts steps to jq strings (`stepToJq`/`ruleToJq`) and executes equivalent JS (`applyStepJs`/`applyRuleJs`). Uses dynamic `Function` for expression eval |

### Data Flow

1. User inputs JSON → `JsonInputPanel` → `useJsonFile` parses into `parsedData`
2. `usePipeline(parsedData)` watches parsed data, resolves array path via `evalSimpleJqPath`
3. Pipeline steps (filter/select/map) evaluated via `applyStepJs` with large-file caps (`LARGE_FILE_ROWS=5000`)
4. `JsonTreeViewer` displays `pipelineResult` or original `parsedData` through recursive `TreeNode`
5. Download: `App.vue` runs jq-web (if available) or JS fallback, then `downloadJson()`

### Pipeline Step Types

- **filter**: boolean condition expression → `select(expr)` in jq
- **select**: column subset, optional values-only for single column → `{col1,col2}` or `.[].col` in jq
- **map**: add/update/delete/conditional field rules → `. + {field: expr}` patterns in jq

### Constants (`constants.js`)

`RENDER_LIMIT_STEP=50`, `LARGE_FILE_ROWS=5000`, `PREVIEW_CAP=50`, `AUTO_COLLAPSE_SIZE=30`, `AUTO_COLLAPSE_DEPTH=3`, `DISPLAY_SIZE_LIMIT=204800`, `DISPLAY_CHARS=8000`, `JQ_CDN` (jq-web CDN URL)
