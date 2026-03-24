"use strict";
/**
 * ClawMemory slash commands — /remember, /recall, /profile, /forget, /memory-stats
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.ClawMemoryTools = void 0;
/**
 * ClawMemoryTools provides slash command handlers for OpenClaw integration.
 */
class ClawMemoryTools {
    constructor(client) {
        this.client = client;
    }
    /**
     * /remember <content> [--category X] [--importance 0.7]
     * Manually stores a fact in memory.
     */
    async remember(args) {
        const parsed = this.parseRememberArgs(args);
        if (!parsed.content) {
            return { success: false, message: 'Usage: /remember <content> [--category X] [--importance 0.7]' };
        }
        try {
            const result = await this.client.remember(parsed.content, parsed.category ?? 'general', parsed.container ?? 'general', parsed.importance ?? 0.7);
            let message = `✅ Remembered: "${result.content}" (${result.category}, importance: ${result.importance})`;
            if (result.contradictions && result.contradictions.length > 0) {
                message += `\n⚠️ Updated ${result.contradictions.length} conflicting fact(s)`;
            }
            return { success: true, message, data: result };
        }
        catch (err) {
            return { success: false, message: `❌ Failed to remember: ${err}` };
        }
    }
    /**
     * /recall <query> [--limit 10] [--container work]
     * Searches memory for relevant facts.
     */
    async recall(args) {
        const parsed = this.parseRecallArgs(args);
        if (!parsed.query) {
            return { success: false, message: 'Usage: /recall <query> [--limit 10]' };
        }
        try {
            const result = await this.client.recall(parsed.query, parsed.limit ?? 10, parsed.container ?? '', 0.0, false);
            if (result.results.length === 0) {
                return { success: true, message: `🔍 No memory found for "${parsed.query}"` };
            }
            const lines = result.results.map((r, i) => `${i + 1}. [${r.category}] ${r.content} (score: ${r.score.toFixed(3)})`);
            return {
                success: true,
                message: `🧠 Found ${result.results.length} memories for "${parsed.query}":\n${lines.join('\n')}`,
                data: result,
            };
        }
        catch (err) {
            return { success: false, message: `❌ Recall failed: ${err}` };
        }
    }
    /**
     * /profile
     * Displays the current user profile.
     */
    async profile() {
        try {
            const result = await this.client.profile();
            const entries = Object.entries(result.entries ?? {});
            if (entries.length === 0 && !result.summary) {
                return { success: true, message: '👤 No profile data yet. Chat more to build your profile!' };
            }
            const lines = ['👤 **User Profile**'];
            if (result.summary) {
                lines.push(`\n📝 ${result.summary}`);
            }
            for (const [key, value] of entries) {
                lines.push(`• **${key}**: ${value}`);
            }
            return { success: true, message: lines.join('\n'), data: result };
        }
        catch (err) {
            return { success: false, message: `❌ Profile failed: ${err}` };
        }
    }
    /**
     * /forget <query> [--max 5]
     * Removes facts matching a query from memory.
     */
    async forget(args) {
        const parts = args.trim().split(/\s+/);
        let query = '';
        let maxDelete = 5;
        for (let i = 0; i < parts.length; i++) {
            if (parts[i] === '--max' && i + 1 < parts.length) {
                maxDelete = parseInt(parts[i + 1], 10) || 5;
                i++;
            }
            else {
                query += (query ? ' ' : '') + parts[i];
            }
        }
        if (!query) {
            return { success: false, message: 'Usage: /forget <query> [--max 5]' };
        }
        try {
            const result = await this.client.forget(query, maxDelete);
            const count = result.deleted_count ?? 0;
            if (count === 0) {
                return { success: true, message: `🗑️ No facts found matching "${query}"` };
            }
            return {
                success: true,
                message: `🗑️ Forgot ${count} fact(s) matching "${query}"`,
                data: result,
            };
        }
        catch (err) {
            return { success: false, message: `❌ Forget failed: ${err}` };
        }
    }
    /**
     * /memory-stats
     * Displays memory statistics.
     */
    async memoryStats() {
        try {
            const stats = await this.client.stats();
            const dbSizeKB = Math.round((stats.db_size_bytes ?? 0) / 1024);
            const message = [
                '📊 **ClawMemory Stats**',
                `• Active facts: ${stats.active_facts}`,
                `• Total facts: ${stats.total_facts} (${stats.superseded_facts} superseded, ${stats.deleted_facts} deleted)`,
                `• Unprocessed turns: ${stats.unprocessed_turns}`,
                `• Profile entries: ${stats.profile_entries}`,
                `• DB size: ${dbSizeKB} KB`,
                `• Embedding dim: ${stats.embedding_dimension}`,
            ].join('\n');
            return { success: true, message, data: stats };
        }
        catch (err) {
            return { success: false, message: `❌ Stats failed: ${err}` };
        }
    }
    /** Parse /remember args. */
    parseRememberArgs(args) {
        const parts = args.trim().split(/\s+/);
        const result = {
            content: '',
        };
        const contentParts = [];
        for (let i = 0; i < parts.length; i++) {
            if (parts[i] === '--category' && i + 1 < parts.length) {
                result.category = parts[++i];
            }
            else if (parts[i] === '--container' && i + 1 < parts.length) {
                result.container = parts[++i];
            }
            else if (parts[i] === '--importance' && i + 1 < parts.length) {
                result.importance = parseFloat(parts[++i]) || 0.7;
            }
            else {
                contentParts.push(parts[i]);
            }
        }
        result.content = contentParts.join(' ');
        return result;
    }
    /** Parse /recall args. */
    parseRecallArgs(args) {
        const parts = args.trim().split(/\s+/);
        const result = { query: '' };
        const queryParts = [];
        for (let i = 0; i < parts.length; i++) {
            if (parts[i] === '--limit' && i + 1 < parts.length) {
                result.limit = parseInt(parts[++i], 10) || 10;
            }
            else if (parts[i] === '--container' && i + 1 < parts.length) {
                result.container = parts[++i];
            }
            else {
                queryParts.push(parts[i]);
            }
        }
        result.query = queryParts.join(' ');
        return result;
    }
}
exports.ClawMemoryTools = ClawMemoryTools;
//# sourceMappingURL=tools.js.map