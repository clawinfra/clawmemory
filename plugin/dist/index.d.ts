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
export declare class ClawMemoryPlugin {
    readonly client: ClawMemoryClient;
    readonly captureHook: CaptureHook;
    readonly recallHook: RecallHook;
    readonly tools: ClawMemoryTools;
    private config;
    constructor(config?: Partial<PluginConfig>);
    /**
     * Called after each assistant turn.
     * Captures facts from the conversation.
     */
    postTurn(sessionId: string, turns: Turn[]): Promise<void>;
    /**
     * Called before each user turn.
     * Retrieves relevant memories to inject into context.
     *
     * @returns Context block string for system prompt injection, or '' if none
     */
    preTurn(query: string): Promise<string>;
    /**
     * Handle slash commands from the chat interface.
     */
    handleCommand(command: string, args: string): Promise<string>;
    /**
     * Check if the ClawMemory server is reachable.
     */
    isHealthy(): Promise<boolean>;
}
export default ClawMemoryPlugin;
//# sourceMappingURL=index.d.ts.map