import { reactive } from 'vue';

export const uiState = reactive({
  toasts: [],
  confirmation: null
});

let toastID = 0;
let confirmResolve = null;

export function toast(message, type = 'info', duration = 3600) {
  const id = ++toastID;
  uiState.toasts.push({ id, message: String(message || ''), type });
  window.setTimeout(() => dismissToast(id), duration);
  return id;
}

export function dismissToast(id) {
  const index = uiState.toasts.findIndex((item) => item.id === id);
  if (index >= 0) uiState.toasts.splice(index, 1);
}

export function confirmDialog(options = {}) {
  if (confirmResolve) confirmResolve(false);
  uiState.confirmation = {
    title: options.title || '确认操作',
    message: options.message || '',
    confirmText: options.confirmText || '确认',
    cancelText: options.cancelText || '取消',
    tone: options.tone || 'danger'
  };
  return new Promise((resolve) => {
    confirmResolve = resolve;
  });
}

export function settleConfirmation(result) {
  const resolve = confirmResolve;
  confirmResolve = null;
  uiState.confirmation = null;
  resolve?.(Boolean(result));
}

export function errorText(error, fallback) {
  return error?.response?.data?.error || error?.response?.data?.message || error?.message || fallback;
}
