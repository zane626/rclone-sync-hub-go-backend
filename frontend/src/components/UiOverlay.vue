<template>
  <Teleport to="body">
    <div class="toast-viewport" aria-live="polite" aria-atomic="true">
      <TransitionGroup name="toast">
        <button
          v-for="item in uiState.toasts"
          :key="item.id"
          class="toast-item"
          :class="`is-${item.type}`"
          type="button"
          @click="dismissToast(item.id)"
        >
          <span class="toast-icon">
            <UiIcon :name="toastIcon(item.type)" :size="17" />
          </span>
          <span>{{ item.message }}</span>
          <UiIcon name="close" :size="14" class="toast-close" />
        </button>
      </TransitionGroup>
    </div>

    <Transition name="dialog-fade">
      <div v-if="uiState.confirmation" class="dialog-layer" role="presentation" @click.self="settleConfirmation(false)">
        <section class="confirm-dialog" role="alertdialog" aria-modal="true" :aria-labelledby="titleID">
          <div class="confirm-dialog__signal" :class="`is-${uiState.confirmation.tone}`">
            <UiIcon :name="uiState.confirmation.tone === 'danger' ? 'alert' : 'info'" :size="22" />
          </div>
          <div class="confirm-dialog__body">
            <span class="eyebrow">ACTION REQUIRED</span>
            <h2 :id="titleID">{{ uiState.confirmation.title }}</h2>
            <p>{{ uiState.confirmation.message }}</p>
          </div>
          <div class="confirm-dialog__actions">
            <button class="ui-button is-ghost" type="button" @click="settleConfirmation(false)">
              {{ uiState.confirmation.cancelText }}
            </button>
            <button
              class="ui-button"
              :class="uiState.confirmation.tone === 'danger' ? 'is-danger' : 'is-primary'"
              type="button"
              @click="settleConfirmation(true)"
            >
              {{ uiState.confirmation.confirmText }}
            </button>
          </div>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { onBeforeUnmount, onMounted } from 'vue';
import UiIcon from './UiIcon.vue';
import { dismissToast, settleConfirmation, uiState } from '../composables/useUi';

const titleID = 'global-confirm-title';

function toastIcon(type) {
  return { success: 'check', error: 'alert', warning: 'alert', info: 'info' }[type] || 'info';
}

function handleKeydown(event) {
  if (event.key === 'Escape' && uiState.confirmation) settleConfirmation(false);
}

onMounted(() => window.addEventListener('keydown', handleKeydown));
onBeforeUnmount(() => window.removeEventListener('keydown', handleKeydown));
</script>
