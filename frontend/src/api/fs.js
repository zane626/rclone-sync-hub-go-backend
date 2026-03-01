import http from './http';

export function fetchSubdirs(path) {
  return http
    .get('/api/fs/subdirs', {
      params: { path }
    })
    .then((res) => res.data);
}

