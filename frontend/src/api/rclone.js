import http from './http';

export function fetchRcloneConfigs() {
  return http.get('/api/rclone/configs').then((res) => res.data);
}

