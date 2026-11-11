// Settings a strategy can push, with the values the RustDesk client understands.
// Keys and values come from reference/rustdesk (libs/base/src/config/keys.rs,
// src/hbbs_http/sync.rs tests, flutter desktop_setting_page.dart).
// "enable-*" options default to on, "allow-*" options default to off.

export type OptionDef = {
  key: string;
  label: string;
  help?: string;
  type: "bool" | "select" | "text" | "number";
  choices?: { value: string; label: string }[];
  placeholder?: string;
};

export type OptionSection = { title: string; options: OptionDef[] };

const onOff = (key: string, label: string, help?: string): OptionDef => ({ key, label, help, type: "bool" });

export const optionSections: OptionSection[] = [
  {
    title: "What remote users may do",
    options: [
      {
        key: "access-mode",
        label: "Permission preset",
        type: "select",
        choices: [
          { value: "custom", label: "Custom (use the switches below)" },
          { value: "full", label: "Full access" },
          { value: "view", label: "Screen share only" },
        ],
      },
      onOff("enable-keyboard", "Keyboard and mouse"),
      onOff("enable-clipboard", "Clipboard"),
      onOff("enable-file-transfer", "File transfer"),
      onOff("enable-audio", "Audio"),
      onOff("enable-camera", "Camera"),
      onOff("enable-terminal", "Terminal"),
      onOff("enable-tunnel", "TCP tunneling"),
      onOff("enable-remote-restart", "Restart the device"),
      onOff("enable-record-session", "Record sessions"),
      onOff("enable-block-input", "Block user input"),
      onOff("enable-privacy-mode", "Privacy mode"),
      onOff("enable-remote-printer", "Remote printer"),
      onOff("allow-remote-config-modification", "Change this device's RustDesk settings remotely"),
    ],
  },
  {
    title: "Accepting connections",
    options: [
      {
        key: "approve-mode",
        label: "Accept sessions",
        help: "Not managed = the device's own choice (both, by default).",
        type: "select",
        choices: [
          { value: "password", label: "Only with a password" },
          { value: "click", label: "Only by clicking Accept" },
        ],
      },
      {
        key: "verification-method",
        label: "Which password",
        type: "select",
        choices: [
          { value: "use-temporary-password", label: "One-time password only" },
          { value: "use-permanent-password", label: "Permanent password only" },
          { value: "use-both-passwords", label: "Both" },
        ],
      },
      {
        key: "temporary-password-length",
        label: "One-time password length",
        type: "select",
        choices: ["6", "8", "10"].map((v) => ({ value: v, label: `${v} characters` })),
      },
      onOff("allow-numeric-one-time-password", "Numeric one-time password"),
      onOff("allow-hide-cm", "Hide the connection window", "Only works when sessions are accepted with a password."),
      onOff("allow-only-conn-window-open", "Only accept connections while the RustDesk window is open"),
      {
        key: "whitelist",
        label: "IP whitelist",
        help: "Comma-separated IPs or ranges (e.g. 192.168.1.0/24). Empty = everyone.",
        type: "text",
        placeholder: "192.168.1.0/24,10.0.0.5",
      },
      {
        key: "id-whitelist",
        label: "ID whitelist",
        help: "Comma-separated RustDesk IDs allowed to connect.",
        type: "text",
      },
    ],
  },
  {
    title: "Sessions",
    options: [
      onOff("allow-auto-disconnect", "Disconnect idle sessions"),
      { key: "auto-disconnect-timeout", label: "Idle timeout (minutes)", type: "number", placeholder: "10" },
      onOff("allow-remove-wallpaper", "Remove wallpaper during sessions"),
      onOff("allow-scope-violation-close", "Close sessions that exceed their permissions"),
      onOff("allow-scope-violation-alarm", "Report sessions that exceed their permissions as alarms"),
    ],
  },
  {
    title: "Network",
    options: [
      onOff("enable-lan-discovery", "LAN discovery"),
      onOff("direct-server", "Direct IP access"),
      { key: "direct-access-port", label: "Direct access port", type: "number", placeholder: "21118" },
    ],
  },
];

export const knownKeys = new Set(optionSections.flatMap((s) => s.options.map((o) => o.key)));

/** Keys the client refuses (BLOCKED_CONFIG_OPTIONS in src/hbbs_http/sync.rs). */
export const blockedKeys = new Set([
  "custom-rendezvous-server", "rendezvous-servers", "api-server", "relay-server", "key", "ice-servers",
  "allow-insecure-tls-fallback", "allow-websocket", "websocket-port", "2fa", "bot",
  "preset-address-book-password", "enable-trusted-devices", "trusted-device-expiration-days",
  "preset-address-book-name", "preset-address-book-tag", "preset-address-book-alias", "preset-address-book-note",
  "preset-device-name", "preset-device-username", "preset-note", "windows-service-video-save-directory",
  "stop-service", "reg_recovery",
]);

export function describeOption(key: string, value: string): string {
  for (const s of optionSections) {
    const o = s.options.find((x) => x.key === key);
    if (!o) continue;
    if (o.type === "bool") return `${o.label}: ${value === "Y" ? "on" : "off"}`;
    if (o.type === "select") return `${o.label}: ${o.choices?.find((c) => c.value === value)?.label ?? value}`;
    return `${o.label}: ${value || "(empty)"}`;
  }
  return `${key} = ${value}`;
}
