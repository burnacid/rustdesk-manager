import { type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode, useEffect, useLayoutEffect, useRef } from "react";
import { X } from "lucide-react";

const cx = (...c: (string | false | undefined)[]) => c.filter(Boolean).join(" ");

export function PageHeader({ title, actions, children }: { title: string; actions?: ReactNode; children?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="text-2xl font-semibold">{title}</h1>
        {children && <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">{children}</p>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  );
}

export function Card({ title, children, className, actions }: { title?: string; children: ReactNode; className?: string; actions?: ReactNode }) {
  return (
    <section className={cx("rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900", className)}>
      {title && (
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 dark:border-slate-800">
          <h2 className="text-sm font-semibold">{title}</h2>
          {actions}
        </div>
      )}
      {children}
    </section>
  );
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "danger" | "ghost"; size?: "sm" | "md" };

export function Button({ variant = "secondary", size = "md", className, ...props }: ButtonProps) {
  return (
    <button
      {...props}
      className={cx(
        "inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50",
        size === "sm" ? "min-h-10 px-3 text-sm sm:min-h-0 sm:px-2.5 sm:py-1 sm:text-xs" : "min-h-11 px-4 text-base sm:min-h-0 sm:px-3.5 sm:py-2 sm:text-sm",
        variant === "primary" && "bg-teal-700 text-white hover:bg-teal-800",
        variant === "secondary" && "border border-slate-300 bg-white hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:hover:bg-slate-800",
        variant === "danger" && "bg-red-600 text-white hover:bg-red-700",
        variant === "ghost" && "text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800",
        className,
      )}
    />
  );
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={cx(
        "min-h-11 w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-base outline-none sm:min-h-0 sm:text-sm focus:border-teal-600 focus:ring-2 focus:ring-teal-600/20 dark:border-slate-700 dark:bg-slate-950",
        className,
      )}
    />
  );
}

export function Select({ className, children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      {...props}
      className={cx(
        "min-h-11 rounded-md border border-slate-300 bg-white px-3 py-2 text-base outline-none sm:min-h-0 sm:text-sm focus:border-teal-600 dark:border-slate-700 dark:bg-slate-950",
        className,
      )}
    >
      {children}
    </select>
  );
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm font-medium">{label}</span>
      {children}
    </label>
  );
}

export function Badge({ tone = "slate", children }: { tone?: "green" | "slate" | "red" | "accent" | "amber"; children: ReactNode }) {
  const tones = {
    green: "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300",
    slate: "bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300",
    red: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
    accent: "bg-teal-100 text-teal-800 dark:bg-teal-950 dark:text-teal-300",
    amber: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  };
  return <span className={cx("inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap", tones[tone])}>{children}</span>;
}

export function OnlineBadge({ online }: { online: boolean }) {
  return (
    <Badge tone={online ? "green" : "slate"}>
      <span className={cx("mr-1.5 h-1.5 w-1.5 rounded-full", online ? "bg-green-500" : "bg-slate-400")} />
      {online ? "Online" : "Offline"}
    </Badge>
  );
}

// On phones each row becomes a card: the "rtable" CSS (index.css) hides the header
// and shows each cell's column name from its data-label.
export function Table({ head, children, empty }: { head: ReactNode[]; children: ReactNode; empty?: boolean }) {
  const ref = useRef<HTMLTableElement>(null);
  useLayoutEffect(() => {
    const labels = head.map((h) => (typeof h === "string" ? h : ""));
    ref.current?.querySelectorAll("tbody tr").forEach((tr) => {
      Array.from(tr.children).forEach((td, i) => td.setAttribute("data-label", labels[i] ?? ""));
    });
  });
  return (
    <div className="sm:overflow-x-auto">
      <table ref={ref} className="rtable w-full text-left text-sm">
        <thead className="border-b border-slate-200 text-xs text-slate-500 uppercase dark:border-slate-800 dark:text-slate-400">
          <tr>
            {head.map((h, i) => (
              <th key={i} className="px-4 py-2.5 font-medium whitespace-nowrap">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100 dark:divide-slate-800">{children}</tbody>
      </table>
      {empty && <p className="px-4 py-10 text-center text-sm text-slate-500">Nothing here yet.</p>}
    </div>
  );
}

export const Td = ({ children, className }: { children?: ReactNode; className?: string }) => (
  <td className={cx("px-4 py-2.5 align-top", className)}>{children}</td>
);

export function Pagination({ page, pageSize, total, onPage }: { page: number; pageSize: number; total: number; onPage: (p: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-slate-200 px-4 py-2.5 text-sm text-slate-500 dark:border-slate-800">
      <span>{total} total</span>
      <div className="flex items-center gap-2">
        <Button size="sm" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          Previous
        </Button>
        <span>
          {page} / {pages}
        </span>
        <Button size="sm" disabled={page >= pages} onClick={() => onPage(page + 1)}>
          Next
        </Button>
      </div>
    </div>
  );
}

export function Modal({ title, open, onClose, children, wide }: { title: string; open: boolean; onClose: () => void; children: ReactNode; wide?: boolean }) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden"; // no background scrolling behind the dialog
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [open, onClose]);
  if (!open) return null;
  // Phones: a sheet from the bottom. Larger screens: centered. Long content scrolls inside.
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/40 sm:items-center sm:p-4" onClick={onClose}>
      <div
        role="dialog"
        aria-modal
        aria-label={title}
        className={cx(
          "flex max-h-[92dvh] w-full flex-col rounded-t-xl bg-white shadow-xl sm:max-h-[90dvh] sm:rounded-lg dark:bg-slate-900",
          wide ? "sm:max-w-2xl" : "sm:max-w-md",
        )}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex shrink-0 items-center justify-between border-b border-slate-200 py-1 pr-1 pl-4 dark:border-slate-800">
          <h2 className="font-semibold">{title}</h2>
          <button
            onClick={onClose}
            aria-label="Close"
            className="flex h-11 w-11 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-900 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            <X size={18} />
          </button>
        </div>
        <div className="overflow-y-auto overscroll-contain p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">{children}</div>
      </div>
    </div>
  );
}

export function ErrorText({ error }: { error: unknown }) {
  if (!error) return null;
  return <p className="text-sm text-red-600 dark:text-red-400">{error instanceof Error ? error.message : String(error)}</p>;
}

export function Loading() {
  return <p className="px-4 py-10 text-center text-sm text-slate-500">Loading…</p>;
}

export function Stat({ label, value, tone }: { label: string; value: ReactNode; tone?: "red" }) {
  return (
    <Card className="p-4">
      <p className="text-sm text-slate-500 dark:text-slate-400">{label}</p>
      <p className={cx("mt-1 text-2xl font-semibold sm:text-3xl", tone === "red" && "text-red-600 dark:text-red-400")}>{value}</p>
    </Card>
  );
}
