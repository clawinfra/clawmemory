/**
 * ClawMemory slash commands — /remember, /recall, /profile, /forget, /memory-stats
 */
import { ClawMemoryClient } from './client';
/** Result of a tool/command invocation. */
export interface ToolResult {
    success: boolean;
    message: string;
    data?: unknown;
}
/**
 * ClawMemoryTools provides slash command handlers for OpenClaw integration.
 */
export declare class ClawMemoryTools {
    private client;
    constructor(client: ClawMemoryClient);
    /**
     * /remember <content> [--category X] [--importance 0.7]
     * Manually stores a fact in memory.
     */
    remember(args: string): Promise<ToolResult>;
    /**
     * /recall <query> [--limit 10] [--container work]
     * Searches memory for relevant facts.
     */
    recall(args: string): Promise<ToolResult>;
    /**
     * /profile
     * Displays the current user profile.
     */
    profile(): Promise<ToolResult>;
    /**
     * /forget <query> [--max 5]
     * Removes facts matching a query from memory.
     */
    forget(args: string): Promise<ToolResult>;
    /**
     * /memory-stats
     * Displays memory statistics.
     */
    memoryStats(): Promise<ToolResult>;
    /** Parse /remember args. */
    private parseRememberArgs;
    /** Parse /recall args. */
    private parseRecallArgs;
}
