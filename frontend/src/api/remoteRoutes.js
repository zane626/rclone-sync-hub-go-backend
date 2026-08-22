import http from './http';

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

