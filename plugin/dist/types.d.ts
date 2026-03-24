/**
 * ClawMemory plugin types.
 */
/** A conversation turn sent to the memory engine. */
export interface Turn {
    role: 'user' | 'assistant';
    content: string;
}
/** A fact stored in ClawMemory. */
export interface Fact {
    id: string;
    content: string;
    category: 'person' | 'project' | 'preference' | 'event' | 'technical' | 'general';
    container: 'work' | 'trading' | 'clawchain' | 'personal' | 'general';
    importance: number;
    confidence: number;
    created_at: number;
    updated_at: number;
    expires_at?: number;
    superseded_by?: string;
    deleted: boolean;
}
/** A search result from /api/v1/recall. */
export interface RecallResult {
    fact_id: string;
    content: string;
    category: string;
    container: string;
    importance: number;
    score: number;
    bm25_score: number;
    vec_score: number;
}
/** Response from POST /api/v1/ingest. */
export interface IngestResponse {
    extracted_facts: Fact[];
    contradictions: ContradictionInfo[];
    turns_stored: number;
}
/** Response from POST /api/v1/recall. */
export interface RecallResponse {
    results: RecallResult[];
    profile_summary: string;
    total_results: number;
    search_latency_ms: number;
}
/** Response from POST /api/v1/remember. */
export interface RememberResponse {
    id: string;
    content: string;
    category: string;
    container: string;
    importance: number;
    contradictions: ContradictionInfo[];
}
/** Contradiction detected during fact insertion. */
export interface ContradictionInfo {
    existing_fact_id: string;
    existing_content: string;
    new_fact_id?: string;
    resolution: string;
}
/** Response from GET /api/v1/profile. */
export interface ProfileResponse {
    entries: Record<string, string>;
    summary: string;
    updated_at: number;
}
/** Response from GET /api/v1/stats. */
export interface StatsResponse {
    total_facts: number;
    active_facts: number;
    superseded_facts: number;
    deleted_facts: number;
    total_turns: number;
    unprocessed_turns: number;
    profile_entries: number;
    db_size_bytes: number;
    embedding_dimension: number;
    last_sync_at: number;
}
/** Plugin configuration. */
export interface PluginConfig {
    server_url: string;
    auto_capture: boolean;
    auto_recall: boolean;
    recall_limit: number;
    recall_threshold: number;
}
//# sourceMappingURL=types.d.ts.map