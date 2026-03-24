/**
 * Post-turn capture hook — automatically extracts facts from each conversation turn.
 * Called after each assistant response is generated.
 */
import { ClawMemoryClient } from './client';
import { PluginConfig, Turn } from './types';
/**
 * CaptureHook processes conversation turns and ingests them into ClawMemory.
 */
export declare class CaptureHook {
    private client;
    private config;
    constructor(client: ClawMemoryClient, config: PluginConfig);
    /**
     * Called after each assistant turn completes.
     * Sends the last N turns to the memory engine for fact extraction.
     *
     * @param sessionId - Unique session identifier
     * @param turns - Recent conversation turns to process
     * @returns Number of facts extracted, or 0 on error
     */
    onPostTurn(sessionId: string, turns: Turn[]): Promise<number>;
}
//# sourceMappingURL=capture.d.ts.map