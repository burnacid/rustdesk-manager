// The "Fleet" mark: stacked screens with a pointer. Same artwork as public/logo.svg.
export function LogoMark({ size = 32 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" aria-hidden="true" className="shrink-0">
      <rect x="21" y="5" width="38" height="28" rx="6" fill="#0f766e" opacity="0.35" />
      <rect x="13.5" y="13" width="38" height="29" rx="6.5" fill="#0f766e" opacity="0.65" />
      <rect x="5" y="22" width="40" height="32" rx="7" fill="#0f766e" />
      <path d="M17 29 V47 L21.8 42.4 L25.4 49.6 L29.2 47.9 L25.8 41 H32.4 Z" fill="#fff" stroke="#fff" strokeWidth="1.5" strokeLinejoin="round" />
    </svg>
  );
}

/** Mark plus name, as on the design board. */
export function Logo({ size = 32 }: { size?: number }) {
  return (
    <span className="flex items-center gap-2.5">
      <LogoMark size={size} />
      <span className="font-semibold tracking-tight">
        RustDesk <span className="font-normal text-slate-500 dark:text-slate-400">Manager</span>
      </span>
    </span>
  );
}
