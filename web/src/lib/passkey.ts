// WebAuthn helpers: the server sends options with binary fields as base64url
// strings; the browser API wants ArrayBuffers, and the answer goes back as JSON.

/* eslint-disable @typescript-eslint/no-explicit-any */

const toBuf = (s: string): ArrayBuffer => {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(s.length / 4) * 4, "=");
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
};

const fromBuf = (b: ArrayBuffer | null | undefined): string | undefined => {
  if (!b) return undefined;
  let s = "";
  for (const c of new Uint8Array(b)) s += String.fromCharCode(c);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
};

const descriptors = (list?: any[]) => list?.map((c) => ({ ...c, id: toBuf(c.id) }));

export const passkeysSupported = () => typeof window !== "undefined" && !!window.PublicKeyCredential;

/** Runs navigator.credentials.create with the server's options; returns the JSON to send back. */
export async function createPasskey(options: { publicKey: any }) {
  const pk = options.publicKey;
  const cred = (await navigator.credentials.create({
    publicKey: {
      ...pk,
      challenge: toBuf(pk.challenge),
      user: { ...pk.user, id: toBuf(pk.user.id) },
      excludeCredentials: descriptors(pk.excludeCredentials),
    },
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error("No passkey was created.");
  const r = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id,
    rawId: fromBuf(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: fromBuf(r.clientDataJSON),
      attestationObject: fromBuf(r.attestationObject),
      transports: r.getTransports?.() ?? [],
    },
  };
}

/** Runs navigator.credentials.get with the server's options; returns the JSON to send back. */
export async function getPasskey(options: { publicKey: any }) {
  const pk = options.publicKey;
  const cred = (await navigator.credentials.get({
    publicKey: { ...pk, challenge: toBuf(pk.challenge), allowCredentials: descriptors(pk.allowCredentials) },
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error("No passkey was chosen.");
  const r = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id,
    rawId: fromBuf(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: fromBuf(r.clientDataJSON),
      authenticatorData: fromBuf(r.authenticatorData),
      signature: fromBuf(r.signature),
      userHandle: fromBuf(r.userHandle),
    },
  };
}

/** Turns the browser's "cancelled" error into a friendlier message. */
export function passkeyError(err: unknown): unknown {
  if (err instanceof DOMException && err.name === "NotAllowedError") return new Error("Cancelled, or the passkey wasn't allowed.");
  if (err instanceof DOMException && err.name === "InvalidStateError") return new Error("This passkey is already registered.");
  if (err instanceof DOMException && err.name === "SecurityError")
    return new Error("Passkeys only work when this console is opened at its PUBLIC_URL address over HTTPS.");
  return err;
}
