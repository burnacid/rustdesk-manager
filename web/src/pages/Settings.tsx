import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Plus } from "lucide-react";
import { api, type DeployToken, type OIDCProvider, type ServerSettings } from "../lib/api";
import { dateTime } from "../lib/format";
import { Badge, Button, Card, ErrorText, Field, Input, Loading, Modal, PageHeader, Select, Table, Td } from "../components/ui";

export function Settings() {
  return (
    <>
      <PageHeader title="Settings" />
      <div className="space-y-6">
        <General />
        <Providers />
        <DeployTokens />
      </div>
    </>
  );
}

function Toggle({ label, help, checked, onChange, disabled }: { label: string; help: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <label className="flex items-start gap-3 px-4 py-3">
      <input type="checkbox" className="mt-1" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span className="text-sm">
        <span className="font-medium">{label}</span>
        <span className="block text-slate-500 dark:text-slate-400">{help}</span>
      </span>
    </label>
  );
}

function General() {
  const qc = useQueryClient();
  const s = useQuery({ queryKey: ["settings"], queryFn: () => api<ServerSettings>("/settings") });
  const save = useMutation({
    mutationFn: (body: Partial<ServerSettings>) => api<ServerSettings>("/settings", { method: "PATCH", body }),
    onSuccess: (data) => qc.setQueryData(["settings"], data),
  });
  if (s.isPending) return <Loading />;
  if (s.error) return <ErrorText error={s.error} />;
  return (
    <Card title="General">
      <div className="divide-y divide-slate-100 dark:divide-slate-800">
        <Toggle
          label="Only accept registered devices"
          help="Machines must be registered with a deploy token (rustdesk --assign) before they appear. Devices already listed keep working."
          checked={s.data.require_registered}
          disabled={save.isPending}
          onChange={(v) => save.mutate({ require_registered: v })}
        />
        <Toggle
          label="Accept session recording uploads"
          help="The standard RustDesk app never uploads recordings; only custom-built clients do. Leave this off unless you use one."
          checked={s.data.accept_recordings}
          disabled={save.isPending}
          onChange={(v) => save.mutate({ accept_recordings: v })}
        />
        <div className="px-4 py-3 text-sm">
          <span className="font-medium">Public URL</span>
          <span className="block font-mono text-slate-500">{s.data.public_url}</span>
          <span className="block text-xs text-slate-500">Set with the PUBLIC_URL environment variable. Single sign-on redirects use it.</span>
        </div>
      </div>
      <div className="px-4 pb-3">
        <ErrorText error={save.error} />
      </div>
    </Card>
  );
}

function CopyText({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <span className="flex items-center gap-2">
      <code className="min-w-0 flex-1 rounded bg-slate-100 px-2 py-1 text-xs break-all dark:bg-slate-950">{text}</code>
      <Button
        size="sm"
        type="button"
        onClick={() => {
          navigator.clipboard?.writeText(text).then(() => setCopied(true), () => {});
          setTimeout(() => setCopied(false), 1500);
        }}
      >
        <Copy size={12} /> {copied ? "Copied" : "Copy"}
      </Button>
    </span>
  );
}

function Providers() {
  const settings = useQuery({ queryKey: ["settings"], queryFn: () => api<ServerSettings>("/settings") });
  const list = useQuery({ queryKey: ["oidc-providers"], queryFn: () => api<OIDCProvider[]>("/oidc-providers") });
  const [editing, setEditing] = useState<{ p: OIDCProvider | null } | null>(null);
  return (
    <Card
      title="Single sign-on (OpenID Connect)"
      actions={
        <Button size="sm" variant="primary" onClick={() => setEditing({ p: null })}>
          <Plus size={14} /> Add provider
        </Button>
      }
    >
      <div className="space-y-2 border-b border-slate-200 px-4 py-3 text-sm dark:border-slate-800">
        <p>
          Lets people sign in to RustDesk and this console with Google, Microsoft Entra, Keycloak, Authentik and other OpenID Connect providers.
          Register this redirect URL at the provider:
        </p>
        {settings.data && <CopyText text={settings.data.oidc_redirect_url} />}
      </div>
      {list.isPending ? (
        <Loading />
      ) : list.error ? (
        <div className="p-4">
          <ErrorText error={list.error} />
        </div>
      ) : (
        <Table head={["Name", "Issuer", "New users", ""]} empty={list.data.length === 0}>
          {list.data.map((p) => (
            <tr key={p.id}>
              <Td>
                <div className="flex items-center gap-2 font-medium">
                  {p.name} {!p.enabled && <Badge>Off</Badge>}
                </div>
              </Td>
              <Td className="font-mono text-xs break-all">{p.issuer}</Td>
              <Td>{p.auto_create ? "Created on first login" : "Must exist (matched by email)"}</Td>
              <Td className="text-right">
                <Button size="sm" variant="ghost" onClick={() => setEditing({ p })}>
                  Edit
                </Button>
              </Td>
            </tr>
          ))}
        </Table>
      )}
      {editing && <ProviderForm provider={editing.p} onClose={() => setEditing(null)} />}
    </Card>
  );
}

const issuerHints: Record<string, string> = {
  google: "https://accounts.google.com",
  azure: "https://login.microsoftonline.com/<tenant-id>/v2.0",
  keycloak: "https://keycloak.example.com/realms/<realm>",
};

function ProviderForm({ provider, onClose }: { provider: OIDCProvider | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [f, setF] = useState({
    name: provider?.name ?? "",
    issuer: provider?.issuer ?? "",
    client_id: provider?.client_id ?? "",
    client_secret: "",
    scopes: provider?.scopes ?? "",
    auto_create: provider?.auto_create ?? false,
    trust_email: provider?.trust_email ?? false,
    enabled: provider?.enabled ?? true,
  });
  const [confirmDelete, setConfirmDelete] = useState(false);
  const done = () => {
    qc.invalidateQueries({ queryKey: ["oidc-providers"] });
    onClose();
  };
  const save = useMutation({
    mutationFn: () => (provider ? api(`/oidc-providers/${provider.id}`, { method: "PATCH", body: f }) : api("/oidc-providers", { method: "POST", body: f })),
    onSuccess: done,
  });
  const remove = useMutation({ mutationFn: () => api(`/oidc-providers/${provider!.id}`, { method: "DELETE" }), onSuccess: done });
  const text = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) => setF({ ...f, [k]: e.target.value });
  const check = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) => setF({ ...f, [k]: e.target.checked });

  return (
    <Modal title={provider ? `Edit ${provider.name}` : "Add sign-in provider"} open onClose={onClose} wide>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Name (button label; google, azure, okta… get an icon)">
            <Input value={f.name} onChange={text("name")} required pattern="[a-zA-Z0-9_\-]{1,32}" />
          </Field>
          <Field label="Issuer URL">
            <Input value={f.issuer} onChange={text("issuer")} required placeholder={issuerHints[f.name.toLowerCase()] ?? "https://…"} />
          </Field>
          <Field label="Client ID">
            <Input value={f.client_id} onChange={text("client_id")} required />
          </Field>
          <Field label={provider?.has_secret ? "Client secret (saved; type to replace)" : "Client secret"}>
            <Input type="password" value={f.client_secret} onChange={text("client_secret")} autoComplete="new-password" />
          </Field>
        </div>
        <Field label="Extra scopes (optional, besides openid email profile)">
          <Input value={f.scopes} onChange={text("scopes")} />
        </Field>
        <div className="space-y-2 text-sm">
          <label className="flex items-start gap-2">
            <input type="checkbox" className="mt-1" checked={f.auto_create} onChange={check("auto_create")} />
            <span>
              Create a user on first sign-in
              <span className="block text-xs text-slate-500">Off: only people who already have a user here (same email) can sign in.</span>
            </span>
          </label>
          <label className="flex items-start gap-2">
            <input type="checkbox" className="mt-1" checked={f.trust_email} onChange={check("trust_email")} />
            <span>
              Trust email addresses from this provider
              <span className="block text-xs text-slate-500">
                Needed for Microsoft Entra, which doesn&apos;t mark emails as verified. Only turn on for a provider whose users can&apos;t pick any email address.
              </span>
            </span>
          </label>
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={f.enabled} onChange={check("enabled")} /> Enabled
          </label>
        </div>
        <ErrorText error={save.error ?? remove.error} />
        <div className="flex justify-between gap-2 pt-2">
          {provider ? (
            confirmDelete ? (
              <Button type="button" variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
                Really delete?
              </Button>
            ) : (
              <Button type="button" variant="ghost" className="text-red-600" onClick={() => setConfirmDelete(true)}>
                Delete
              </Button>
            )
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            <Button type="button" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={save.isPending}>
              Save
            </Button>
          </div>
        </div>
      </form>
    </Modal>
  );
}

function DeployTokens() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["deploy-tokens"], queryFn: () => api<DeployToken[]>("/deploy-tokens") });
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [days, setDays] = useState("0");
  const [created, setCreated] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => api<{ token: string }>("/deploy-tokens", { method: "POST", body: { name, expires_days: Number(days) } }),
    onSuccess: (res) => {
      setCreated(res.token);
      setName("");
      qc.invalidateQueries({ queryKey: ["deploy-tokens"] });
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) => api(`/deploy-tokens/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["deploy-tokens"] }),
  });
  const example = `rustdesk.exe --assign --token ${created ?? "<token>"} --device_group_name "Office" --user_name alice`;

  return (
    <Card
      title="Deploy tokens"
      actions={
        <Button size="sm" variant="primary" onClick={() => setCreating(true)}>
          <Plus size={14} /> New token
        </Button>
      }
    >
      <div className="space-y-2 border-b border-slate-200 px-4 py-3 text-sm dark:border-slate-800">
        <p>
          Used by install scripts to register a machine and assign its owner, group, strategy or address book. Run as administrator on an
          installed RustDesk:
        </p>
        <CopyText text={example} />
        <p className="text-xs text-slate-500">
          Other options: --strategy_name, --address_book_name (with --address_book_tag, --address_book_alias, --address_book_password), --note,
          --device_name, --device_username.
        </p>
      </div>
      {list.isPending ? (
        <Loading />
      ) : (
        <Table head={["Name", "Token", "Expires", "Created", ""]} empty={(list.data ?? []).length === 0}>
          {list.data?.map((t) => (
            <tr key={t.id}>
              <Td className="font-medium">{t.name}</Td>
              <Td className="font-mono text-xs">{t.prefix}…</Td>
              <Td>{t.expires_at ? dateTime(t.expires_at) : "Never"}</Td>
              <Td>{dateTime(t.created_at)}</Td>
              <Td className="text-right">
                <Button size="sm" variant="ghost" disabled={remove.isPending} onClick={() => remove.mutate(t.id)}>
                  Revoke
                </Button>
              </Td>
            </tr>
          ))}
        </Table>
      )}
      <Modal
        title={created ? "Token created" : "New deploy token"}
        open={creating}
        onClose={() => {
          setCreating(false);
          setCreated(null);
        }}
      >
        {created ? (
          <div className="space-y-3 text-sm">
            <p>Copy it now; it won&apos;t be shown again.</p>
            <CopyText text={created} />
          </div>
        ) : (
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate();
            }}
          >
            <Field label="Name">
              <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} required placeholder="e.g. Office rollout" />
            </Field>
            <Field label="Expires">
              <Select value={days} onChange={(e) => setDays(e.target.value)} className="w-full">
                <option value="1">After 1 day</option>
                <option value="7">After 7 days</option>
                <option value="30">After 30 days</option>
                <option value="0">Never</option>
              </Select>
            </Field>
            <ErrorText error={create.error} />
            <div className="flex justify-end">
              <Button variant="primary" type="submit" disabled={create.isPending}>
                Create
              </Button>
            </div>
          </form>
        )}
      </Modal>
    </Card>
  );
}
