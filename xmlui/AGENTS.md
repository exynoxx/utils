# xmlui — Project Overview

Vue 3 + Vite app for interactive XML transformation. Three-column layout: input (left), document table viewer (middle), pipeline editor + download (right).

## Build

- `npm run dev` / `npm run build` — standard Vite dev/build (`vite.config.js`)
- `npm run build:single` — single self-contained HTML via `vite-plugin-singlefile` (`vite.single.config.js`, output: `dist-single/`)
- `@` alias → `src/`

After any changes to files in `xmlui/`, always run:
```
cd xmlui && npm run build
```
Verify the build exits with code 0 before considering the task complete.

## Entry

`index.html` → `src/main.js` → mounts `App.vue` + imports `styles/global.css`

## App.vue (root orchestrator)

Wires all three panels and all composables. Owns the download handler: calls `getOutputDocs()` → `rebuildTree()` → `treeToXml()` → `downloadXml()`. Handles global keyboard shortcut (Escape to deselect). Uses `useXmlFile`, `usePipeline`, `useToast`.

## Components

| Component | Panel | Role |
|---|---|---|
| `AppHeader.vue` | Top | Static banner ("XML Processor"). No props/emits. Orange accent. |
| `XmlInputPanel.vue` | Left | File upload/drag-drop zone, paste textarea, "Parse XML" button. Emits `update:rawInput`, `parse`, `drop`, `trigger-file`, `textarea-input`. Shows file name/size, load progress bar, parse errors. |
| `XmlDocViewer.vue` | Middle | Paginated table of flat documents (50/page). Checkbox per row for selection. Drag handle for row reorder (individual or multi-select group). Inline cell editing (click value → input). Exclude button per row. Column headers: click to sort, × to hide. Emits all interaction events up. |
| `XmlElementNode.vue` | Middle (recursive) | Renders a single XML element node: tag name, attributes (blue), text (purple), nested children. Collapse/expand at depth ≥ 3. Used for the raw tree view (not the main table). |
| `TransformPipeline.vue` | Right | Array path input with chip suggestions. Stats badge (total/filtered/excluded/selected). Sort controls (field + asc/desc + Sort All). Column visibility toggles. Draggable step list. Add Filter/Select/Map buttons. Selection actions. Download XML button. Wraps `PipelineStep` instances. |
| `PipelineStep.vue` | Right (per-step) | Expandable step card with drag handle, type badge, summary. Filter: expression textarea + field chips. Select: checkbox grid + manual input. Map: rule cards (add/delete/rename/conditional) with field datalist. |

## Composables

| Composable | State Owned | Role |
|---|---|---|
| `useXmlFile.js` | `rawInput`, `parsedData` (tree), `parseError`, `loadProgress`, `fileMeta` | File reading via `FileReader`, large-file truncation (`DISPLAY_SIZE_LIMIT=200KB`, `DISPLAY_CHARS=8000`), XML parsing via `parseXMLText`. Race-condition guard (`_readToken`). `onParsed(cb)` callback registration. |
| `usePipeline.js` | `arrayPath`, `pipelineSteps[]`, `excludedIds`, `selectedIds`, `docOrder`, `docEdits`, `hiddenColumns`, `sortConfig`, drag state | Core pipeline orchestrator. Extracts flat docs from XML tree via `evalXPath` + `elementToDoc`. Computes ordered/filtered/paginated page view. Step CRUD + drag-reorder. Document selection (click/ctrl/shift). Inline edits. Column visibility. Sort. Doc row drag-and-drop reorder. `getOutputDocs()` for download. |
| `useToast.js` | `toast` (reactive) | Timed toast notifications (3 s default). `showToast(msg, type, duration)`. `onScopeDispose` cleanup. |

## Utils

| Util | Exports | Role |
|---|---|---|
| `xmlUtils.js` | `parseXMLText`, `domToTree`, `evalXPath`, `getArrayPathSuggestions`, `elementToDoc`, `docToElement`, `treeToXml`, `rebuildTree`, `downloadXml` | XML parse (`DOMParser` → plain `{tag,attrs,children,text}` tree). Path evaluation for dot-notation expressions. Flat doc extraction (attributes as `@key`, child text as field name, one-level nesting as `parent.child`). Serialization back to indented XML. Tree rebuilding with modified docs for download. Browser download via Blob/anchor. |
| `pipelineEval.js` | `evaluateExpr`, `applyMapRule`, `parseColumns`, `applyStepJs`, `stepSummaryText` | Pure-JS pipeline evaluator. `evaluateExpr` uses dynamic `new Function` with `with(item)`. `applyStepJs` applies filter/select/map steps to a doc array. `stepSummaryText` generates collapsed step header text. |

## Data Flow

1. User inputs XML → `XmlInputPanel` → `useXmlFile` parses into `parsedData` (plain tree)
2. `usePipeline(parsedData)` watches parsed data; `arrayPath` fed to `evalXPath` → flat `baseDocuments[]`
3. `filterPassMap` computed: each doc tested against all filter steps for visual highlighting
4. `orderedIds` computed: respects `docOrder` (drag reorder), `sortConfig`, or natural order
5. `pageDocs` computed: current page slice with `_excluded`, `_filterPass`, `_selected` metadata merged
6. `XmlDocViewer` renders `pageDocs` as a table; all interactions emit events to `App.vue`
7. Download: `getOutputDocs()` → applies edits, order, exclusions, pipeline steps → `rebuildTree()` → `treeToXml()` → `downloadXml()`

## Pipeline Step Types

- **filter**: boolean condition expression against the flat doc object (e.g. `status === "active"`)
- **select**: choose which fields to include in output (column subset)
- **map**: rules applied per-doc — `add` (set field to expression), `delete` (remove field), `rename` (field → new name), `conditional` (if-then-else expression)

## Document Model

XML elements are flattened to plain JS objects:
- `@attrName` — XML attribute values
- `fieldName` — direct child element text content
- `parent.child` — grandchild element text (one level of nesting flattened)
- `_id` — internal integer, stripped before output

Example: `<item id="1"><name>Alice</name><address><city>NY</city></address></item>`
→ `{ _id: 0, "@id": "1", name: "Alice", "address.city": "NY" }`

## Constants (`constants.js`)

| Constant | Value | Purpose |
|---|---|---|
| `DOCS_PER_PAGE` | 50 | Documents shown per page in the viewer |
| `LARGE_FILE_ROWS` | 5000 | Threshold above which live evaluation is skipped |
| `PREVIEW_CAP` | 50 | Max rows for pipeline preview |
| `DISPLAY_SIZE_LIMIT` | 204800 | File size (bytes) above which textarea is truncated |
| `DISPLAY_CHARS` | 8000 | Characters shown in textarea for large files |
| `MAX_FIELD_SAMPLE` | 200 | Documents sampled to detect available fields |

## Styling

Dark theme via CSS custom properties in `styles/global.css`. Orange accent (`#f7896a` / `--accent`) distinguishes this from the jqui purple theme. XML value colors: `--tag` (pink, element names), `--attr-key` (blue, attribute names), `--attr-val` (green, attribute values), `--text-node` (purple, text content).
