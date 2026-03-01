import http from './http';

export function fetchWatchFolders(params) {
  return http.get('/api/watch-folders', {
    params
  }).then((res) => res.data);
}

export function createWatchFolder(body) {
  return http.post('/api/watch-folders', body).then((res) => res.data);
}

export function updateWatchFolder(id, body) {
  return http.put(`/api/watch-folders/${id}`, body).then((res) => res.data);
}

export function deleteWatchFolder(id) {
  return http.delete(`/api/watch-folders/${id}`).then((res) => res.data);
}

