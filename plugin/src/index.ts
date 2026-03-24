/**
 * ClawMemory OpenClaw Plugin
 *
 * Provides sovereign agent memory with:
 * - Post-turn auto-capture: extracts facts from each conversation
 * - Pre-turn auto-recall: retrieves relevant memories before processing
 * - Slash commands: /remember, /recall, /profile, /forget, /memory-stats
 *
 * Configuration via manifest.json config section or environment variables:
 * - CLAWMEMORY_URL: server URL (default: http://127.0.0.1:7437)
 * - CLAWMEMORY_AUTO_CAPTURE: enable auto-capture (default: true)
 * - CLAWMEMORY_AUTO_RECALL: enable auto-recall (default: true)
 * - CLAWMEMORY_RECALL_LIMIT: max recall results (default: 5)
 */

import { ClawMemoryClient } from './client';
import { CaptureHook } from './capture';
import { RecallHook } from './recall';
import { ClawMemoryTools } from './tools';
import { PluginConfig, Turn } from './types';

export { ClawMemoryClient } from './client';
export { CaptureHook } from './capture';
export { RecallHook } from './recall';
export { ClawMemoryTools } from './tools';
export * from './types';

/**
 * ClawMemoryPlugin is the main entry point for the OpenClaw plugin.
 */
export class ClawMemoryPlugin {
  readonly client: ClawMemoryClient;
  readonly captureHook: CaptureHook;
  readonly recallHook: RecallHook;
  readonly tools: ClawMemoryTools;
  private config: PluginConfig;

  constructor(config?: Partial<PluginConfig>) {
    this.config = {
      server_url: process.env.CLAWMEMORY_URL ?? 'http://127.0.0.1:7437',
      auto_capture: process.env.CLAWMEMORY_AUTO_CAPTURE !== 'false',
      auto_recall: process.env.CLAWMEMORY_AUTO_RECALL !== 'false',
      recall_limit: parseInt(process.env.CLAWMEMORY_RECALL_LIMIT ?? '5', 10),
      recall_threshold: parseFloat(process.env.CLAWMEMORY_RECALL_THRESHOLD ?? '0.1'),
      ...config,
    };

    this.client = new ClawMemoryClient(this.config.server_url);
    this.captureHook = new CaptureHook(this.client, this.config);
    this.recallHook = new RecallHook(this.client, this.config);
    this.tools = new ClawMemoryTools(this.client);
  }

  /**
   * Called after each assistant turn.
   * Captures facts from the conversation.
   */
  async postTurn(sessionId: string, turns: Turn[]): Promise<void> {
    await this.captureHook.onPostTurn(sessionId, turns);
  }

  /**
   * Called before each user turn.
   * Retrieves relevant memories to inject into context.
   *
   * @returns Context block string for system prompt injection, or '' if none
   */
  async preTurn(query: string): Promise<string> {
    const ctx = await this.recallHook.onPreTurn(query);
    return ctx.contextBlock;
  }

  /**
   * Handle slash commands from the chat interface.
   */
  async handleCommand(command: string, args: string): Promise<string> {
    switch (command) {
      case 'remember':
        return (await this.tools.remember(args)).message;
      case 'recall':
        return (await this.tools.recall(args)).message;
      case 'profile':
        return (await this.tools.profile()).message;
      case 'forget':
        return (await this.tools.forget(args)).message;
      case 'memory-stats':
        return (await this.tools.memoryStats()).message;
      default:
        return `Unknown ClawMemory command: ${command}`;
    }
  }

  /**
   * Check if the ClawMemory server is reachable.
   */
  async isHealthy(): Promise<boolean> {
    try {
      const health = await this.client.health();
      return health.status === 'ok';
    } catch {
      return false;
    }
  }
}

// Default singleton export for OpenClaw plugin loader
export default ClawMemoryPlugin;
