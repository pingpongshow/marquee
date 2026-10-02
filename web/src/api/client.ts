import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "./schema.gen";

const TOKEN_KEY = "marquee.token";
const CLIENT_ID_KEY = "marquee.clientId";

function storageGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function storageSet(key: string, value: string | null) {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    /* storage unavailable (private mode); session lasts for this tab only */
  }
}

let token: string | null = storageGet(TOKEN_KEY);
/** Image-only key from /me (D85); image URLs fall back to the token until it is known. */
let imageKey: string | null = null;
const unauthorizedListeners = new Set<() => void>();

export const session = {
  get token() {
    return token;
  },
  set(t: string | null) {
    token = t;
    imageKey = null;
    storageSet(TOKEN_KEY, t);
  },
  setImageKey(k: string | null | undefined) {
    imageKey = k || null;
  },
  onUnauthorized(fn: () => void) {
    unauthorizedListeners.add(fn);
    return () => {
      unauthorizedListeners.delete(fn);
    };
  },
};

/**
 * Random hex ID. crypto.randomUUID only exists in secure contexts (HTTPS/localhost), and
 * LAN clients use plain http://10.1.1.10, so use getRandomValues, which works everywhere.
 */
function randomId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/** Stable per-browser identifier so each browser shows up as one device. */
export function clientId(): string {
  let id = storageGet(CLIENT_ID_KEY);
  if (!id) {
    id = randomId();
    storageSet(CLIENT_ID_KEY, id);
  }
  return id;
}

export function deviceInfo() {
  const ua = navigator.userAgent;
  const browser = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : "Browser";
  const os = /Mac OS X/.test(ua) ? "macOS" : /Windows/.test(ua) ? "Windows" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Linux/.test(ua) ? "Linux" : "";
  return {
    clientId: clientId(),
    name: os ? `${browser} on ${os}` : browser,
    platform: "web" as const,
    product: "Marquee Web",
    version: import.meta.env.VITE_APP_VERSION ?? "dev",
  };
}

const authMiddleware: Middleware = {
  onRequest({ request }) {
    if (token) request.headers.set("Authorization", `Bearer ${token}`);
    return request;
  },
  onResponse({ response, request }) {
    if (response.status === 401 && request.headers.has("Authorization")) {
      session.set(null);
      unauthorizedListeners.forEach((fn) => fn());
    }
    return response;
  },
};

export const api = createClient<paths>({ baseUrl: "/api/v1" });
api.use(authMiddleware);

/** Query parameter authenticating an <img>: the image key once /me has loaded, else the token. */
function imageAuth(): string {
  if (imageKey) return `key=${encodeURIComponent(imageKey)}`;
  return `token=${encodeURIComponent(token ?? "")}`;
}

/**
 * URL for an artwork image at a display width. The image key rides in the query string because
 * <img> can't send headers. Width is doubled for high-DPI screens and snapped server-side.
 */
export function imageUrl(artworkId: number, width: number) {
  const w = Math.round(width * Math.min(window.devicePixelRatio || 1, 2));
  return `/api/v1/images/${artworkId}?w=${w}&${imageAuth()}`;
}

export function personPhotoUrl(personId: number, width: number) {
  const w = Math.round(width * Math.min(window.devicePixelRatio || 1, 2));
  return `/api/v1/people/${personId}/photo?w=${w}&${imageAuth()}`;
}

/** A seek-preview sprite sheet (PLAY-13). */
export function trickplaySheetUrl(itemId: number, sheet: number) {
  return `/api/v1/items/${itemId}/trickplay/${sheet}?token=${encodeURIComponent(token ?? "")}`;
}

/** Profile picture URL usable in <img> (signed-in viewers authenticate with the image key or token). */
export function avatarSrc(avatarUrl: string) {
  return imageKey || token ? `${avatarUrl}&${imageAuth()}` : avatarUrl;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

/** Unwraps an openapi-fetch result, throwing ApiError on failure. */
export async function unwrap<T>(p: Promise<{ data?: T; error?: unknown; response: Response }>): Promise<T> {
  const { data, error, response } = await p;
  if (error !== undefined || !response.ok) {
    const e = (error ?? {}) as { code?: string; message?: string };
    throw new ApiError(response.status, e.code ?? "error", e.message ?? response.statusText);
  }
  return data as T;
}
