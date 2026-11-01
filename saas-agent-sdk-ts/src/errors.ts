/**
 * APIError is a typed error returned by the SaaS Agent API.
 *
 * The REST error body currently only carries a message string
 * (`{"error": "..."}`) — there is no explicit taxonomy code on the
 * wire for this transport (unlike the separate JSON-RPC layer).
 * `code` is therefore derived from `statusCode` using the same
 * mapping the server's writeMCPError uses, so callers get a stable,
 * switchable value instead of having to string-match `message`.
 *
 * Mirrors sdk/go/client.go's APIError 1:1.
 */
export class APIError extends Error {
  readonly code: string;
  readonly statusCode: number;

  constructor(code: string, message: string, statusCode: number) {
    super(`API error [${code}]: ${message} (status ${statusCode})`);
    this.name = "APIError";
    this.code = code;
    this.statusCode = statusCode;
    // Preserve the original message before it was formatted into
    // `Error.message` above, for callers that want just the text
    // the server sent.
    this.message = message === undefined ? "" : message;
    Object.setPrototypeOf(this, APIError.prototype);
  }

  /**
   * Formats the error the same way Go's `(*APIError).Error()` does:
   * `API error [<code>]: <message> (status <statusCode>)`.
   */
  override toString(): string {
    return `API error [${this.code}]: ${this.message} (status ${this.statusCode})`;
  }
}

/**
 * Derives a stable taxonomy code from the HTTP status code,
 * mirroring the server-side status mapping used by
 * `apiErrorCodeForStatus` in sdk/go/client.go.
 */
export function apiErrorCodeForStatus(statusCode: number): string {
  switch (statusCode) {
    case 400:
      return "invalid_request";
    case 401:
      return "unauthenticated";
    case 403:
      return "forbidden";
    case 404:
      return "tool_not_found";
    case 500:
      return "internal_error";
    default:
      return "unknown_error";
  }
}

/**
 * Builds an {@link APIError} from a failed fetch Response, mirroring
 * `(*Client).parseError` in sdk/go/client.go:
 *
 * - Reads the raw response body as text.
 * - Attempts to parse it as `{"error": "..."}` and uses that as the
 *   message when present.
 * - Falls back to the raw body text when the body isn't that shape
 *   (or isn't JSON at all).
 */
export async function parseError(response: Response): Promise<APIError> {
  const code = apiErrorCodeForStatus(response.status);
  let body: string;
  try {
    body = await response.text();
  } catch (err) {
    return new APIError(
      code,
      `failed to read error body: ${err instanceof Error ? err.message : String(err)}`,
      response.status,
    );
  }

  let message = body;
  try {
    const parsed = JSON.parse(body) as { error?: unknown };
    if (typeof parsed?.error === "string" && parsed.error !== "") {
      message = parsed.error;
    }
  } catch {
    // Not JSON (or not the expected shape) — fall back to the raw body,
    // matching the Go client's behavior.
  }

  return new APIError(code, message, response.status);
}
