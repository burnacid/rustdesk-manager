import { useEffect, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type User } from "../lib/api";
import { Button, ErrorText, Field, Input } from "../components/ui";
import { Logo } from "../components/Logo";
import { ThemeSwitcher } from "../components/ThemeSwitcher";

type LoginResp = { user?: User; tfa_required?: boolean; ticket?: string };

const providerLabel = (name: string) =>
  ({ azure: "Microsoft", microsoft: "Microsoft", github: "GitHub", gitlab: "GitLab" })[name.toLowerCase()] ??
  name.charAt(0).toUpperCase() + name.slice(1);

export function Login({ onLoggedIn }: { onLoggedIn: (u: User) => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [ticket, setTicket] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const providers = useQuery({ queryKey: ["sso-providers"], queryFn: () => api<{ name: string }[]>("/auth/sso") });

  // Errors from an SSO round trip come back as ?sso_error=...
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const e = params.get("sso_error");
    if (e) {
      setError(new Error(e));
      window.history.replaceState(null, "", window.location.pathname);
    }
  }, []);

  async function run(f: () => Promise<void>) {
    setBusy(true);
    setError(null);
    try {
      await f();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  const submitPassword = (e: FormEvent) => {
    e.preventDefault();
    run(async () => {
      const res = await api<LoginResp>("/auth/login", { method: "POST", body: { username, password } });
      if (res.tfa_required && res.ticket) setTicket(res.ticket);
      else if (res.user) onLoggedIn(res.user);
    });
  };

  const submitCode = (e: FormEvent) => {
    e.preventDefault();
    run(async () => {
      const res = await api<LoginResp>("/auth/login/2fa", { method: "POST", body: { ticket, code } });
      if (res.user) onLoggedIn(res.user);
    });
  };

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 p-4">
      <div className="w-full max-w-sm space-y-4 rounded-lg border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
        <div>
          <h1 className="mb-2 text-xl">
            <Logo size={36} />
          </h1>
          <p className="text-sm text-slate-500 dark:text-slate-400">{ticket ? "Enter the 6-digit code from your authenticator app." : "Sign in to continue."}</p>
        </div>
        {ticket ? (
          <form onSubmit={submitCode} className="space-y-4">
            <Field label="Code">
              <Input
                autoFocus
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9 ]{6,7}"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                required
              />
            </Field>
            <ErrorText error={error} />
            <Button variant="primary" type="submit" disabled={busy} className="w-full">
              {busy ? "Checking…" : "Verify"}
            </Button>
            <button
              type="button"
              className="w-full text-sm text-slate-500 hover:underline"
              onClick={() => {
                setTicket(null);
                setCode("");
                setError(null);
              }}
            >
              Back
            </button>
          </form>
        ) : (
          <>
            <form onSubmit={submitPassword} className="space-y-4">
              <Field label="Username">
                <Input autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
              </Field>
              <Field label="Password">
                <Input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
              </Field>
              <ErrorText error={error} />
              <Button variant="primary" type="submit" disabled={busy} className="w-full">
                {busy ? "Signing in…" : "Sign in"}
              </Button>
            </form>
            {providers.data && providers.data.length > 0 && (
              <div className="space-y-2 border-t border-slate-200 pt-4 dark:border-slate-800">
                {providers.data.map((p) => (
                  <a
                    key={p.name}
                    href={`/admin/api/auth/sso/${encodeURIComponent(p.name)}`}
                    className="flex w-full items-center justify-center rounded-md border border-slate-300 px-3.5 py-2 text-sm font-medium hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-800"
                  >
                    Continue with {providerLabel(p.name)}
                  </a>
                ))}
              </div>
            )}
          </>
        )}
      </div>
      <ThemeSwitcher className="w-full max-w-60" />
    </div>
  );
}
