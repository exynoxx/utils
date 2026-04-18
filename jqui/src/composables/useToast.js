import { reactive, onScopeDispose } from 'vue'

/**
 * Provides a toast notification system.
 * Usage: const { toast, showToast } = useToast()
 */
export function useToast() {
  const toast = reactive({ visible: false, msg: '', type: 'success' })
  let toastTimer = null

  onScopeDispose(() => clearTimeout(toastTimer))

  function showToast(msg, type = 'success', duration = 3000) {
    clearTimeout(toastTimer)
    toast.msg = msg
    toast.type = type
    toast.visible = true
    toastTimer = setTimeout(() => { toast.visible = false }, duration)
  }

  return { toast, showToast }
}
