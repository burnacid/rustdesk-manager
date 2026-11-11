// Human labels for the numeric codes the RustDesk client sends
// (reference/rustdesk/src/server/connection.rs).

export const connTypes: Record<number, string> = {
  0: "Remote control",
  1: "File transfer",
  2: "Port forward",
  3: "Camera",
  4: "Terminal",
};

export const primaryAuths: Record<number, string> = {
  0: "",
  1: "Accepted by click",
  2: "One-time password",
  3: "Permanent password",
  4: "Switch sides",
};

export const twoFactors: Record<number, string> = { 0: "", 1: "2FA code", 2: "Trusted device" };

export const fileDirections: Record<number, string> = {
  0: "Device → peer",
  1: "Peer → device",
};

export const alarmTypes: Record<number, string> = {
  0: "Blocked by IP whitelist",
  1: "Over 30 failed password attempts",
  2: "6 failed attempts within 1 minute",
  6: "Many attempts from one IPv6 prefix",
  7: "Terminal OS login back-off",
  8: "Terminal OS login concurrency limit",
  9: "Session scope violation",
  10: "Blocked by ID whitelist",
};

export const label = (map: Record<number, string>, v: number) => map[v] ?? `Unknown (${v})`;

export const shareRules: Record<number, string> = { 1: "Read only", 2: "Read/write", 3: "Full control" };

/** Flutter stores colors as ARGB integers (0xFFRRGGBB). Avoid JS 32-bit bitwise ops. */
export const tagColorCss = (c: number) => `#${(c % 0x1000000).toString(16).padStart(6, "0")}`;
export const cssToTagColor = (hex: string) => 0xff000000 + parseInt(hex.slice(1), 16);
