import { ref } from 'vue'
import { DISPLAY_SIZE_LIMIT, DISPLAY_CHARS } from '@/constants'
import { parseXMLText } from '@/utils/xmlUtils'

/**
 * Handles XML file reading, textarea parsing, and related UI state.
 */
export function useXmlFile() {
  const rawInput     = ref('')
  const parsedData   = ref(null)   // plain tree: {tag, attrs, children, text}
  const parseError   = ref('')
  const isDragging   = ref(false)
  const fileInput    = ref(null)
  const loadProgress = ref(null)
  const fileFullText = ref(null)
  const fileMeta     = ref(null)

  // Callbacks to notify when data is successfully parsed
  const _onParsed = []
  function onParsed(cb) { _onParsed.push(cb) }
  function _notifyParsed() { _onParsed.forEach(cb => cb(parsedData.value)) }

  function parseXML() {
    parseError.value = ''
    const src = fileFullText.value ?? rawInput.value
    try {
      parsedData.value = parseXMLText(src.trim())
      _notifyParsed()
    } catch (e) {
      parseError.value = e.message
      parsedData.value = null
    }
  }

  function triggerFileInput() {
    fileInput.value && fileInput.value.click()
  }

  function onFileChange(e) {
    const file = e.target.files[0]
    if (file) _readFile(file)
    e.target.value = ''
  }

  function onDrop(e) {
    isDragging.value = false
    const file = e.dataTransfer.files[0]
    if (file) _readFile(file)
  }

  function onTextareaInput() {
    fileFullText.value = null
    fileMeta.value = null
  }

  // Guard against race: stale reads from previously loaded files are discarded.
  let _readToken = 0

  function _readFile(file) {
    const token = ++_readToken
    const isLarge = file.size > DISPLAY_SIZE_LIMIT
    if (isLarge) loadProgress.value = 0

    const reader = new FileReader()
    if (isLarge) {
      reader.onprogress = e => {
        if (e.lengthComputable)
          loadProgress.value = Math.min(99, Math.round((e.loaded / e.total) * 100))
      }
    }
    reader.onload = ev => {
      if (token !== _readToken) return
      loadProgress.value = isLarge ? 100 : null
      const text = ev.target.result
      setTimeout(() => {
        if (isLarge) {
          fileFullText.value = text
          rawInput.value = text.slice(0, DISPLAY_CHARS)
          fileMeta.value = { name: file.name, sizeMB: (file.size / 1048576).toFixed(2), truncated: true }
        } else {
          fileFullText.value = null
          fileMeta.value = { name: file.name, sizeMB: (file.size / 1048576).toFixed(2), truncated: false }
          rawInput.value = text
        }
        if (isLarge) {
          loadProgress.value = 'Parsing XML…'
          setTimeout(() => {
            parseXML()
            loadProgress.value = null
          }, 50)
        } else {
          parseXML()
          loadProgress.value = null
        }
      }, 30)
    }
    reader.onerror = () => {
      loadProgress.value = null
      parseError.value = 'Failed to read file'
    }
    reader.readAsText(file)
  }

  return {
    rawInput, parsedData, parseError, isDragging,
    fileInput, loadProgress, fileMeta,
    displayChars: DISPLAY_CHARS,
    parseXML, triggerFileInput, onFileChange, onDrop, onTextareaInput, onParsed,
  }
}
