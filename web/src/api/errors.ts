import { ERROR_STATUS, type ErrorBody, type ErrorCode } from "../contract/generated";

/** A body as read off the wire: the generated shape with the code widened to any string. */
export type WireErrorBody = Omit<ErrorBody, "code"> & { code: string };

/** A failed API call: the registry error body plus the HTTP status. */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly details: Record<string, unknown>;

  constructor(status: number, body: WireErrorBody) {
    super(body.message);
    this.name = "ApiError";
    this.code = body.code;
    this.status = status;
    this.details = body.details;
  }

  /** The list of problems (config or validation) the server put under details.problems. */
  get problems(): string[] {
    const p = this.details.problems;
    return Array.isArray(p) ? p.filter((x): x is string => typeof x === "string") : [];
  }
}

function isErrorBody(value: unknown): value is WireErrorBody {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return typeof v.code === "string" && typeof v.message === "string";
}

/** Builds an ApiError from a failed response, tolerating non-JSON bodies. */
export async function toApiError(res: Response): Promise<ApiError> {
  try {
    const body: unknown = await res.json();
    if (isErrorBody(body)) {
      const details = typeof body.details === "object" && body.details !== null ? body.details : {};
      return new ApiError(res.status, { code: body.code, message: body.message, details });
    }
  } catch {
    // fall through to the generic error
  }
  return new ApiError(res.status, {
    code: "internal",
    message: res.statusText || "Request failed",
    details: {},
  });
}

/** Human-facing summary of an API error code; unknown codes degrade to a generic line. */
export function describeError(code: string): string {
  if (!(code in ERROR_STATUS)) return "Something went wrong.";
  const status = ERROR_STATUS[code as ErrorCode];
  return status >= 500 ? "The server hit a problem. Try again." : "The request was not accepted.";
}

/**
 * The readable line for a failed call: the message mapped to its code, else the first 422 problem
 * (or message), else the fallback. Screens pass their own wording; none re-implement the branching.
 */
export function describeApiError(
  e: unknown,
  { byCode = {}, fallback }: { byCode?: Record<string, string>; fallback: string },
): string {
  if (e instanceof ApiError) {
    const mapped = byCode[e.code];
    if (mapped !== undefined) return mapped;
    if (e.status === 422) return e.problems[0] ?? e.message;
  }
  return fallback;
}
