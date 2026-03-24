"use strict";
/**
 * Post-turn capture hook — automatically extracts facts from each conversation turn.
 * Called after each assistant response is generated.
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.CaptureHook = void 0;
/**
 * CaptureHook processes conversation turns and ingests them into ClawMemory.
 */
class CaptureHook {
    constructor(client, config) {
        this.client = client;
        this.config = config;
    }
    /**
     * Called after each assistant turn completes.
     * Sends the last N turns to the memory engine for fact extraction.
     *
     * @param sessionId - Unique session identifier
     * @param turns - Recent conversation turns to process
     * @returns Number of facts extracted, or 0 on error
     */
    async onPostTurn(sessionId, turns) {
        if (!this.config.auto_capture) {
            return 0;
        }
        if (turns.length === 0) {
            return 0;
        }
        try {
            const result = await this.client.ingest(sessionId, turns);
            const factsExtracted = result.extracted_facts?.length ?? 0;
            if (factsExtracted > 0) {
                console.log(`[clawmemory] Captured ${factsExtracted} fact(s) from session ${sessionId}`);
            }
            return factsExtracted;
        }
        catch (err) {
            // Gracefully degrade — memory capture failure should not break the agent
            console.warn(`[clawmemory] Capture failed (non-fatal): ${err}`);
            return 0;
        }
    }
}
exports.CaptureHook = CaptureHook;
//# sourceMappingURL=capture.js.map