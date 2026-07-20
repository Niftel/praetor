import { afterEach, describe, expect, it, vi } from 'vitest';
import { streamJobDiagnostics } from './api';

const encoder = new TextEncoder();

function streamResponse(chunks: string[]) {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      chunks.forEach(chunk => controller.enqueue(encoder.encode(chunk)));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

describe('resumable diagnostic stream', () => {
  afterEach(() => vi.restoreAllMocks());

  it('resumes from the exclusive cursor and suppresses replayed events', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(streamResponse([
      'id: 7\nevent: diagnostic\ndata: {"seq":7,"event_type":"JOB_STARTED","created_at":"2026-07-20T10:00:00Z"}\n\n',
      'id: 8\r',
      '\nevent: diagnostic\r\ndata: {"seq":8,"event_type":"JOB_COMPLETED","created_at":"2026-07-20T10:00:01Z"}\r\n\r\n',
      'event: terminal\ndata: {"state":"successful","cursor":8}\n\n',
    ]));
    const events: number[] = [];
    const terminal = vi.fn();

    await streamJobDiagnostics('run-1', 7, {
      onEvent: event => events.push(event.seq),
      onTerminal: terminal,
    }, new AbortController().signal);

    expect(events).toEqual([8]);
    expect(terminal).toHaveBeenCalledWith('successful', 8);
    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/jobs/runs/run-1/diagnostics/stream?cursor=7');
    expect(new Headers(options?.headers).get('Last-Event-ID')).toBe('7');
  });

  it('can reconnect with the last event accepted by the browser', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(streamResponse([
        'id: 3\nevent: diagnostic\ndata: {"seq":3,"event_type":"JOB_STARTED","created_at":"2026-07-20T10:00:00Z"}\n\n',
      ]))
      .mockResolvedValueOnce(streamResponse([
        'id: 3\nevent: diagnostic\ndata: {"seq":3,"event_type":"JOB_STARTED","created_at":"2026-07-20T10:00:00Z"}\n\n',
        'id: 4\nevent: diagnostic\ndata: {"seq":4,"event_type":"JOB_COMPLETED","created_at":"2026-07-20T10:00:01Z"}\n\n',
        'event: terminal\ndata: {"state":"successful","cursor":4}\n\n',
      ]));
    let cursor = 2;
    const accepted: number[] = [];
    const callbacks = {
      onEvent: (event: { seq: number }) => { cursor = event.seq; accepted.push(event.seq); },
      onTerminal: vi.fn(),
    };

    await streamJobDiagnostics('run-2', cursor, callbacks, new AbortController().signal);
    await streamJobDiagnostics('run-2', cursor, callbacks, new AbortController().signal);

    expect(accepted).toEqual([3, 4]);
    expect(globalThis.fetch).toHaveBeenNthCalledWith(2,
      '/api/v1/jobs/runs/run-2/diagnostics/stream?cursor=3', expect.anything());
  });
});
