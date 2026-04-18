import { JQ_CDN } from '@/constants'

let jqPromise = null

/**
 * Lazily loads jq-web from CDN and returns the jq API object.
 * Subsequent calls return the same promise (singleton).
 */
export function loadJq() {
  if (jqPromise) return jqPromise
  jqPromise = new Promise((resolve, reject) => {
    const script = document.createElement('script')
    script.src = JQ_CDN
    script.onload = () => {
      if (window.jq && window.jq.promised) resolve(window.jq.promised)
      else if (window.jq) resolve(window.jq)
      else { jqPromise = null; reject(new Error('jq-web did not expose jq global')) }
    }
    script.onerror = () => { jqPromise = null; reject(new Error('Failed to load jq-web from CDN')) }
    document.head.appendChild(script)
  })
  return jqPromise
}
