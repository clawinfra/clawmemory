/**
 * Post-turn capture hook — automatically extracts facts from each conversation turn.
 * Called after each assistant response is generated.
 */

import { ClawMemoryClient } from './client';
import { PluginConfig, Turn } from './types';

/**
 * CaptureHook processes conversation turns and ingests them into ClawMemory.
 */
export class CaptureHook {
  private client: ClawMemoryClient;
  private config: PluginConfig;

  constructor(client: ClawMemoryClient, config: PluginConfig) {
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
  async onPostTurn(sessionId: string, turns: Turn[]): Promise<number> {
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
    } catch (err) {
      // Gracefully degrade — memory capture failure should not break the agent
      console.warn(`[clawmemory] Capture failed (non-fatal): ${err}`);
      return 0;
    }
  }
}
