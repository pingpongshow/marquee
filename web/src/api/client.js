import createClient, {} from "openapi-fetch";
const TOKEN_KEY = "marquee.token";
const CLIENT_ID_KEY = "marquee.clientId";
function storageGet(key) {
    try {
        return localStorage.getItem(key);
    }
    catch {
        return null;
    }
}
function storageSet(key, value) {
    try {
        if (value === null)
            localStorage.removeItem(key);
        else
            localStorage.setItem(key, value);
    }
    catch {
        /* storage unavailable (private mode); session lasts for this tab only */
    }
}
let token = storageGet(TOKEN_KEY);
const unauthorizedListeners = new Set();
export const session = {
    get token() {
        return token;
    },
    set(t) {
        token = t;
        storageSet(TOKEN_KEY, t);
    },
    onUnauthorized(fn) {
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
function randomId() {
    const bytes = crypto.getRandomValues(new Uint8Array(16));
    return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}
/** Stable per-browser identifier so each browser shows up as one device. */
export function clientId() {
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
        platform: "web",
        product: "Marquee Web",
        version: import.meta.env.VITE_APP_VERSION ?? "dev",
    };
}
const authMiddleware = {
    onRequest({ request }) {
        if (token)
            request.headers.set("Authorization", `Bearer ${token}`);
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
export const api = createClient({ baseUrl: "/api/v1" });
api.use(authMiddleware);
/**
 * URL for an artwork image at a display width. The token rides in the query string because
 * <img> can't send headers. Width is doubled for high-DPI screens and snapped server-side.
 */
export function imageUrl(artworkId, width) {
    const w = Math.round(width * Math.min(window.devicePixelRatio || 1, 2));
    return `/api/v1/images/${artworkId}?w=${w}&token=${encodeURIComponent(token ?? "")}`;
}
export function personPhotoUrl(personId, width) {
    const w = Math.round(width * Math.min(window.devicePixelRatio || 1, 2));
    return `/api/v1/people/${personId}/photo?w=${w}&token=${encodeURIComponent(token ?? "")}`;
}
/** Profile picture URL usable in <img> (signed-in viewers authenticate with the token parameter). */
export function avatarSrc(avatarUrl) {
    return token ? `${avatarUrl}&token=${encodeURIComponent(token)}` : avatarUrl;
}
export class ApiError extends Error {
    status;
    code;
    constructor(status, code, message) {
        super(message);
        this.status = status;
        this.code = code;
    }
}
/** Unwraps an openapi-fetch result, throwing ApiError on failure. */
export async function unwrap(p) {
    const { data, error, response } = await p;
    if (error !== undefined || !response.ok) {
        const e = (error ?? {});
        throw new ApiError(response.status, e.code ?? "error", e.message ?? response.statusText);
    }
    return data;
}
