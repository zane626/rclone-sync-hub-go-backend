import http from './http';

export function getLiveness() {
  return http.get('/api/health/live').then((res) => res.data);
}

export function getReadiness() {
  return http.get('/api/health/ready').then((res) => res.data);
}

export function getCurrentSession() {
  return http.get('/api/auth/me').then((res) => res.data);
}

export function fetchScanRuns(params) {
  return http.get('/api/scan-runs', { params }).then((res) => res.data);
}

export function fetchAuditLogs(params) {
  return http.get('/api/audit-logs', { params }).then((res) => res.data);
}
