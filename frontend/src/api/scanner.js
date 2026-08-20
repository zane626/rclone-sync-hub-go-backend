import http from './http';

export function triggerScan() {
  return http.post('/api/scan').then((res) => res.data);
}
