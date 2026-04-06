/**
 * ClawMemory HTTP client — wraps the ClawMemory REST API.
 */
import { IngestResponse, RecallResponse, RememberResponse, ProfileResponse, StatsResponse, Turn } from './types';
/**
 * ClawMemoryClient provides a typed HTTP client for the ClawMemory API.
 */
export declare class ClawMemoryClient {
    private baseURL;
    private timeoutMs;
    constructor(baseURL: string, timeoutMs?: number);
    /**
     * Ingest conversation turns — auto-extracts facts and stores them.
     */
    ingest(sessionId: string, turns: Turn[]): Promise<IngestResponse>;
    /**
     * Remember a fact manually.
     */
    remember(content: string, category?: string, container?: string, importance?: number, expiresAt?: number): Promise<RememberResponse>;
    /**
     * Recall facts matching a query using hybrid search.
     */
    recall(query: string, limit?: number, container?: string, threshold?: number, includeProfile?: boolean): Promise<RecallResponse>;
    /**
     * Get the user profile.
     */
    profile(): Promise<ProfileResponse>;
    /**
     * Forget facts matching a query.
     */
    forget(query: string, maxDelete?: number): Promise<{
        deleted_count: number;
    }>;
    /**
     * Get memory statistics.
     */
    stats(): Promise<StatsResponse>;
    /**
     * Trigger immediate cloud sync.
     */
    sync(): Promise<{
        synced: boolean;
        sync_latency_ms: number;
    }>;
    /**
     * Check if the server is healthy.
     */
    health(): Promise<{
        status: string;
        version: string;
    }>;
    private get;
    private post;
    private request;
}
