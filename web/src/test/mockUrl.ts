import type {
  AuditActor,
  AuditEntry,
  ResetUrlResult,
  Setup,
  SetupStatus,
  WorkspaceView,
} from "../api/contract04";
import { ALLOWED_CIDRS_MAX, AUDIT_ACTIONS, GRACE_MINUTES_MAX } from "../api/contract04";
import { type AccessMock, errorResponse, jsonResponse } from "./mockAccess";

export interface UrlInit {
  /** Seeded audit rows, newest first. */
  audit?: AuditEntry[] | undefined;
  status?: SetupStatus | undefined;
}

/** What the URL routes need from the in-memory server around them. */
export interface UrlContext {
  access: AccessMock;
  host: string;
  actor: AuditActor;
  workspaces: () => WorkspaceView[];
  update: (w: WorkspaceView) => void;
}

export interface UrlMock {
  audit: AuditEntry[];
  status: SetupStatus;
  /** A workspace as the API sends it: the URL fields are null without workspace_url:read. */
  mask: (w: WorkspaceView) => WorkspaceView;
  route: (method: string, path: string, body: Record<string, unknown>) => Response | undefined;
}

const forbidden = () =>
  errorResponse(403, "forbidden", "not allowed", { reason: "no matching grant" });
const invalid = (problem: string) =>
  errorResponse(422, "validation_failed", "invalid request", { problems: [problem] });

const V4 = /^(\d{1,3})(\.\d{1,3}){3}(\/(\d|[12]\d|3[0-2]))?$/;
const V6 = /^[0-9a-f:]+(\/\d{1,3})?$/i;

/** A valid CIDR, or a bare IP (normalised to /32 or /128); undefined when it is neither. */
export function normaliseCidr(raw: string): string | undefined {
  const v = raw.trim();
  if (
    V4.test(v) &&
    v
      .split("/")[0]
      ?.split(".")
      .every((o) => Number(o) <= 255)
  ) {
    return v.includes("/") ? v : `${v}/32`;
  }
  if (v.includes(":") && V6.test(v)) return v.includes("/") ? v : `${v}/128`;
  return undefined;
}

let seq = 0;
const PAGE_DEFAULT = 50;

export function createUrlMock(ctx: UrlContext, init: UrlInit = {}): UrlMock {
  const m: UrlMock = {
    audit: structuredClone(init.audit ?? []),
    status: init.status ?? { firstCallAt: null, lastCallAt: null, callsToday: 0 },
    mask: (w) =>
      ctx.access.can("workspace_url", "read", w.id)
        ? w
        : { ...w, publicId: null, proxyUrl: null, allowedClientCidrs: null },
    route: (method, path, body) => handle(m, ctx, method, path, body),
  };
  return m;
}

function record(m: UrlMock, ctx: UrlContext, action: string, ws: WorkspaceView) {
  m.audit = [
    {
      id: `aud_${++seq}`,
      at: new Date().toISOString(),
      actor: ctx.actor,
      action,
      resourceType: "workspace",
      resourceId: ws.id,
      workspaceId: ws.id,
      ip: "127.0.0.1",
      metadata: {},
    },
    ...m.audit,
  ];
}

function snippets(url: string): Setup["snippets"] {
  return {
    openaiEnv: `export OPENAI_BASE_URL=${url}/v1`,
    anthropicEnv: `export ANTHROPIC_BASE_URL=${url}`,
    opencodeJson: JSON.stringify(
      { provider: { proxy: { npm: "@ai-sdk/openai-compatible", options: { baseURL: url } } } },
      null,
      2,
    ),
    claudeCodeEnv: `export ANTHROPIC_BASE_URL=${url}`,
    curl: `curl ${url}/v1/chat/completions -H "authorization: Bearer $OPENAI_API_KEY"`,
  };
}

function handle(
  m: UrlMock,
  ctx: UrlContext,
  method: string,
  path: string,
  body: Record<string, unknown>,
) {
  if (path.startsWith("/api/audit-log")) return auditLog(m, ctx, method, path);
  const r =
    /^\/api\/workspaces\/([^/]+)\/(setup|url\/reset|url\/retire-previous|url\/allowed-cidrs)$/.exec(
      path,
    );
  if (!r) return undefined;
  const ws = ctx.workspaces().find((w) => w.id === r[1]);
  if (!ws) return errorResponse(404, "not_found", "no such workspace");
  const need = r[2] === "setup" ? "read" : "manage";
  if (!ctx.access.can("workspace_url", need, ws.id)) return forbidden();
  if (r[2] === "setup" && method === "GET") {
    return jsonResponse({
      proxyUrl: ws.proxyUrl,
      snippets: snippets(String(ws.proxyUrl)),
      status: m.status,
    });
  }
  if (r[2] === "url/reset" && method === "POST") return reset(m, ctx, ws, body);
  if (r[2] === "url/retire-previous" && method === "POST") {
    record(m, ctx, "workspace.url_retire_previous", ws);
    return new Response(null, { status: 204 });
  }
  if (r[2] === "url/allowed-cidrs" && method === "PUT") return cidrs(m, ctx, ws, body);
  return undefined;
}

function reset(m: UrlMock, ctx: UrlContext, ws: WorkspaceView, body: Record<string, unknown>) {
  const grace = body.graceMinutes === undefined ? 10 : body.graceMinutes;
  if (!Number.isInteger(grace) || (grace as number) < 0 || (grace as number) > GRACE_MINUTES_MAX) {
    return invalid(`graceMinutes must be a whole number from 0 to ${GRACE_MINUTES_MAX}`);
  }
  const publicId = `rst${String(++seq).padStart(19, "0")}`;
  const proxyUrl = `https://${ctx.host}/w/${publicId}`;
  ctx.update({ ...ws, publicId, proxyUrl });
  record(m, ctx, "workspace.url_reset", ws);
  const result: ResetUrlResult = {
    proxyUrl,
    previousGraceUntil: new Date(Date.now() + (grace as number) * 60_000).toISOString(),
  };
  return jsonResponse(result);
}

function cidrs(m: UrlMock, ctx: UrlContext, ws: WorkspaceView, body: Record<string, unknown>) {
  const raw = body.cidrs;
  if (!Array.isArray(raw)) return invalid("cidrs must be a list");
  if (raw.length > ALLOWED_CIDRS_MAX) return invalid(`at most ${ALLOWED_CIDRS_MAX} entries`);
  const out: string[] = [];
  for (const c of raw) {
    const n = normaliseCidr(String(c));
    if (!n) return invalid(`${String(c)} is not a valid IP or CIDR`);
    out.push(n);
  }
  ctx.update({ ...ws, allowedClientCidrs: out });
  record(m, ctx, "workspace.allowed_cidrs_update", ws);
  return jsonResponse({ cidrs: out });
}

function auditLog(m: UrlMock, ctx: UrlContext, method: string, path: string) {
  if (method !== "GET") return undefined;
  if (!ctx.access.can("audit_log", "read")) return forbidden();
  const q = new URLSearchParams(path.split("?")[1] ?? "");
  const action = q.get("action");
  if (action && !(AUDIT_ACTIONS as readonly string[]).includes(action)) {
    return invalid(`unknown action ${action}`);
  }
  const limit = Math.min(Number(q.get("limit") || PAGE_DEFAULT), 200);
  const rows = m.audit.filter(
    (e) =>
      (!action || e.action === action) &&
      (!q.get("workspaceId") || e.workspaceId === q.get("workspaceId")),
  );
  const from = Number(q.get("cursor") || 0);
  const items = rows.slice(from, from + limit);
  const next = from + limit < rows.length ? String(from + limit) : null;
  return jsonResponse({ items, nextCursor: next });
}
