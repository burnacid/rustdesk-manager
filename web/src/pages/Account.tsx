import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { ShieldCheck } from "lucide-react";
import { api } from "../lib/api";
import { useRefreshMe, useUser } from "../auth";
import { Badge, Button, Card, ErrorText, Field, Input, PageHeader } from "../components/ui";
import { ThemeSwitcher } from "../components/ThemeSwitcher";

export function Account() {
  const user = useUser();
  return (
    <>
      <PageHeader title="My account">
        Signed in as <b>{user.display_name || user.name}</b>
        {user.display_name && ` (${user.name})`}
      </PageHeader>
      <div className="grid max-w-4xl gap-6 lg:grid-cols-2">
        <TwoFactor />
        <ChangePassword />
        <Card title="Appearance">
          <div className="space-y-2 p-4 text-sm">
            <p className="text-slate-500 dark:text-slate-400">Saved in this browser. System follows your device&apos;s light or dark setting.</p>
            <ThemeSwitcher className="max-w-80" />
          </div>
        </Card>
      </div>
    </>
  );
}

function TwoFactor() {
  const user = useUser();
  const refresh = useRefreshMe();
  const [setup, setSetup] = useState<{ secret: string; qr: string } | null>(null);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const start = useMutation({
    mutationFn: () => api<{ secret: string; qr: string }>("/me/totp/setup", { method: "POST" }),
    onSuccess: setSetup,
  });
  const enable = useMutation({
    mutationFn: () => api("/me/totp/enable", { method: "POST", body: { code } }),
    onSuccess: () => {
      setSetup(null);
      setCode("");
      refresh();
    },
  });
  const disable = useMutation({
    mutationFn: () => api("/me/totp/disable", { method: "POST", body: { password } }),
    onSuccess: () => {
      setPassword("");
      refresh();
    },
  });

  return (
    <Card title="Two-factor authentication" actions={user.totp_enabled ? <Badge tone="green">On</Badge> : <Badge>Off</Badge>}>
      <div className="space-y-4 p-4 text-sm">
        {user.totp_enabled ? (
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              disable.mutate();
            }}
          >
            <p className="flex items-start gap-2">
              <ShieldCheck size={18} className="shrink-0 text-green-600" />
              Signing in to RustDesk and to this console asks for a code from your authenticator app.
            </p>
            <Field label="Password (to turn 2FA off)">
              <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="current-password" />
            </Field>
            <ErrorText error={disable.error} />
            <Button type="submit" disabled={disable.isPending}>
              Turn off
            </Button>
          </form>
        ) : setup ? (
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              enable.mutate();
            }}
          >
            <p>1. Scan this code with an authenticator app (Google Authenticator, Microsoft Authenticator, 1Password, …).</p>
            <img src={setup.qr} alt="QR code for your authenticator app" className="h-48 w-48 rounded bg-white p-2" />
            <p className="text-xs text-slate-500">
              Can&apos;t scan? Enter this key: <code className="break-all">{setup.secret}</code>
            </p>
            <Field label="2. Enter the 6-digit code it shows">
              <Input inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} required />
            </Field>
            <ErrorText error={enable.error} />
            <div className="flex gap-2">
              <Button variant="primary" type="submit" disabled={enable.isPending}>
                Turn on
              </Button>
              <Button type="button" onClick={() => setSetup(null)}>
                Cancel
              </Button>
            </div>
          </form>
        ) : (
          <>
            <p>Protect your account with a code from your phone, in addition to your password.</p>
            <ErrorText error={start.error} />
            <Button variant="primary" disabled={start.isPending} onClick={() => start.mutate()}>
              Set up
            </Button>
          </>
        )}
      </div>
    </Card>
  );
}

function ChangePassword() {
  const [f, setF] = useState({ current: "", next: "", repeat: "" });
  const [done, setDone] = useState(false);
  const save = useMutation({
    mutationFn: () => {
      if (f.next !== f.repeat) throw new Error("The new passwords don't match.");
      return api("/me/password", { method: "POST", body: { current: f.current, new: f.next } });
    },
    onSuccess: () => {
      setF({ current: "", next: "", repeat: "" });
      setDone(true);
    },
  });
  return (
    <Card title="Password">
      <form
        className="space-y-3 p-4"
        onSubmit={(e) => {
          e.preventDefault();
          setDone(false);
          save.mutate();
        }}
      >
        <Field label="Current password">
          <Input type="password" value={f.current} onChange={(e) => setF({ ...f, current: e.target.value })} required autoComplete="current-password" />
        </Field>
        <Field label="New password (at least 8 characters)">
          <Input type="password" value={f.next} onChange={(e) => setF({ ...f, next: e.target.value })} required minLength={8} autoComplete="new-password" />
        </Field>
        <Field label="Repeat new password">
          <Input type="password" value={f.repeat} onChange={(e) => setF({ ...f, repeat: e.target.value })} required autoComplete="new-password" />
        </Field>
        <ErrorText error={save.error} />
        {done && <p className="text-sm text-green-700 dark:text-green-400">Password changed. You were signed out of RustDesk and other browsers.</p>}
        <Button variant="primary" type="submit" disabled={save.isPending}>
          Change password
        </Button>
      </form>
    </Card>
  );
}
