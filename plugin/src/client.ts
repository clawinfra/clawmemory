/**
 * ClawMemory HTTP client — wraps the ClawMemory REST API.
 */

import * as http from 'http';
import * as https from 'https';
import { URL } from 'url';

import {
  IngestResponse,
  RecallResponse,
  RememberResponse,
  ProfileResponse,
  StatsResponse,
  Turn,
} from './types';

/** Default timeout for API requests in milliseconds. */
const DEFAULT_TIMEOUT_MS = 10000;

/**
 * ClawMemoryClient provides a typed HTTP client for the ClawMemory API.
 */
export class ClawMemoryClient {
  private baseURL: string;
  private timeoutMs: number;

  constructor(baseURL: string, timeoutMs: number = DEFAULT_TIMEOUT_MS) {
    this.baseURL = baseURL.replace(/\/$/, ''); // strip trailing slash
    this.timeoutMs = timeoutMs;
  }

  /**
   * Ingest conversation turns — auto-extracts facts and stores them.
   */
  async ingest(sessionId: string, turns: Turn[]): Promise<IngestResponse> {
    return this.post('/api/v1/ingest', { session_id: sessionId, turns });
  }

  /**
   * Remember a fact manually.
   */
  async remember(
    content: string,
    category: string = 'general',
    container: string = 'general',
    importance: number = 0.7,
    expiresAt?: number
  ): Promise<RememberResponse> {
    const body: Record<string, unknown> = { content, category, container, importance };
    if (expiresAt !== undefined) {
      body.expires_at = expiresAt;
    }
    return this.post('/api/v1/remember', body);
  }

  /**
   * Recall facts matching a query using hybrid search.
   */
  async recall(
    query: string,
    limit: number = 10,
    container: string = '',
    threshold: number = 0.0,
    includeProfile: boolean = false
  ): Promise<RecallResponse> {
    return this.post('/api/v1/recall', {
      query,
      limit,
      container,
      threshold,
      include_profile: includeProfile,
    });
  }

  /**
   * Get the user profile.
   */
  async profile(): Promise<ProfileResponse> {
    return this.get('/api/v1/profile');
  }

  /**
   * Forget facts matching a query.
   */
  async forget(query: string, maxDelete: number = 5): Promise<{ deleted_count: number }> {
    return this.post('/api/v1/forget', { query, max_delete: maxDelete });
  }

  /**
   * Get memory statistics.
   */
  async stats(): Promise<StatsResponse> {
    return this.get('/api/v1/stats');
  }

  /**
   * Trigger immediate cloud sync.
   */
  async sync(): Promise<{ synced: boolean; sync_latency_ms: number }> {
    return this.post('/api/v1/sync', {});
  }

  /**
   * Check if the server is healthy.
   */
  async health(): Promise<{ status: string; version: string }> {
    return this.get('/health');
  }

  private async get<T>(path: string): Promise<T> {
    return this.request<T>('GET', path, undefined);
  }

  private async post<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>('POST', path, body);
  }

  private request<T>(method: string, path: string, body: unknown): Promise<T> {
    return new Promise((resolve, reject) => {
      const url = new URL(this.baseURL + path);
      const isHttps = url.protocol === 'https:';
      const lib = isHttps ? https : http;

      const bodyStr = body !== undefined ? JSON.stringify(body) : undefined;
      const options: http.RequestOptions = {
        hostname: url.hostname,
        port: url.port || (isHttps ? 443 : 80),
        path: url.pathname + url.search,
        method,
        headers: {
          'Content-Type': 'application/json',
          'Accept': 'application/json',
          ...(bodyStr ? { 'Content-Length': Buffer.byteLength(bodyStr) } : {}),
        },
        timeout: this.timeoutMs,
      };

      const req = lib.request(options, (res) => {
        let data = '';
        res.on('data', (chunk) => { data += chunk; });
        res.on('end', () => {
          try {
            const parsed = JSON.parse(data);
            if (res.statusCode && res.statusCode >= 400) {
              reject(new Error(`ClawMemory API error ${res.statusCode}: ${parsed.error || data}`));
            } else {
              resolve(parsed as T);
            }
          } catch (e) {
            reject(new Error(`Failed to parse response: ${data}`));
          }
        });
      });

      req.on('error', reject);
      req.on('timeout', () => {
        req.destroy();
        reject(new Error(`Request timeout after ${this.timeoutMs}ms`));
      });

      if (bodyStr) {
        req.write(bodyStr);
      }
      req.end();
    });
  }
}
