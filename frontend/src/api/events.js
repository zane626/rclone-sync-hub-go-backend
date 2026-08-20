export async function streamTaskEvents(onEvent, signal, onConnectionChange = () => {}) {
  const token = sessionStorage.getItem('rsh_access_token');
  try {
    const response = await fetch('/api/events', {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      signal
    });
    if (!response.ok || !response.body) {
      throw new Error(`event stream failed: ${response.status}`);
    }
    onConnectionChange(true);
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    while (!signal.aborted) {
      const { value, done } = await reader.read();
      if (done) return;
      buffer += decoder.decode(value, { stream: true });
      let boundary;
      while ((boundary = buffer.indexOf('\n\n')) >= 0) {
        const frame = buffer.slice(0, boundary);
        buffer = buffer.slice(boundary + 2);
        const data = frame.split('\n').filter((line) => line.startsWith('data:')).map((line) => line.slice(5).trim()).join('\n');
        if (!data) continue;
        try {
          onEvent(JSON.parse(data));
        } catch {
          // Ignore a malformed snapshot and keep the stream alive.
        }
      }
    }
  } finally {
    onConnectionChange(false);
  }
}
