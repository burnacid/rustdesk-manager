export function dateTime(s: string | null | undefined): string {
  if (!s) return "—";
  return new Date(s).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function timeAgo(s: string | null | undefined): string {
  if (!s) return "never";
  const sec = Math.round((Date.now() - new Date(s).getTime()) / 1000);
  if (sec < 60) return "just now";
  const min = Math.round(sec / 60);
  if (min < 60) return `${min} min ago`;
  const h = Math.round(min / 60);
  if (h < 48) return `${h} h ago`;
  return `${Math.round(h / 24)} days ago`;
}

export function duration(from: string, to: string | null): string {
  const ms = (to ? new Date(to).getTime() : Date.now()) - new Date(from).getTime();
  const min = Math.floor(ms / 60000);
  if (min < 1) return "< 1 min";
  if (min < 60) return `${min} min`;
  return `${Math.floor(min / 60)} h ${min % 60} min`;
}

export function bytes(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

/** Short OS name from the client's "windows / Windows 11 Pro - 26300" format. */
export function osShort(os: string): string {
  const parts = os.split(" / ");
  return (parts[1] ?? parts[0] ?? "").split(" - ")[0] || "—";
}
