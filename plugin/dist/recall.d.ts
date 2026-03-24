/**
 * Pre-turn recall hook — injects relevant memories into context before each turn.
 * Called before the LLM processes the user's message.
 */
import { ClawMemoryClient } from './client';
import { PluginConfig, RecallResult } from './types';
/**
 * RecallContext is the result of a pre-turn recall, ready to inject into context.
 */
export interface RecallContext {
    facts: RecallResult[];
    profileSummary: string;
    contextBlock: string;
}
/**
 * RecallHook retrieves relevant memories before each turn.
 */
export declare class RecallHook {
    private client;
    private config;
    constructor(client: ClawMemoryClient, config: PluginConfig);
    /**
     * Called before each user turn is processed.
     * Retrieves the most relevant facts for the incoming message.
     *
     * @param query - The user's incoming message or topic
     * @returns RecallContext with relevant facts and a formatted context block
     */
    onPreTurn(query: string): Promise<RecallContext>;
    /**
     * Formats recalled facts and profile into a context block for system prompt injection.
     */
    private formatContextBlock;
}
//# sourceMappingURL=recall.d.ts.map