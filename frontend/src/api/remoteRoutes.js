import http from './http';

// WebDAV mutations can take longer than the default 15-second API timeout,
// especially when OpenList has to wait for an upstream provider such as 115.
const remoteMutationTimeout = 10 * 60 * 1000;

export function fetchRemoteRoutes(params) {
  return http.get('/api/remote-routes', { params }).then((res) => res.data);
}

export function createRemoteRoute(body) {
  return http.post('/api/remote-routes', body).then((res) => res.data);
}

export function updateRemoteRoute(id, body) {
  return http.put(`/api/remote-routes/${id}`, body).then((res) => res.data);
}

export function deleteRemoteRoute(id) {
  return http.delete(`/api/remote-routes/${id}`).then((res) => res.data);
}

export function scanRemoteRoute(id) {
  return http.post(`/api/remote-routes/${id}/scan`).then((res) => res.data);
}

export function scanAllRemoteRoutes() {
  return http.post('/api/remote-routes/scan').then((res) => res.data);
}

export function fetchRemoteRouteFiles(id, params) {
  return http.get(`/api/remote-routes/${id}/files`, { params }).then((res) => res.data);
}

export function createRemoteFolder(id, body) {
  return http.post(`/api/remote-routes/${id}/folders`, body, { timeout: remoteMutationTimeout }).then((res) => res.data);
}

export function renameRemoteFolder(id, body) {
  return http.put(`/api/remote-routes/${id}/folders`, body, { timeout: remoteMutationTimeout }).then((res) => res.data);
}

export function startRemoteFileMove(id, body) {
  return http.post(`/api/remote-routes/${id}/files/move`, body).then((res) => res.data);
}

export function fetchRemoteFileMove(id, operationId) {
  return http.get(`/api/remote-routes/${id}/files/move/${encodeURIComponent(operationId)}`).then((res) => res.data);
}

function wait(milliseconds) {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds));
}

export async function moveRemoteFiles(id, body, onProgress = () => {}) {
  let operation = await startRemoteFileMove(id, body);
  onProgress(operation);
  let pollFailures = 0;
  while (operation.status === 'queued' || operation.status === 'running') {
    await wait(pollFailures ? Math.min(5000, 800 + pollFailures * 400) : 800);
    try {
      operation = await fetchRemoteFileMove(id, operation.id);
      pollFailures = 0;
      onProgress(operation);
    } catch (error) {
      const status = Number(error?.response?.status || 0);
      if ([401, 403, 404].includes(status)) throw error;
      pollFailures += 1;
      onProgress({ ...operation, poll_warning: `进度连接暂时中断，正在第 ${pollFailures} 次重试` });
    }
  }
  if (operation.status === 'failed') {
    const error = new Error(operation.error || '远端文件移动任务失败');
    error.operation = operation;
    throw error;
  }
  return {
    operation_id: operation.id,
    moved: operation.moved || [],
    failed: operation.failed || [],
    refresh_scheduled: Boolean(operation.refresh_scheduled)
  };
}
