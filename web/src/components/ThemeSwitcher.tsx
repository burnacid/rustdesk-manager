import { Monitor, Moon, Sun } from "lucide-react";
import { useTheme, type Theme } from "../lib/theme";

const options: { value: Theme; label: string; icon: typeof Sun }[] = [
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
  { value: "system", label: "System", icon: Monitor },
];

/** Three-way Light / Dark / System switch. */
export function ThemeSwitcher({ className = "" }: { className?: string }) {
  const [theme, setTheme] = useTheme();
  return (
    <div role="group" aria-label="Color theme" className={`flex rounded-md border border-slate-200 p-0.5 dark:border-slate-700 ${className}`}>
      {options.map(({ value, label, icon: Icon }) => {
        const active = theme === value;
        return (
          <button
            key={value}
            type="button"
            aria-pressed={active}
            title={label}
            onClick={() => setTheme(value)}
            className={`flex min-h-10 flex-1 items-center justify-center gap-1.5 rounded px-2 text-xs md:min-h-8 ${
              active
                ? "bg-teal-50 font-medium text-teal-700 dark:bg-teal-950 dark:text-teal-300"
                : "text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
            }`}
          >
            <Icon size={14} aria-hidden />
            {label}
          </button>
        );
      })}
    </div>
  );
}
