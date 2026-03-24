"use strict";
/**
 * ClawMemory HTTP client — wraps the ClawMemory REST API.
 */
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.ClawMemoryClient = void 0;
const http = __importStar(require("http"));
const https = __importStar(require("https"));
const url_1 = require("url");
/** Default timeout for API requests in milliseconds. */
const DEFAULT_TIMEOUT_MS = 10000;
/**
 * ClawMemoryClient provides a typed HTTP client for the ClawMemory API.
 */
class ClawMemoryClient {
    constructor(baseURL, timeoutMs = DEFAULT_TIMEOUT_MS) {
        this.baseURL = baseURL.replace(/\/$/, ''); // strip trailing slash
        this.timeoutMs = timeoutMs;
    }
    /**
     * Ingest conversation turns — auto-extracts facts and stores them.
     */
    async ingest(sessionId, turns) {
        return this.post('/api/v1/ingest', { session_id: sessionId, turns });
    }
    /**
     * Remember a fact manually.
     */
    async remember(content, category = 'general', container = 'general', importance = 0.7, expiresAt) {
        const body = { content, category, container, importance };
        if (expiresAt !== undefined) {
            body.expires_at = expiresAt;
        }
        return this.post('/api/v1/remember', body);
    }
    /**
     * Recall facts matching a query using hybrid search.
     */
    async recall(query, limit = 10, container = '', threshold = 0.0, includeProfile = false) {
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
    async profile() {
        return this.get('/api/v1/profile');
    }
    /**
     * Forget facts matching a query.
     */
    async forget(query, maxDelete = 5) {
        return this.post('/api/v1/forget', { query, max_delete: maxDelete });
    }
    /**
     * Get memory statistics.
     */
    async stats() {
        return this.get('/api/v1/stats');
    }
    /**
     * Trigger immediate cloud sync.
     */
    async sync() {
        return this.post('/api/v1/sync', {});
    }
    /**
     * Check if the server is healthy.
     */
    async health() {
        return this.get('/health');
    }
    async get(path) {
        return this.request('GET', path, undefined);
    }
    async post(path, body) {
        return this.request('POST', path, body);
    }
    request(method, path, body) {
        return new Promise((resolve, reject) => {
            const url = new url_1.URL(this.baseURL + path);
            const isHttps = url.protocol === 'https:';
            const lib = isHttps ? https : http;
            const bodyStr = body !== undefined ? JSON.stringify(body) : undefined;
            const options = {
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
                        }
                        else {
                            resolve(parsed);
                        }
                    }
                    catch (e) {
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
exports.ClawMemoryClient = ClawMemoryClient;
//# sourceMappingURL=client.js.map