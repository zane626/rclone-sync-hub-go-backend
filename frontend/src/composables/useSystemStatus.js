import { computed, onBeforeUnmount, onMounted, reactive, readonly } from 'vue';
import { getLiveness, getReadiness } from '../api/operations';

const state = reactive({
  liveness: 'checking',
  readiness: 'checking',
  uptimeSeconds: 0,
  lastCheckedAt: null,
  latencyMilliseconds: 0,
  error: ''
});

let subscribers = 0;
let refreshTimer;
let inFlight;

async function refreshSystemStatus() {
  if (inFlight) return inFlight;
  inFlight = (async () => {
    const startedAt = performance.now();
    const [liveResult, readyResult] = await Promise.allSettled([getLiveness(), getReadiness()]);

    if (liveResult.status === 'fulfilled') {
      state.liveness = liveResult.value?.status === 'ok' ? 'online' : 'offline';
      state.uptimeSeconds = Number(liveResult.value?.uptime_seconds || 0);
    } else {
      state.liveness = 'offline';
    }

    if (readyResult.status === 'fulfilled') {
      state.readiness = readyResult.value?.status === 'ok' ? 'ready' : 'degraded';
    } else {
      state.readiness = readyResult.reason?.response?.data?.status === 'not_ready' ? 'degraded' : 'offline';
    }

    state.latencyMilliseconds = Math.max(0, Math.round(performance.now() - startedAt));
    state.lastCheckedAt = new Date();
    state.error = state.liveness === 'online' && state.readiness === 'ready'
      ? ''
      : state.liveness !== 'online' ? '服务节点不可达' : '数据库尚未就绪';
  })().finally(() => {
    inFlight = null;
  });
  return inFlight;
}

function startPolling(interval) {
  subscribers += 1;
  refreshSystemStatus();
  if (!refreshTimer) {
    refreshTimer = window.setInterval(() => {
      if (document.visibilityState === 'visible') refreshSystemStatus();
    }, interval);
  }
}

function stopPolling() {
  subscribers = Math.max(0, subscribers - 1);
  if (!subscribers && refreshTimer) {
    window.clearInterval(refreshTimer);
    refreshTimer = undefined;
  }
}

export function useSystemStatus(options = {}) {
  const interval = Math.max(5000, Number(options.interval || 30000));
  const operational = computed(() => state.liveness === 'online' && state.readiness === 'ready');
  const checking = computed(() => state.liveness === 'checking' || state.readiness === 'checking');

  onMounted(() => startPolling(interval));
  onBeforeUnmount(stopPolling);

  return {
    status: readonly(state),
    operational,
    checking,
    refresh: refreshSystemStatus
  };
}
