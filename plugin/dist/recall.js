"use strict";
/**
 * Pre-turn recall hook — injects relevant memories into context before each turn.
 * Called before the LLM processes the user's message.
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.RecallHook = void 0;
/**
 * RecallHook retrieves relevant memories before each turn.
 */
class RecallHook {
    constructor(client, config) {
        this.client = client;
        this.config = config;
    }
    /**
     * Called before each user turn is processed.
     * Retrieves the most relevant facts for the incoming message.
     *
     * @param query - The user's incoming message or topic
     * @returns RecallContext with relevant facts and a formatted context block
     */
    async onPreTurn(query) {
        if (!this.config.auto_recall || !query.trim()) {
            return { facts: [], profileSummary: '', contextBlock: '' };
        }
        try {
            const result = await this.client.recall(query, this.config.recall_limit, '', // no container filter
            this.config.recall_threshold, true // include profile
            );
            const contextBlock = this.formatContextBlock(result.results, result.profile_summary);
            return {
                facts: result.results,
                profileSummary: result.profile_summary,
                contextBlock,
            };
        }
        catch (err) {
            // Gracefully degrade — recall failure should not block the turn
            console.warn(`[clawmemory] Recall failed (non-fatal): ${err}`);
            return { facts: [], profileSummary: '', contextBlock: '' };
        }
    }
    /**
     * Formats recalled facts and profile into a context block for system prompt injection.
     */
    formatContextBlock(facts, profileSummary) {
        const parts = [];
        if (profileSummary) {
            parts.push(`## User Profile\n${profileSummary}`);
        }
        if (facts.length > 0) {
            const factLines = facts
                .slice(0, this.config.recall_limit)
                .map((f) => `- ${f.content}`)
                .join('\n');
            parts.push(`## Relevant Memory\n${factLines}`);
        }
        if (parts.length === 0) {
            return '';
        }
        return `<!-- ClawMemory Context -->\n${parts.join('\n\n')}\n<!-- End ClawMemory Context -->`;
    }
}
exports.RecallHook = RecallHook;
//# sourceMappingURL=recall.js.map