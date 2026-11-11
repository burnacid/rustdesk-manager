import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, ShieldCheck, Trash2 } from "lucide-react";
import { api } from "../lib/api";
import { dateTime, timeAgo } from "../lib/format";
import { createPasskey, passkeyError, passkeysSupported } from "../lib/passkey";
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
        <Passkeys />
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

type PasskeyRow = { id: number; name: string; created_at: string; last_used_at: string | null };

function Passkeys() {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const list = useQuery({ queryKey: ["passkeys"], queryFn: () => api<PasskeyRow[]>("/me/passkeys") });
  const add = useMutation({
    mutationFn: async () => {
      const begin = await api<{ ticket: string; options: { publicKey: never } }>("/me/passkeys/register/begin", { method: "POST" });
      let credential;
      try {
        credential = await createPasskey(begin.options);
      } catch (err) {
        throw passkeyError(err);
      }
      return api("/me/passkeys/register/finish", { method: "POST", body: { ticket: begin.ticket, name, credential } });
    },
    onSuccess: () => {
      setName("");
      qc.invalidateQueries({ queryKey: ["passkeys"] });
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) => api(`/me/passkeys/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["passkeys"] }),
  });
  const rows = list.data ?? [];

  return (
    <Card title="Passkeys" actions={rows.length > 0 ? <Badge tone="green">{rows.length}</Badge> : <Badge>None</Badge>}>
      <div className="space-y-4 p-4 text-sm">
        <p>Sign in to this console with your fingerprint, face, device PIN or a security key. No password or code needed.</p>
        {rows.length > 0 && (
          <ul className="divide-y divide-slate-200 rounded-md border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
            {rows.map((p) => (
              <li key={p.id} className="flex items-center gap-3 px-3 py-2">
                <KeyRound size={16} className="shrink-0 text-slate-400" />
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{p.name}</div>
                  <div className="text-xs text-slate-500" title={dateTime(p.created_at)}>
                    Added {timeAgo(p.created_at)}
                    {p.last_used_at && ` · last used ${timeAgo(p.last_used_at)}`}
                  </div>
                </div>
                <Button
                  size="sm"
                  aria-label={`Remove ${p.name}`}
                  disabled={remove.isPending}
                  onClick={() => confirm(`Remove the passkey "${p.name}"?`) && remove.mutate(p.id)}
                >
                  <Trash2 size={14} />
                </Button>
              </li>
            ))}
          </ul>
        )}
        {passkeysSupported() ? (
          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              add.mutate();
            }}
          >
            <div className="min-w-40 flex-1">
              <Field label="Name">
                <Input placeholder="e.g. Work laptop" value={name} onChange={(e) => setName(e.target.value)} maxLength={100} />
              </Field>
            </div>
            <Button variant="primary" type="submit" disabled={add.isPending}>
              Add passkey
            </Button>
          </form>
        ) : (
          <p className="text-slate-500">This browser doesn&apos;t support passkeys.</p>
        )}
        <ErrorText error={add.error ?? remove.error ?? list.error} />
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
