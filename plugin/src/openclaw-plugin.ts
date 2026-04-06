/**
 * ClawMemory OpenClaw Plugin
 * Hooks: before_prompt_build (recall), agent_end (capture)
 * Commands: /remember /recall /memory-stats /forget /profile
 */

import { ClawMemoryClient } from "./client";
import type { RecallResult, ProfileResponse } from "./types";

// Minimal inline types — avoids compile-time dep on openclaw/plugin-sdk
type Logger = { info(m: string): void; warn(m: string): void; error(m: string): void };
type ServiceCtx = { logger: Logger; config: unknown; workspaceDir?: string; stateDir: string };
type CmdCtx = { args?: string; channel: string; isAuthorizedSender: boolean; commandBody: string; config: unknown };
type CmdResult = { text: string };
type Api = {
  registerService(s: { id: string; start(ctx: ServiceCtx): void | Promise<void> }): void;
  registerCommand(c: { name: string; description: string; acceptsArgs?: boolean; requireAuth?: boolean; handler(ctx: CmdCtx): CmdResult | Promise<CmdResult> }): void;
  on(hook: string, handler: (...a: unknown[]) => unknown, opts?: { priority?: number }): void;
};

const CLAWMEMORY_URL = process.env.CLAWMEMORY_URL ?? "http://127.0.0.1:7437";
const AUTO_RECALL   = process.env.CLAWMEMORY_AUTO_RECALL  !== "false";
const AUTO_CAPTURE  = process.env.CLAWMEMORY_AUTO_CAPTURE !== "false";
const RECALL_LIMIT  = parseInt(process.env.CLAWMEMORY_RECALL_LIMIT ?? "5", 10);

let _client: ClawMemoryClient | null = null;
const C = (): ClawMemoryClient => _client ?? (_client = new ClawMemoryClient(CLAWMEMORY_URL));

const toText = (c: unknown): string => {
  if (typeof c === "string") return c;
  if (Array.isArray(c)) return (c as Array<{type?: string; text?: string}>).filter(b => b.type === "text").map(b => b.text ?? "").join(" ");
  return "";
};

const plugin = {
  id: "clawmemory",
  name: "ClawMemory",
  version: "0.1.0",
  description: "Sovereign agent memory — BM25+vector hybrid recall, 100% local",
  configSchema: { safeParse: () => ({ success: true as const }) },

  register(api: Api) {
    // Health check on gateway start
    api.registerService({
      id: "clawmemory",
      async start(ctx: ServiceCtx) {
        try {
          const h = await C().health() as { store?: { active_facts?: number }; version?: string };
          ctx.logger.info(`[clawmemory] Server OK — v${h.version ?? "?"}, ${h.store?.active_facts ?? 0} facts`);
        } catch {
          ctx.logger.warn(`[clawmemory] Server not reachable at ${CLAWMEMORY_URL}. Start: clawmemory serve --port 7437`);
        }
      },
    });

    // Pre-turn recall
    if (AUTO_RECALL) {
      api.on("before_prompt_build", async (event: unknown) => {
        const ev = event as { prompt?: string };
        if (!ev.prompt || ev.prompt.trim().length < 5) return;
        try {
          const res = await C().recall(ev.prompt, RECALL_LIMIT);
          if (!res.results?.length) return;
          const block = res.results.map((f: RecallResult) =>
            `• ${f.content} (score: ${f.score.toFixed(2)})`
          ).join("\n");
          return { prependContext: `[ClawMemory — relevant context]\n${block}\n` };
        } catch { /* server down — skip silently */ }
      });
    }

    // Post-turn capture
    if (AUTO_CAPTURE) {
      api.on("agent_end", async (event: unknown) => {
        const ev = event as { success?: boolean; messages?: Array<{role: string; content: unknown}> };
        if (!ev.success) return;
        const msgs = ev.messages ?? [];
        const user = [...msgs].reverse().find(m => m.role === "user");
        const asst = [...msgs].reverse().find(m => m.role === "assistant");
        if (!user || !asst) return;
        const u = toText(user.content);
        const a = toText(asst.content);
        if (u.length < 20 && a.length < 50) return;
        try {
          await C().remember(`User: ${u.slice(0, 300)}\nAssistant: ${a.slice(0, 500)}`, "general", "work", 0.6);
        } catch { /* silent */ }
      });
    }

    // /remember
    api.registerCommand({
      name: "remember", description: "Store a fact in ClawMemory", acceptsArgs: true,
      async handler(ctx) {
        const content = ctx.args?.trim();
        if (!content) return { text: "Usage: /remember <fact>" };
        try { await C().remember(content, "general", "work"); return { text: `✅ Stored: "${content}"` }; }
        catch (e) { return { text: `❌ ${e instanceof Error ? e.message : String(e)}` }; }
      },
    });

    // /recall
    api.registerCommand({
      name: "recall", description: "Search ClawMemory", acceptsArgs: true,
      async handler(ctx) {
        const q = ctx.args?.trim();
        if (!q) return { text: "Usage: /recall <query>" };
        try {
          const res = await C().recall(q, 10);
          if (!res.results?.length) return { text: `No memories for: "${q}"` };
          return { text: `🧠 ${res.results.length} results:\n` + res.results.map((f: RecallResult, i: number) => `${i+1}. ${f.content} (${f.score.toFixed(3)})`).join("\n") };
        } catch (e) { return { text: `❌ ${e instanceof Error ? e.message : String(e)}` }; }
      },
    });

    // /memory-stats
    api.registerCommand({
      name: "memory-stats", description: "ClawMemory statistics", acceptsArgs: false,
      async handler() {
        try {
          const h = await C().health() as { version?: string; store?: { active_facts?: number; db_size_bytes?: number }; uptime_seconds?: number };
          return { text: `🧠 ClawMemory v${h.version ?? "?"}\nFacts: ${h.store?.active_facts ?? 0}\nDB: ${h.store?.db_size_bytes ? Math.round(h.store.db_size_bytes/1024)+"KB" : "?"}\nUptime: ${h.uptime_seconds ? Math.round(h.uptime_seconds/60)+"m" : "?"}` };
        } catch (e) { return { text: `❌ ${e instanceof Error ? e.message : String(e)}` }; }
      },
    });

    // /forget
    api.registerCommand({
      name: "forget", description: "Remove facts from ClawMemory", acceptsArgs: true,
      async handler(ctx) {
        const q = ctx.args?.trim();
        if (!q) return { text: "Usage: /forget <query>" };
        try {
          const r = await C().forget(q, 5) as { deleted_count?: number };
          return { text: `🗑️ Forgot ${r.deleted_count ?? 0} facts matching "${q}"` };
        } catch (e) { return { text: `❌ ${e instanceof Error ? e.message : String(e)}` }; }
      },
    });

    // /profile
    api.registerCommand({
      name: "profile", description: "Show ClawMemory profile", acceptsArgs: false,
      async handler() {
        try {
          const r: ProfileResponse = await C().profile();
          if (!r.entries || Object.keys(r.entries).length === 0) return { text: "No profile data yet." };
          return { text: `👤 Profile:\n` + Object.entries(r.entries).map(([k,v]) => `• ${k}: ${v}`).join("\n") };
        } catch (e) { return { text: `❌ ${e instanceof Error ? e.message : String(e)}` }; }
      },
    });
  },
};

export default plugin;
