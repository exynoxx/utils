import { ref, computed } from 'vue'
import { DISPLAY_SIZE_LIMIT, DISPLAY_CHARS } from '@/constants'

/**
 * Handles JSON file reading, textarea parsing, and related state.
 *
 * Exposes:
 *   rawInput, parsedData, parseError, isDragging,
 *   loadProgress, fileMeta, fileFullText,
 *   parseJSON, triggerFileInput, onFileChange, onDrop, onTextareaInput
 */
export function useJsonFile() {
  const rawInput     = ref('')
  const parsedData   = ref(null)
  const parseError   = ref('')
  const isDragging   = ref(false)
  const fileInput    = ref(null)
  const loadProgress = ref(null)
  const fileFullText = ref(null)
  const fileMeta     = ref(null)

  // Callbacks to notify parent when data is parsed
  const _onParsed = []
  function onParsed(cb) { _onParsed.push(cb) }
  function _notifyParsed() { _onParsed.forEach(cb => cb(parsedData.value)) }

  function parseJSON() {
    parseError.value = ''
    const src = fileFullText.value ?? rawInput.value
    try {
      parsedData.value = JSON.parse(src.trim())
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

  function _readFile(file) {
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
        parseJSON()
        loadProgress.value = null
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
    parseJSON, triggerFileInput, onFileChange, onDrop, onTextareaInput, onParsed,
  }
}
