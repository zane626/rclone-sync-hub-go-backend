import axios from 'axios';

const http = axios.create({
  baseURL: '',
  timeout: 15000
});

http.interceptors.response.use(
  (response) => response,
  (error) => {
    return Promise.reject(error);
  }
);

export default http;

