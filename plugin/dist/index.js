"use strict";
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
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __exportStar = (this && this.__exportStar) || function(m, exports) {
    for (var p in m) if (p !== "default" && !Object.prototype.hasOwnProperty.call(exports, p)) __createBinding(exports, m, p);
};
Object.defineProperty(exports, "__esModule", { value: true });
exports.ClawMemoryPlugin = exports.ClawMemoryTools = exports.RecallHook = exports.CaptureHook = exports.ClawMemoryClient = void 0;
const client_1 = require("./client");
const capture_1 = require("./capture");
const recall_1 = require("./recall");
const tools_1 = require("./tools");
var client_2 = require("./client");
Object.defineProperty(exports, "ClawMemoryClient", { enumerable: true, get: function () { return client_2.ClawMemoryClient; } });
var capture_2 = require("./capture");
Object.defineProperty(exports, "CaptureHook", { enumerable: true, get: function () { return capture_2.CaptureHook; } });
var recall_2 = require("./recall");
Object.defineProperty(exports, "RecallHook", { enumerable: true, get: function () { return recall_2.RecallHook; } });
var tools_2 = require("./tools");
Object.defineProperty(exports, "ClawMemoryTools", { enumerable: true, get: function () { return tools_2.ClawMemoryTools; } });
__exportStar(require("./types"), exports);
/**
 * ClawMemoryPlugin is the main entry point for the OpenClaw plugin.
 */
class ClawMemoryPlugin {
    constructor(config) {
        this.config = {
            server_url: process.env.CLAWMEMORY_URL ?? 'http://127.0.0.1:7437',
            auto_capture: process.env.CLAWMEMORY_AUTO_CAPTURE !== 'false',
            auto_recall: process.env.CLAWMEMORY_AUTO_RECALL !== 'false',
            recall_limit: parseInt(process.env.CLAWMEMORY_RECALL_LIMIT ?? '5', 10),
            recall_threshold: parseFloat(process.env.CLAWMEMORY_RECALL_THRESHOLD ?? '0.1'),
            ...config,
        };
        this.client = new client_1.ClawMemoryClient(this.config.server_url);
        this.captureHook = new capture_1.CaptureHook(this.client, this.config);
        this.recallHook = new recall_1.RecallHook(this.client, this.config);
        this.tools = new tools_1.ClawMemoryTools(this.client);
    }
    /**
     * Called after each assistant turn.
     * Captures facts from the conversation.
     */
    async postTurn(sessionId, turns) {
        await this.captureHook.onPostTurn(sessionId, turns);
    }
    /**
     * Called before each user turn.
     * Retrieves relevant memories to inject into context.
     *
     * @returns Context block string for system prompt injection, or '' if none
     */
    async preTurn(query) {
        const ctx = await this.recallHook.onPreTurn(query);
        return ctx.contextBlock;
    }
    /**
     * Handle slash commands from the chat interface.
     */
    async handleCommand(command, args) {
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
    async isHealthy() {
        try {
            const health = await this.client.health();
            return health.status === 'ok';
        }
        catch {
            return false;
        }
    }
}
exports.ClawMemoryPlugin = ClawMemoryPlugin;
// Default singleton export for OpenClaw plugin loader
exports.default = ClawMemoryPlugin;
//# sourceMappingURL=index.js.map