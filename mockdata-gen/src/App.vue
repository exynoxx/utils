<template>
  <div class="app">
    <header class="app-header">
      <div class="header-inner">
        <div class="logo">
          <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
            <rect width="32" height="32" rx="8" fill="#6366f1"/>
            <path d="M8 10h16M8 16h10M8 22h13" stroke="white" stroke-width="2.5" stroke-linecap="round"/>
            <circle cx="23" cy="22" r="3" fill="white"/>
            <path d="M21.5 22l1 1 2-2" stroke="#6366f1" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
          <span>MockGen</span>
        </div>
        <p class="subtitle">Generate realistic test data as CSV, JSON, or XML in seconds</p>
      </div>
    </header>

    <main class="main-content">
      <!-- Step 1: Column Type Palette -->
      <section class="card">
        <div class="card-header">
          <div class="step-badge">1</div>
          <div>
            <h2>Define Columns</h2>
            <p class="card-hint">Click a type to add it as a column. Drag columns to reorder.</p>
          </div>
        </div>

        <div class="type-palette">
          <button
            v-for="type in columnTypes"
            :key="type.id"
            class="type-pill"
            :style="{ '--pill-color': type.color }"
            @click="addColumn(type)"
            :title="type.description"
          >
            <span class="pill-icon">{{ type.icon }}</span>
            <span class="pill-label">{{ type.label }}</span>
          </button>
        </div>

        <!-- Selected columns -->
        <div v-if="selectedColumns.length" class="columns-area">
          <div class="columns-label">
            <span>Selected columns ({{ selectedColumns.length }})</span>
            <button class="btn-ghost-sm" @click="selectedColumns.length = 0; selectedColumns.splice(0)">Clear all</button>
          </div>
          <div class="columns-list">
            <div
              v-for="(col, idx) in selectedColumns"
              :key="col.uid"
              class="column-chip"
              :style="{ '--chip-color': col.color }"
              :draggable="true"
              @dragstart="dragStart(idx)"
              @dragover.prevent="dragOver(idx)"
              @dragend="dragEnd"
            >
              <span class="chip-drag">⠿</span>
              <span class="chip-icon">{{ col.icon }}</span>
              <input
                class="chip-name"
                v-model="col.name"
                :placeholder="col.defaultName"
                @click.stop
              />
              <span class="chip-type-label">{{ col.label }}</span>

              <!-- DateTime format picker -->
              <select
                v-if="col.id === 'datetime'"
                class="chip-select"
                v-model="col.datetimeFormat"
                @click.stop
                title="Date/time format"
              >
                <option v-for="fmt in datetimeFormats" :key="fmt.value" :value="fmt.value">{{ fmt.label }}</option>
              </select>
              <input
                v-if="col.id === 'datetime' && col.datetimeFormat === 'custom'"
                class="chip-input chip-input-custom"
                v-model="col.datetimeCustom"
                placeholder="yyyy-MM-dd HH:mm:ss"
                @click.stop
                title="C# format tokens: yyyy yy MM M dd d HH H hh h mm m ss s fff tt"
              />

              <!-- Enum custom values -->
              <input
                v-if="col.id === 'enum'"
                class="chip-input"
                v-model="col.enumValues"
                placeholder="val1,val2,val3"
                @click.stop
                title="Comma-separated values"
              />

              <!-- Number range -->
              <template v-if="col.id === 'integer' || col.id === 'float'">
                <input class="chip-input chip-input-sm" type="number" v-model.number="col.min" placeholder="min" @click.stop title="Min" />
                <span class="chip-sep">–</span>
                <input class="chip-input chip-input-sm" type="number" v-model.number="col.max" placeholder="max" @click.stop title="Max" />
              </template>

              <!-- Phone country -->
              <select
                v-if="col.id === 'phone'"
                class="chip-select"
                v-model="col.phoneCountry"
                @click.stop
                title="Phone number format"
              >
                <option value="random">Random</option>
                <option value="dk">Denmark (+45)</option>
                <option value="de">Germany (+49)</option>
              </select>

              <!-- Boolean format -->
              <select v-if="col.id === 'boolean'" class="chip-select" v-model="col.boolFormat" @click.stop>
                <option value="true/false">true / false</option>
                <option value="1/0">1 / 0</option>
                <option value="yes/no">yes / no</option>
                <option value="Yes/No">Yes / No</option>
              </select>

              <button class="chip-remove" @click.stop="removeColumn(idx)" title="Remove">×</button>
            </div>
          </div>
        </div>

        <div v-else class="empty-columns">
          <span>No columns yet — click a type above to get started</span>
        </div>
      </section>

      <!-- Step 2: Generation Settings -->
      <section class="card" :class="{ disabled: !selectedColumns.length }">
        <div class="card-header">
          <div class="step-badge">2</div>
          <div>
            <h2>Configure &amp; Generate</h2>
            <p class="card-hint">Set your output preferences and download.</p>
          </div>
        </div>

        <div class="settings-row">
          <div class="setting-group">
            <label>Rows</label>
            <div class="row-input-wrap">
              <input
                type="number"
                v-model.number="rowCount"
                min="1"
                class="row-input"
                placeholder="1000"
              />
              <div class="row-presets">
                <button v-for="n in rowPresets" :key="n" class="preset-btn" :class="{ active: rowCount === n }" @click="rowCount = n">{{ formatNumber(n) }}</button>
              </div>
            </div>
          </div>

          <div class="setting-group">
            <label>File Format</label>
            <div class="format-tabs">
              <button
                v-for="fmt in fileFormats"
                :key="fmt.value"
                class="format-tab"
                :class="{ active: fileFormat === fmt.value }"
                @click="fileFormat = fmt.value"
              >
                <span>{{ fmt.icon }}</span>
                <span>{{ fmt.label }}</span>
              </button>
            </div>
          </div>

          <div v-if="fileFormat === 'csv'" class="setting-group">
            <label>CSV Delimiter</label>
            <div class="format-tabs">
              <button v-for="d in delimiters" :key="d.value" class="format-tab" :class="{ active: csvDelimiter === d.value }" @click="csvDelimiter = d.value">{{ d.label }}</button>
            </div>
          </div>

          <div v-if="fileFormat === 'json'" class="setting-group">
            <label>JSON Format</label>
            <div class="format-tabs">
              <button class="format-tab" :class="{ active: jsonFormat === 'array' }" @click="jsonFormat = 'array'">Array</button>
              <button class="format-tab" :class="{ active: jsonFormat === 'lines' }" @click="jsonFormat = 'lines'">Lines (NDJSON)</button>
            </div>
          </div>

          <div v-if="fileFormat === 'xml'" class="setting-group">
            <label>XML Root Element</label>
            <input class="row-input" v-model="xmlRootElement" placeholder="records" style="width:140px" />
          </div>

          <div v-if="fileFormat === 'xml'" class="setting-group">
            <label>XML Row Element</label>
            <input class="row-input" v-model="xmlRowElement" placeholder="record" style="width:140px" />
          </div>

          <div v-if="fileFormat === 'xml'" class="setting-group">
            <label>XML Value Mode</label>
            <div class="format-tabs">
              <button class="format-tab" :class="{ active: xmlMode === 'elements' }" @click="xmlMode = 'elements'">Elements</button>
              <button class="format-tab" :class="{ active: xmlMode === 'attributes' }" @click="xmlMode = 'attributes'">Attributes</button>
            </div>
          </div>
        </div>

        <div class="generate-area">
          <button
            class="btn-generate"
            :disabled="!selectedColumns.length || !rowCount || rowCount < 1 || generating"
            @click="generate"
          >
            <span v-if="generating" class="spinner"></span>
            <span v-else>⬇</span>
            {{ generating ? `Generating ${formatNumber(generatedRows)} / ${formatNumber(rowCount)}…` : 'Generate &amp; Download' }}
          </button>
          <div v-if="lastFileInfo" class="file-info">
            <span class="file-info-icon">✓</span> Generated <strong>{{ lastFileInfo.filename }}</strong> — {{ lastFileInfo.size }}
          </div>
        </div>
      </section>

      <!-- Preview -->
      <section v-if="selectedColumns.length" class="card">
        <div class="card-header">
          <div class="step-badge">↗</div>
          <div>
            <h2>Live Preview <span class="preview-note">(first {{ preview.length }} rows)</span></h2>
          </div>
        </div>
        <div class="table-wrap">
          <table class="preview-table">
            <thead>
              <tr>
                <th v-for="col in selectedColumns" :key="col.uid">{{ col.name || col.defaultName }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, ri) in preview" :key="ri">
                <td v-for="col in selectedColumns" :key="col.uid">{{ row[col.name || col.defaultName] }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </main>

    <footer class="app-footer">
      MockGen — all data generated client-side, nothing leaves your browser
    </footer>
  </div>
</template>

<script setup>
import { ref, reactive, watch } from 'vue'

/* ─────────────────────────────────────────────
   Column type definitions
───────────────────────────────────────────── */
// Shared colors for grouped column types
const C_DATETIME = '#10b981' // datetime, date, time, timestamp
const C_GEO      = '#b45309' // country, city, address, longitude, latitude
const C_NETWORK  = '#14b8a6' // ipv4, ipv6
const C_WEB      = '#0891b2' // url, mime, useragent
const C_MONEY    = '#ca8a04' // currency, price
const C_TEXT     = '#9333ea' // language, lorem, word, sentence, paragraph
const C_HASH     = '#64748b' // md5, sha1, sha256

const columnTypes = [
  { id: 'id',          label: 'Auto ID',       icon: '🔢', color: '#6366f1',   description: 'Sequential integer ID starting from 1' },
  { id: 'uuid',        label: 'UUID',           icon: '🪪', color: '#a855f7',   description: 'Random UUID v4' },
  { id: 'integer',     label: 'Integer',        icon: '#',  color: '#3b82f6',   description: 'Random integer in range' },
  { id: 'float',       label: 'Float',          icon: '~',  color: '#0ea5e9',   description: 'Random decimal number' },
  { id: 'boolean',     label: 'Boolean',        icon: '☑',  color: '#eab308',   description: 'True / false value' },
  { id: 'datetime',    label: 'DateTime',       icon: '📅', color: C_DATETIME,  description: 'Random date/time' },
  { id: 'date',        label: 'Date',           icon: '📆', color: C_DATETIME,  description: 'Random date (YYYY-MM-DD)' },
  { id: 'time',        label: 'Time',           icon: '⏰', color: C_DATETIME,  description: 'Random time (HH:MM:SS)' },
  { id: 'timestamp',   label: 'Timestamp',      icon: '⏱',  color: C_DATETIME,  description: 'Unix timestamp (seconds)' },
  { id: 'firstname',   label: 'First Name',     icon: '👤', color: '#f97316',   description: 'Random first name' },
  { id: 'lastname',    label: 'Last Name',      icon: '👥', color: '#fb923c',   description: 'Random last name' },
  { id: 'fullname',    label: 'Full Name',      icon: '🙋', color: '#ea580c',   description: 'Random full name' },
  { id: 'gender',      label: 'Gender',         icon: '⚥',  color: '#ec4899',   description: 'Male / Female / Other' },
  { id: 'email',       label: 'Email',          icon: '✉',  color: '#ef4444',   description: 'Random email address' },
  { id: 'phone',       label: 'Phone',          icon: '📞', color: '#e11d48',   description: 'Random phone number' },
  { id: 'address',     label: 'Address',        icon: '🏠', color: C_GEO,       description: 'Random street address' },
  { id: 'city',        label: 'City',           icon: '🏙', color: C_GEO,       description: 'Random city name' },
  { id: 'country',     label: 'Country',        icon: '🌍', color: C_GEO,       description: 'Random country name' },
  { id: 'zipcode',     label: 'Zip Code',       icon: '📮', color: '#a16207',   description: 'Random postal / zip code' },
  { id: 'company',     label: 'Company',        icon: '🏢', color: '#84cc16',   description: 'Random company name' },
  { id: 'url',         label: 'URL',            icon: '🔗', color: C_WEB,       description: 'Random URL' },
  { id: 'mime',        label: 'MIME Type',      icon: '📁', color: C_WEB,       description: 'Random MIME type' },
  { id: 'useragent',   label: 'User Agent',     icon: '🤖', color: C_WEB,       description: 'Random browser user-agent' },
  { id: 'ipv4',        label: 'IPv4',           icon: '🌐', color: C_NETWORK,   description: 'Random IPv4 address' },
  { id: 'ipv6',        label: 'IPv6',           icon: '🌐', color: C_NETWORK,   description: 'Random IPv6 address' },
  { id: 'color_hex',   label: 'Color (hex)',    icon: '🎨', color: '#db2777',   description: 'Random hex color code' },
  { id: 'language',    label: 'Language',       icon: '🔤', color: C_TEXT,      description: 'Random language name' },
  { id: 'lorem',       label: 'Lorem Text',     icon: '📝', color: C_TEXT,      description: 'Random lorem ipsum sentence' },
  { id: 'word',        label: 'Word',           icon: 'Aa', color: C_TEXT,      description: 'Single random word' },
  { id: 'sentence',    label: 'Sentence',       icon: '💬', color: C_TEXT,      description: 'Random sentence' },
  { id: 'paragraph',   label: 'Paragraph',      icon: '📄', color: C_TEXT,      description: 'Random paragraph' },
  { id: 'enum',        label: 'Enum',           icon: '📋', color: '#06b6d4',   description: 'Pick from custom list' },
  { id: 'job',         label: 'Job Title',      icon: '💼', color: '#65a30d',   description: 'Random job title' },
  { id: 'currency',    label: 'Currency',       icon: '💰', color: C_MONEY,     description: 'Random currency code' },
  { id: 'price',       label: 'Price',          icon: '$',  color: C_MONEY,     description: 'Random price value' },
  { id: 'creditcard',  label: 'Credit Card',    icon: '💳', color: '#1d4ed8',   description: 'Random credit card number' },
  { id: 'iban',        label: 'IBAN',           icon: '🏦', color: '#2563eb',   description: 'Random IBAN' },
  { id: 'latitude',    label: 'Latitude',       icon: '📍', color: C_GEO,       description: 'Random latitude' },
  { id: 'longitude',   label: 'Longitude',      icon: '📌', color: C_GEO,       description: 'Random longitude' },
  { id: 'status',      label: 'Status',         icon: '🚦', color: '#22c55e',   description: 'active/inactive/pending/etc.' },
  { id: 'md5',         label: 'MD5 Hash',       icon: '🔑', color: C_HASH,      description: 'Random 32-char hex hash' },
  { id: 'sha1',        label: 'SHA-1 Hash',     icon: '🔑', color: C_HASH,      description: 'Random 40-char hex hash' },
  { id: 'sha256',      label: 'SHA-256 Hash',   icon: '🔑', color: C_HASH,      description: 'Random 64-char hex hash' },
  { id: 'base64',      label: 'Base64',         icon: '🧬', color: C_HASH,      description: 'Base64-encoded full name' },
]

const datetimeFormats = [
  { label: 'ISO 8601 (2024-04-13T14:30:00Z)',      value: 'iso' },
  { label: 'ISO date+time (2024-04-13 14:30:00)',  value: 'iso_space' },
  { label: 'Date only (2024-04-13)',               value: 'date' },
  { label: 'EU (13/04/2024 14:30)',                value: 'eu' },
  { label: 'US (04/13/2024 2:30 PM)',              value: 'us' },
  { label: 'US date (04/13/2024)',                 value: 'us_date' },
  { label: 'Unix timestamp (s)',                   value: 'unix_s' },
  { label: 'Unix timestamp (ms)',                  value: 'unix_ms' },
  { label: 'RFC 2822',                             value: 'rfc2822' },
  { label: 'Long (April 13, 2024 2:30:00 PM)',     value: 'long' },
  { label: 'Short (Apr 13, 2024)',                 value: 'short' },
  { label: 'Year-Month (2024-04)',                 value: 'yearmonth' },
  { label: 'Month/Year (04/2024)',                 value: 'monthyear' },
  { label: 'Custom (C# style)…',                  value: 'custom' },
]

const fileFormats = [
  { value: 'csv',  label: 'CSV',  icon: '📊' },
  { value: 'json', label: 'JSON', icon: '{ }' },
  { value: 'xml',  label: 'XML',  icon: '</>' },
]

const delimiters = [
  { value: ',',  label: 'Comma (,)' },
  { value: ';',  label: 'Semicolon (;)' },
  { value: '\t', label: 'Tab (⇥)' },
  { value: '|',  label: 'Pipe (|)' },
]

const rowPresets = [10, 100, 1000, 10000, 100000]

/* ─────────────────────────────────────────────
   Reactive state
───────────────────────────────────────────── */
let uidCounter = 0
const selectedColumns = reactive([])
const rowCount = ref(100)
const fileFormat = ref('csv')
const csvDelimiter = ref(',')
const jsonFormat = ref('array')
const xmlRootElement = ref('records')
const xmlRowElement = ref('record')
const xmlMode = ref('elements')
const generating = ref(false)
const generatedRows = ref(0)
const lastFileInfo = ref(null)
const preview = reactive([])
const dragIdx = ref(null)
const PREVIEW_ROWS = 10

function refreshPreview() {
  preview.splice(0)
  if (!selectedColumns.length) return
  for (let i = 0; i < PREVIEW_ROWS; i++) {
    const obj = {}
    selectedColumns.forEach(c => { obj[colName(c)] = generateValue(c, i) })
    preview.push(obj)
  }
}

let _previewTimer = null
function debouncedRefreshPreview() {
  clearTimeout(_previewTimer)
  _previewTimer = setTimeout(refreshPreview, 150)
}
watch(selectedColumns, debouncedRefreshPreview, { deep: true, immediate: false })

/* ─────────────────────────────────────────────
   Column management
───────────────────────────────────────────── */
function addColumn(type) {
  const count = selectedColumns.filter(c => c.id === type.id).length
  selectedColumns.push({
    ...type,
    uid: ++uidCounter,
    name: '',
    defaultName: count === 0 ? type.label : `${type.label} ${count + 1}`,
    datetimeFormat: 'custom',
    datetimeCustom: 'yyyy-MM-dd HH:mm:ss',
    enumValues: '',
    phoneCountry: 'random',
    boolFormat: 'true/false',
    min: 0,
    max: type.id === 'float' ? 1000 : 1000,
  })
}

function removeColumn(idx) {
  selectedColumns.splice(idx, 1)
}

/* ─────────────────────────────────────────────
   Drag-to-reorder
───────────────────────────────────────────── */
function dragStart(idx) { dragIdx.value = idx }
function dragOver(idx) {
  if (dragIdx.value === null || dragIdx.value === idx) return
  const item = selectedColumns.splice(dragIdx.value, 1)[0]
  selectedColumns.splice(idx, 0, item)
  dragIdx.value = idx
}
function dragEnd() { dragIdx.value = null }

/* ─────────────────────────────────────────────
   Data generators
───────────────────────────────────────────── */
const firstNames = ['James','Olivia','Liam','Emma','Noah','Ava','William','Sophia','Benjamin','Isabella','Lucas','Mia','Henry','Charlotte','Alexander','Amelia','Michael','Harper','Ethan','Evelyn','Daniel','Abigail','Matthew','Emily','Aiden','Ella','Jackson','Elizabeth','Sebastian','Camila','Jack','Luna','Owen','Sofia','Samuel','Aria','Ryan','Scarlett','Nathan','Victoria','Dylan','Madison','Leo','Layla','Isaac','Penelope','John','Chloe','David','Riley']
const lastNames = ['Smith','Johnson','Williams','Brown','Jones','Garcia','Miller','Davis','Rodriguez','Martinez','Hernandez','Lopez','Gonzalez','Wilson','Anderson','Thomas','Taylor','Moore','Jackson','Martin','Lee','Perez','Thompson','White','Harris','Sanchez','Clark','Ramirez','Lewis','Robinson','Walker','Young','Allen','King','Wright','Scott','Torres','Nguyen','Hill','Flores','Green','Adams','Nelson','Baker','Hall','Rivera','Campbell','Mitchell','Carter','Roberts']
const cities = ['New York','Los Angeles','Chicago','Houston','Phoenix','Philadelphia','San Antonio','San Diego','Dallas','San Jose','Austin','Jacksonville','Fort Worth','Columbus','Charlotte','Indianapolis','San Francisco','Seattle','Denver','Washington','Nashville','Oklahoma City','El Paso','Las Vegas','Boston','Portland','Memphis','Louisville','Baltimore','Milwaukee','Tokyo','London','Paris','Berlin','Madrid','Rome','Amsterdam','Sydney','Toronto','São Paulo','Mumbai','Shanghai','Beijing','Dubai','Singapore','Cape Town','Istanbul','Seoul','Mexico City','Buenos Aires']
const countries = ['United States','United Kingdom','Canada','Australia','Germany','France','Italy','Spain','Netherlands','Sweden','Norway','Denmark','Finland','Switzerland','Japan','China','India','Brazil','Mexico','Argentina','South Africa','Nigeria','Egypt','Turkey','Saudi Arabia','United Arab Emirates','Singapore','South Korea','New Zealand','Poland','Austria','Belgium','Portugal','Czech Republic','Hungary','Romania','Greece','Thailand','Vietnam','Indonesia','Philippines','Malaysia','Pakistan','Bangladesh','Ukraine','Russia','Colombia','Chile','Peru','Venezuela']
const companies = ['Acme Corp','Globex','Initech','Umbrella','Soylent','Massive Dynamic','Aperture Science','Cyberdyne','Weyland Corp','Tyrell Corp','Buy n Large','Rekall','LexCorp','Oscorp','Stark Industries','Wayne Enterprises','Dharma Initiative','Virtucon','Praxis Corp','Veridian Dynamics','Bluth Company','Dunder Mifflin','Vandelay Industries','Prestige Worldwide','Wonka Industries','Wernham Hogg','Black Mesa','Multi Corp','InGen','OCP','Omni Consumer','Nakatomi Trading','Oceanic Airlines','Gekko & Co','Sterling Cooper','Pearson Specter','Wolfram & Hart','Tech Innovations','X Corp','Initech Plus']
const tlds = ['.com','.net','.org','.io','.co','.dev','.app','.tech']
const companiesSanitizedEmail = companies.map(c => c.toLowerCase().replace(/[^a-z]/g, '').slice(0, 10))
const companiesSanitizedUrl   = companies.map(c => c.toLowerCase().replace(/[^a-z]/g, '').slice(0, 12))
const streets =['Main St','Oak Ave','Maple Dr','Cedar Ln','Pine Rd','Elm St','Washington Blvd','Park Ave','Lake Dr','Hill Rd','River Rd','Forest Ave','Sunset Blvd','Broadway','Fifth Ave','Bourbon St','Baker St','Abbey Rd','Penny Lane','Wall St','Madison Ave','Lincoln Ave','Jefferson St','Highland Ave','Crescent Rd','Summit Dr','Valley Rd','Harbor Blvd','Ocean Dr','Bayview Ave']
const words = ['apple','banana','castle','dragon','electric','freedom','galaxy','horizon','island','jungle','kitten','lantern','mountain','nebula','ocean','puzzle','quantum','rainbow','silver','thunder','umbrella','valley','whisper','zenith','artifact','beacon','compass','delta','ember','falcon','glacier','harbor','ivory','jasmine','karma','labyrinth','magic','notion']
const sentences = ['The quick brown fox jumps over the lazy dog.','Pack my box with five dozen liquor jugs.','How vexingly quick daft zebras jump!','The five boxing wizards jump quickly.','Sphinx of black quartz, judge my vow.','Bright vixens jump dozy fowl quack.','We promptly judged antique ivory buckles for the next prize.','A wizard\'s job is to vex chumps quickly in fog.','Crazy Fredrick bought many very exquisite opal jewels.','The job requires extra pluck and zeal from every young wage earner.','Lorem ipsum dolor sit amet consectetur adipiscing elit.','Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.','Ut enim ad minim veniam quis nostrud exercitation ullamco.','Duis aute irure dolor in reprehenderit in voluptate velit esse.','Excepteur sint occaecat cupidatat non proident sunt in culpa.']
const jobs = ['Software Engineer','Product Manager','Data Scientist','UX Designer','DevOps Engineer','QA Analyst','Business Analyst','Project Manager','Full Stack Developer','Backend Developer','Frontend Developer','Mobile Developer','Security Engineer','Cloud Architect','Database Administrator','Machine Learning Engineer','Systems Analyst','Technical Writer','Scrum Master','Solution Architect','CTO','CEO','CFO','VP of Engineering','Director of Product','Engineering Manager','Data Analyst','Customer Success Manager','Sales Engineer','Marketing Manager']
const currencies = ['USD','EUR','GBP','JPY','CHF','CAD','AUD','CNY','HKD','NZD','SEK','DKK','NOK','SGD','KRW','MXN','BRL','INR','RUB','ZAR','TRY','PLN','CZK','HUF','RON']
const statuses = ['active','inactive','pending','archived','suspended','draft','published','deleted','processing','completed','failed','cancelled','approved','rejected','on_hold']
const languages = ['English','Spanish','French','German','Italian','Portuguese','Dutch','Russian','Japanese','Korean','Chinese','Arabic','Hindi','Bengali','Urdu','Turkish','Polish','Swedish','Danish','Finnish','Norwegian','Czech','Hungarian','Romanian','Greek','Hebrew','Malay','Indonesian','Thai','Vietnamese']
const mimeTypes = ['text/plain','text/html','text/css','text/javascript','application/json','application/xml','application/pdf','application/zip','application/gzip','application/octet-stream','image/jpeg','image/png','image/gif','image/svg+xml','image/webp','audio/mpeg','audio/wav','video/mp4','video/webm','font/woff2','multipart/form-data']
const userAgents = [
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36',
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36',
  'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36',
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:125.0) Gecko/20100101 Firefox/125.0',
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 14.4; rv:125.0) Gecko/20100101 Firefox/125.0',
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4_1) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4.1 Safari/605.1.15',
  'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4.1 Mobile/15E148 Safari/604.1',
  'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36',
]

function rand(min, max) { return Math.floor(Math.random() * (max - min + 1)) + min }
function pick(arr) { return arr[Math.floor(Math.random() * arr.length)] }
function randFloat(min, max, decimals = 2) { return parseFloat((Math.random() * (max - min) + min).toFixed(decimals)) }

function randDate(start = new Date(2000, 0, 1), end = new Date()) {
  return new Date(start.getTime() + Math.random() * (end.getTime() - start.getTime()))
}

function pad2(n) { return String(n).padStart(2, '0') }

function formatDatetime(d, fmt, customPattern) {
  const Y = d.getFullYear()
  const M = pad2(d.getMonth() + 1)
  const D = pad2(d.getDate())
  const h = pad2(d.getHours())
  const m = pad2(d.getMinutes())
  const s = pad2(d.getSeconds())
  const h12 = d.getHours() % 12 || 12
  const ampm = d.getHours() >= 12 ? 'PM' : 'AM'
  const months = ['January','February','March','April','May','June','July','August','September','October','November','December']
  const monthsShort = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec']
  switch (fmt) {
    case 'iso':        return `${Y}-${M}-${D}T${h}:${m}:${s}Z`
    case 'iso_space':  return `${Y}-${M}-${D} ${h}:${m}:${s}`
    case 'date':       return `${Y}-${M}-${D}`
    case 'eu':         return `${D}/${M}/${Y} ${h}:${m}`
    case 'us':         return `${M}/${D}/${Y} ${h12}:${m} ${ampm}`
    case 'us_date':    return `${M}/${D}/${Y}`
    case 'unix_s':     return String(Math.floor(d.getTime() / 1000))
    case 'unix_ms':    return String(d.getTime())
    case 'rfc2822':    return d.toUTCString()
    case 'long':       return `${months[d.getMonth()]} ${D}, ${Y} ${h12}:${m}:${s} ${ampm}`
    case 'short':      return `${monthsShort[d.getMonth()]} ${D}, ${Y}`
    case 'yearmonth':  return `${Y}-${M}`
    case 'monthyear':  return `${M}/${Y}`
    case 'custom':     return formatDatetimeCustom(d, customPattern || 'yyyy-MM-dd HH:mm:ss')
    default:           return d.toISOString()
  }
}

// Compiles a C#-style datetime format string once per unique pattern; cached for reuse.
const _patternCache = new Map()
function compileDatetimePattern(pattern) {
  if (_patternCache.has(pattern)) return _patternCache.get(pattern)
  const tokenDefs = [
    ['yyyy',    d => String(d.getFullYear())],
    ['yyy',     d => String(d.getFullYear())],
    ['yy',      d => String(d.getFullYear()).slice(-2)],
    ['y',       d => String(d.getFullYear() % 100)],
    ['MMMM',    d => ['January','February','March','April','May','June','July','August','September','October','November','December'][d.getMonth()]],
    ['MMM',     d => ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'][d.getMonth()]],
    ['MM',      d => String(d.getMonth() + 1).padStart(2, '0')],
    ['M',       d => String(d.getMonth() + 1)],
    ['dddd',    d => ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'][d.getDay()]],
    ['ddd',     d => ['Sun','Mon','Tue','Wed','Thu','Fri','Sat'][d.getDay()]],
    ['dd',      d => String(d.getDate()).padStart(2, '0')],
    ['d',       d => String(d.getDate())],
    ['HH',      d => String(d.getHours()).padStart(2, '0')],
    ['H',       d => String(d.getHours())],
    ['hh',      d => String(d.getHours() % 12 || 12).padStart(2, '0')],
    ['h',       d => String(d.getHours() % 12 || 12)],
    ['mm',      d => String(d.getMinutes()).padStart(2, '0')],
    ['m',       d => String(d.getMinutes())],
    ['ss',      d => String(d.getSeconds()).padStart(2, '0')],
    ['s',       d => String(d.getSeconds())],
    ['fffffff', d => String(d.getMilliseconds()).padStart(3, '0') + '0000'],
    ['ffffff',  d => String(d.getMilliseconds()).padStart(3, '0') + '000'],
    ['fffff',   d => String(d.getMilliseconds()).padStart(3, '0') + '00'],
    ['ffff',    d => String(d.getMilliseconds()).padStart(3, '0') + '0'],
    ['fff',     d => String(d.getMilliseconds()).padStart(3, '0')],
    ['ff',      d => String(d.getMilliseconds()).padStart(3, '0').slice(0, 2)],
    ['f',       d => String(d.getMilliseconds()).padStart(3, '0').slice(0, 1)],
    ['tt',      d => d.getHours() >= 12 ? 'PM' : 'AM'],
    ['t',       d => d.getHours() >= 12 ? 'P' : 'A'],
    ['TT',      d => d.getHours() >= 12 ? 'pm' : 'am'],
    ['T',       d => d.getHours() >= 12 ? 'p' : 'a'],
    ['K',       () => 'Z'],
    ['z',       () => '+0'],
    ['zz',      () => '+00'],
    ['zzz',     () => '+00:00'],
  ]
  const steps = []
  let i = 0
  while (i < pattern.length) {
    if (pattern[i] === "'") {
      let j = i + 1
      while (j < pattern.length && pattern[j] !== "'") j++
      const lit = pattern.slice(i + 1, j)
      steps.push(() => lit)
      i = j + 1
      continue
    }
    let matched = false
    for (const [token, fn] of tokenDefs) {
      if (pattern.startsWith(token, i)) {
        steps.push(fn)
        i += token.length
        matched = true
        break
      }
    }
    if (!matched) {
      const ch = pattern[i]
      steps.push(() => ch)
      i++
    }
  }
  _patternCache.set(pattern, steps)
  return steps
}
function formatDatetimeCustom(d, pattern) {
  const steps = compileDatetimePattern(pattern || 'yyyy-MM-dd HH:mm:ss')
  let s = ''
  for (let i = 0; i < steps.length; i++) s += steps[i](d)
  return s
}

function randUUID() {
  // crypto.randomUUID() is ~5–10× faster than the regex-replace approach
  return crypto.randomUUID()
}

function randIP4() { return `${rand(1,254)}.${rand(0,255)}.${rand(0,255)}.${rand(1,254)}` }
function randIP6() {
  let s = ''
  for (let i = 0; i < 8; i++) {
    if (i) s += ':'
    s += (Math.random() * 65536 | 0).toString(16).padStart(4, '0')
  }
  return s
}
function randHex(len) {
  let s = ''
  for (let i = 0; i < len; i++) s += (Math.random() * 16 | 0).toString(16)
  return s
}
function randBase64(text) {
  const bytes = new TextEncoder().encode(text)
  let bin = ''
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i])
  return btoa(bin)
}

function randPhone(country = 'random') {
  if (country === 'dk') {
    return `+45 ${rand(10,99)} ${rand(10,99)} ${rand(10,99)} ${rand(10,99)}`
  }
  if (country === 'de') {
    return `+49 ${rand(100,999)} ${rand(1000000,9999999)}`
  }
  const formats = [
    `+1 (${rand(200,999)}) ${rand(200,999)}-${rand(1000,9999)}`,
    `+44 ${rand(1000,9999)} ${rand(100000,999999)}`,
    `+49 ${rand(100,999)} ${rand(1000000,9999999)}`,
    `(${rand(200,999)}) ${rand(200,999)}-${rand(1000,9999)}`,
  ]
  return pick(formats)
}

function randCreditCard() {
  const prefixes = ['4','5','37','6011']
  const pre = pick(prefixes)
  let num = pre
  while (num.length < 16) num += rand(0,9)
  return num.match(/.{1,4}/g).join(' ')
}

function randIBAN() {
  const cc_list = ['DE','GB','FR','NL','IT','ES','CH','AT','BE','PL']
  const cc = pick(cc_list)
  const check = rand(10,99)
  let bban = ''
  for (let i = 0; i < 16; i++) bban += rand(0, 9)
  return `${cc}${check}${bban}`
}

function randLorem(cnt = 2) {
  const result = []
  for (let i = 0; i < cnt; i++) result.push(pick(sentences))
  return result.join(' ')
}

function generateValue(col, rowIdx) {
  switch (col.id) {
    case 'id':         return rowIdx + 1
    case 'uuid':       return randUUID()
    case 'integer': {
      const mn = col.min !== '' && col.min !== undefined ? Number(col.min) : 0
      const mx = col.max !== '' && col.max !== undefined ? Number(col.max) : 1000
      return rand(mn, mx)
    }
    case 'float': {
      const mn = col.min !== '' && col.min !== undefined ? Number(col.min) : 0
      const mx = col.max !== '' && col.max !== undefined ? Number(col.max) : 1000
      return randFloat(mn, mx)
    }
    case 'boolean': {
      const val = Math.random() < 0.5
      switch (col.boolFormat) {
        case '1/0':    return val ? 1 : 0
        case 'yes/no': return val ? 'yes' : 'no'
        case 'Yes/No': return val ? 'Yes' : 'No'
        default:       return val
      }
    }
    case 'datetime':   return formatDatetime(randDate(), col.datetimeFormat || 'custom', col.datetimeCustom)
    case 'date':       return formatDatetime(randDate(), 'date')
    case 'time':       return `${pad2(rand(0,23))}:${pad2(rand(0,59))}:${pad2(rand(0,59))}`
    case 'timestamp':  return Math.floor(randDate().getTime() / 1000)
    case 'firstname':  return pick(firstNames)
    case 'lastname':   return pick(lastNames)
    case 'fullname':   return `${pick(firstNames)} ${pick(lastNames)}`
    case 'email': {
      const fn = pick(firstNames).toLowerCase()
      const ln = pick(lastNames).toLowerCase()
      return `${fn}.${ln}@${pick(companiesSanitizedEmail)}${pick(tlds)}`
    }
    case 'phone':      return randPhone(col.phoneCountry || 'random')
    case 'address':    return `${rand(1,9999)} ${pick(streets)}`
    case 'city':       return pick(cities)
    case 'country':    return pick(countries)
    case 'zipcode':    return String(rand(10000,99999))
    case 'company':    return pick(companies)
    case 'url': {
      const paths = ['','about','products','contact','blog','docs','api']
      return `https://www.${pick(companiesSanitizedUrl)}${pick(tlds)}/${pick(paths)}`
    }
    case 'ipv4':       return randIP4()
    case 'ipv6':       return randIP6()
    case 'useragent':  return pick(userAgents)
    case 'color_hex':  return `#${randHex(6)}`
    case 'lorem':      return randLorem(rand(1,3))
    case 'word':       return pick(words)
    case 'sentence':   return pick(sentences)
    case 'paragraph':  return randLorem(rand(3,6))
    case 'enum': {
      const vals = col.enumValues ? col.enumValues.split(',').map(v => v.trim()).filter(Boolean) : ['A','B','C']
      return pick(vals)
    }
    case 'gender':     return pick(['Male','Female','Non-binary','Other','Prefer not to say'])
    case 'job':        return pick(jobs)
    case 'currency':   return pick(currencies)
    case 'price':      return randFloat(0.99, 9999.99, 2)
    case 'creditcard': return randCreditCard()
    case 'iban':       return randIBAN()
    case 'latitude':   return randFloat(-90, 90, 6)
    case 'longitude':  return randFloat(-180, 180, 6)
    case 'status':     return pick(statuses)
    case 'language':   return pick(languages)
    case 'mime':       return pick(mimeTypes)
    case 'md5':        return randHex(32)
    case 'sha1':       return randHex(40)
    case 'sha256':     return randHex(64)
    case 'base64':     return randBase64(`${pick(firstNames)} ${pick(lastNames)}`)
    default:           return ''
  }
}

/* ─────────────────────────────────────────────
   Generation & Download
───────────────────────────────────────────── */
function formatNumber(n) {
  return n != null ? Number(n).toLocaleString() : ''
}

function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

function colName(col) {
  return col.name || col.defaultName
}

function xmlEscape(str) {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;')
}

function csvEscape(value, sep) {
  const str = String(value)
  if (/["\n\r]/.test(str) || str.includes(sep)) {
    return '"' + str.replace(/"/g, '""') + '"'
  }
  return str
}

async function generate() {
  if (!selectedColumns.length || !rowCount.value || rowCount.value < 1) return

  const total = Math.max(1, Math.floor(rowCount.value))
  const mimeType = fileFormat.value === 'csv' ? 'text/csv' : fileFormat.value === 'json' ? 'application/json' : 'application/xml'
  const filename = `mockgen_${total}rows_${Date.now()}.${fileFormat.value}`
  const useStream = total > 1_000_000 && typeof window.showSaveFilePicker !== 'undefined'

  let writable = null
  if (useStream) {
    try {
      const fileHandle = await window.showSaveFilePicker({
        suggestedName: filename,
        types: [{ description: fileFormat.value.toUpperCase() + ' file', accept: { [mimeType]: ['.' + fileFormat.value] } }],
      })
      writable = await fileHandle.createWritable()
    } catch (err) {
      if (err.name === 'AbortError') return
      throw err
    }
  }

  generating.value = true
  generatedRows.value = 0
  lastFileInfo.value = null

  await new Promise(r => setTimeout(r, 20))

  const CHUNK = total <= 5000 ? total + 1 : Math.min(20000, Math.ceil(total / 10))
  const cols = [...selectedColumns]
  const sep = csvDelimiter.value
  const names = cols.map(c => colName(c))

  const yieldControl = () => new Promise(r => {
    if (typeof requestIdleCallback !== 'undefined') {
      requestIdleCallback(r, { timeout: 100 })
    } else {
      setTimeout(r, 0)
    }
  })

  let totalBytes = 0
  const parts = []
  const flush = async () => {
    if (useStream && parts.length) {
      const chunk = parts.join('')
      totalBytes += new TextEncoder().encode(chunk).byteLength
      await writable.write(chunk)
      parts.length = 0
    }
  }

  try {
    if (fileFormat.value === 'csv') {
      let header = ''
      for (let j = 0; j < names.length; j++) {
        if (j) header += sep
        header += csvEscape(names[j], sep)
      }
      parts.push(header + '\n')

      for (let i = 0; i < total; i++) {
        let row = ''
        for (let j = 0; j < cols.length; j++) {
          if (j) row += sep
          row += csvEscape(generateValue(cols[j], i), sep)
        }
        parts.push(row + '\n')
        if ((i + 1) % CHUNK === 0) {
          generatedRows.value = i + 1
          await flush()
          await yieldControl()
        }
      }
    } else if (fileFormat.value === 'json') {
      const keyPrefixes = names.map((n, j) => (j === 0 ? '{' : ',') + JSON.stringify(n) + ':')

      if (jsonFormat.value === 'lines') {
        for (let i = 0; i < total; i++) {
          let row = ''
          for (let j = 0; j < cols.length; j++) {
            row += keyPrefixes[j] + JSON.stringify(generateValue(cols[j], i))
          }
          parts.push(row + '}\n')
          if ((i + 1) % CHUNK === 0) {
            generatedRows.value = i + 1
            await flush()
            await yieldControl()
          }
        }
      } else {
        parts.push('[\n')
        for (let i = 0; i < total; i++) {
          let row = ''
          for (let j = 0; j < cols.length; j++) {
            row += keyPrefixes[j] + JSON.stringify(generateValue(cols[j], i))
          }
          parts.push('  ' + row + '}' + (i < total - 1 ? ',\n' : '\n'))
          if ((i + 1) % CHUNK === 0) {
            generatedRows.value = i + 1
            await flush()
            await yieldControl()
          }
        }
        parts.push(']')
      }
    } else {
      const rootEl  = (xmlRootElement.value || 'records').replace(/[^a-zA-Z0-9_\-.:]/g, '_')
      const rowEl   = (xmlRowElement.value  || 'record' ).replace(/[^a-zA-Z0-9_\-.:]/g, '_')
      const tagNames = names.map(n => n.replace(/[^a-zA-Z0-9_\-.:]/g, '_'))
      parts.push('<?xml version="1.0" encoding="UTF-8"?>\n', `<${rootEl}>\n`)

      for (let i = 0; i < total; i++) {
        if (xmlMode.value === 'attributes') {
          let attrs = ''
          for (let j = 0; j < cols.length; j++) {
            attrs += ` ${tagNames[j]}="${xmlEscape(String(generateValue(cols[j], i)))}"`
          }
          parts.push(`  <${rowEl}${attrs}/>\n`)
        } else {
          let row = `  <${rowEl}>\n`
          for (let j = 0; j < cols.length; j++) {
            row += `    <${tagNames[j]}>${xmlEscape(String(generateValue(cols[j], i)))}</${tagNames[j]}>\n`
          }
          row += `  </${rowEl}>\n`
          parts.push(row)
        }

        if ((i + 1) % CHUNK === 0) {
          generatedRows.value = i + 1
          await flush()
          await yieldControl()
        }
      }
      parts.push(`</${rootEl}>\n`)
    }

    await flush()
    generatedRows.value = total

    if (useStream) {
      await writable.close()
      writable = null
      lastFileInfo.value = { filename, size: formatBytes(totalBytes) }
    } else {
      const blob = new Blob(parts, { type: mimeType })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
      lastFileInfo.value = { filename, size: formatBytes(blob.size) }
    }
  } catch (err) {
    if (writable) {
      await writable.abort()
      writable = null
    }
    throw err
  } finally {
    generating.value = false
  }
}
</script>

<style>
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
body { background: #0f0f13; color: #e2e8f0; font-family: Inter, system-ui, sans-serif; min-height: 100vh; }
</style>

<style scoped>
.app {
  min-height: 100vh;
  background: linear-gradient(135deg, #0f0f13 0%, #13131e 50%, #0d0d18 100%);
  color: #e2e8f0;
}

/* Header */
.app-header {
  background: linear-gradient(90deg, rgba(99,102,241,0.15) 0%, rgba(139,92,246,0.1) 100%);
  border-bottom: 1px solid rgba(99,102,241,0.2);
  padding: 28px 0;
}
.header-inner {
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 24px;
  display: flex;
  align-items: center;
  gap: 20px;
}
.logo {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 1.6rem;
  font-weight: 800;
  letter-spacing: -0.5px;
  background: linear-gradient(135deg, #818cf8, #c084fc);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}
.subtitle {
  color: #94a3b8;
  font-size: 0.95rem;
  padding-left: 8px;
  border-left: 1px solid rgba(148,163,184,0.2);
}

/* Main */
.main-content {
  max-width: 1200px;
  margin: 0 auto;
  padding: 32px 24px 64px;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

/* Card */
.card {
  background: rgba(255,255,255,0.04);
  border: 1px solid rgba(255,255,255,0.08);
  border-radius: 16px;
  padding: 28px;
  transition: opacity 0.2s;
}
.card.disabled { opacity: 0.45; pointer-events: none; }
.card-header {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 24px;
}
.card-header h2 {
  font-size: 1.15rem;
  font-weight: 700;
  color: #f1f5f9;
}
.card-hint {
  font-size: 0.82rem;
  color: #64748b;
  margin-top: 3px;
}
.step-badge {
  min-width: 32px;
  height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, #6366f1, #8b5cf6);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 0.85rem;
  color: white;
  flex-shrink: 0;
  margin-top: 1px;
}

/* Type palette */
.type-palette {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 20px;
}
.type-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 13px 6px 10px;
  border-radius: 999px;
  border: 1px solid color-mix(in srgb, var(--pill-color) 40%, transparent);
  background: color-mix(in srgb, var(--pill-color) 12%, transparent);
  color: color-mix(in srgb, var(--pill-color) 90%, white);
  font-size: 0.8rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
  letter-spacing: 0.01em;
}
.type-pill:hover {
  background: color-mix(in srgb, var(--pill-color) 25%, transparent);
  border-color: color-mix(in srgb, var(--pill-color) 70%, transparent);
  transform: translateY(-1px);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--pill-color) 20%, transparent);
}
.pill-icon { font-size: 0.9rem; }

/* Selected columns */
.columns-area { margin-top: 4px; }
.columns-label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 0.82rem;
  color: #64748b;
  margin-bottom: 10px;
  font-weight: 500;
}
.btn-ghost-sm {
  background: none;
  border: 1px solid rgba(255,255,255,0.1);
  color: #94a3b8;
  font-size: 0.75rem;
  padding: 3px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s;
}
.btn-ghost-sm:hover { background: rgba(255,255,255,0.07); color: #e2e8f0; }

.columns-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.column-chip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 10px;
  border: 1px solid color-mix(in srgb, var(--chip-color) 30%, transparent);
  background: color-mix(in srgb, var(--chip-color) 8%, rgba(255,255,255,0.02));
  transition: all 0.15s;
  cursor: grab;
  flex-wrap: wrap;
}
.column-chip:active { cursor: grabbing; }
.column-chip:hover { border-color: color-mix(in srgb, var(--chip-color) 55%, transparent); }

.chip-drag { color: #475569; font-size: 1rem; cursor: grab; flex-shrink: 0; }
.chip-icon { font-size: 0.95rem; flex-shrink: 0; }
.chip-name {
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 6px;
  color: #e2e8f0;
  font-size: 0.82rem;
  padding: 3px 8px;
  width: 130px;
  outline: none;
  transition: border-color 0.15s;
}
.chip-name:focus { border-color: color-mix(in srgb, var(--chip-color) 70%, transparent); }
.chip-type-label {
  font-size: 0.72rem;
  color: color-mix(in srgb, var(--chip-color) 80%, white);
  background: color-mix(in srgb, var(--chip-color) 18%, transparent);
  padding: 2px 8px;
  border-radius: 999px;
  font-weight: 600;
  white-space: nowrap;
}
.chip-select {
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 6px;
  color: #e2e8f0;
  font-size: 0.78rem;
  padding: 3px 6px;
  outline: none;
  cursor: pointer;
  max-width: 240px;
}
.chip-select:focus { border-color: color-mix(in srgb, var(--chip-color) 60%, transparent); }
.chip-input {
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 6px;
  color: #e2e8f0;
  font-size: 0.78rem;
  padding: 3px 8px;
  width: 120px;
  outline: none;
}
.chip-input-sm { width: 70px; }
.chip-input-custom { width: 180px; font-family: monospace; }
.chip-sep { color: #475569; font-size: 0.85rem; }
.chip-remove {
  margin-left: auto;
  background: none;
  border: none;
  color: #475569;
  font-size: 1.1rem;
  cursor: pointer;
  padding: 0 2px;
  line-height: 1;
  border-radius: 4px;
  transition: color 0.15s;
  flex-shrink: 0;
}
.chip-remove:hover { color: #f87171; }

.empty-columns {
  text-align: center;
  color: #334155;
  font-size: 0.87rem;
  padding: 20px;
  border: 2px dashed rgba(255,255,255,0.06);
  border-radius: 10px;
}

/* Settings */
.settings-row {
  display: flex;
  flex-wrap: wrap;
  gap: 28px;
  align-items: flex-start;
  margin-bottom: 28px;
}
.setting-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.setting-group label {
  font-size: 0.8rem;
  font-weight: 600;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.row-input-wrap { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.row-input {
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.12);
  border-radius: 8px;
  color: #f1f5f9;
  font-size: 1rem;
  font-weight: 600;
  padding: 8px 14px;
  width: 130px;
  outline: none;
  transition: border-color 0.15s;
}
.row-input:focus { border-color: #6366f1; }
.row-presets { display: flex; gap: 5px; flex-wrap: wrap; }
.preset-btn {
  background: rgba(255,255,255,0.05);
  border: 1px solid rgba(255,255,255,0.1);
  color: #94a3b8;
  font-size: 0.75rem;
  padding: 5px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s;
  font-weight: 500;
}
.preset-btn:hover, .preset-btn.active {
  background: rgba(99,102,241,0.2);
  border-color: rgba(99,102,241,0.5);
  color: #a5b4fc;
}
.format-tabs { display: flex; gap: 5px; flex-wrap: wrap; }
.format-tab {
  display: flex;
  align-items: center;
  gap: 5px;
  background: rgba(255,255,255,0.05);
  border: 1px solid rgba(255,255,255,0.1);
  color: #94a3b8;
  font-size: 0.82rem;
  padding: 7px 14px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s;
  font-weight: 500;
}
.format-tab:hover { border-color: rgba(255,255,255,0.2); color: #e2e8f0; }
.format-tab.active {
  background: rgba(99,102,241,0.22);
  border-color: rgba(99,102,241,0.55);
  color: #a5b4fc;
  font-weight: 600;
}

/* Generate */
.generate-area {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
}
.btn-generate {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  background: linear-gradient(135deg, #6366f1, #8b5cf6);
  border: none;
  border-radius: 10px;
  color: white;
  font-size: 1rem;
  font-weight: 700;
  padding: 12px 28px;
  cursor: pointer;
  transition: all 0.2s;
  box-shadow: 0 4px 20px rgba(99,102,241,0.35);
  letter-spacing: 0.01em;
}
.btn-generate:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 8px 28px rgba(99,102,241,0.5);
}
.btn-generate:disabled { opacity: 0.4; cursor: not-allowed; transform: none; }
.spinner {
  width: 16px;
  height: 16px;
  border: 2px solid rgba(255,255,255,0.3);
  border-top-color: white;
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
  flex-shrink: 0;
}
@keyframes spin { to { transform: rotate(360deg); } }

.file-info {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.85rem;
  color: #10b981;
  background: rgba(16,185,129,0.1);
  border: 1px solid rgba(16,185,129,0.25);
  border-radius: 8px;
  padding: 8px 14px;
}
.file-info-icon { font-size: 1rem; }

/* Preview */
.preview-note {
  font-size: 0.78rem;
  color: #475569;
  font-weight: 400;
}
.table-wrap {
  overflow-x: auto;
  border-radius: 10px;
  border: 1px solid rgba(255,255,255,0.08);
}
.preview-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.82rem;
}
.preview-table th {
  background: rgba(99,102,241,0.12);
  color: #a5b4fc;
  font-weight: 600;
  padding: 10px 14px;
  text-align: left;
  white-space: nowrap;
  border-bottom: 1px solid rgba(255,255,255,0.07);
}
.preview-table td {
  padding: 8px 14px;
  color: #cbd5e1;
  border-bottom: 1px solid rgba(255,255,255,0.04);
  max-width: 250px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.preview-table tr:last-child td { border-bottom: none; }
.preview-table tr:hover td { background: rgba(255,255,255,0.02); }

/* Footer */
.app-footer {
  text-align: center;
  padding: 20px;
  font-size: 0.78rem;
  color: #334155;
  border-top: 1px solid rgba(255,255,255,0.05);
}
</style>
