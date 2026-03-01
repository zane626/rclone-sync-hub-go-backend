import http from './http';

export function fetchTasks(params) {
  return http.get('/api/tasks', {
    params
  }).then((res) => res.data);
}

export function deleteTask(id) {
  return http.delete(`/api/tasks/${id}`).then((res) => res.data);
}

export function pauseTask(id) {
  return http.post(`/api/tasks/${id}/pause`).then((res) => res.data);
}

export function batchDeleteTasks(body) {
  return http.post('/api/tasks/batch/delete', body).then((res) => res.data);
}

export function batchPauseTasks(body) {
  return http.post('/api/tasks/batch/pause', body).then((res) => res.data);
}

export function batchRetryTasks(body) {
  return http.post('/api/tasks/batch/retry', body).then((res) => res.data);
}

