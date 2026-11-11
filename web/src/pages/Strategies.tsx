import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { Plus, Trash2 } from "lucide-react";
import { api, type Strategy } from "../lib/api";
import { blockedKeys, describeOption, knownKeys, optionSections, type OptionDef } from "../lib/strategyOptions";
import { timeAgo } from "../lib/format";
import { Badge, Button, Card, ErrorText, Input, Loading, Modal, PageHeader, Select } from "../components/ui";

export function Strategies() {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<{ strategy: Strategy | null } | null>(null);
  const [deleting, setDeleting] = useState<Strategy | null>(null);
  const list = useQuery({ queryKey: ["strategies"], queryFn: () => api<Strategy[]>("/strategies") });
  const makeDefault = useMutation({
    mutationFn: (id: number) => api(`/strategies/${id}`, { method: "PATCH", body: { is_default: true } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["strategies"] }),
  });
  const remove = useMutation({
    mutationFn: (id: number) => api(`/strategies/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      setDeleting(null);
      qc.invalidateQueries({ queryKey: ["strategies"] });
    },
  });

  return (
    <>
      <PageHeader
        title="Strategies"
        actions={
          <Button variant="primary" onClick={() => setEditing({ strategy: null })}>
            <Plus size={16} /> New strategy
          </Button>
        }
      >
        Settings pushed to devices within seconds. A device uses its own strategy, otherwise its group&apos;s, otherwise the default. Settings that
        aren&apos;t managed keep the device&apos;s own choice.
      </PageHeader>
      {list.isPending ? (
        <Loading />
      ) : list.error ? (
        <ErrorText error={list.error} />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {list.data.map((s) => {
            const entries = Object.entries(s.options);
            return (
              <Card key={s.id} className="flex flex-col">
                <div className="flex flex-wrap items-start justify-between gap-2 border-b border-slate-200 px-4 py-3 dark:border-slate-800">
                  <div>
                    <div className="flex items-center gap-2 font-semibold">
                      {s.name} {s.is_default && <Badge tone="accent">Default</Badge>}
                    </div>
                    <div className="text-xs text-slate-500">
                      {s.is_default ? "Devices without another strategy" : `${s.device_count} device(s)`}
                      {s.groups.length > 0 && ` · groups: ${s.groups.map((g) => g.name).join(", ")}`} · changed {timeAgo(s.updated_at)}
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    {!s.is_default && (
                      <Button size="sm" variant="ghost" disabled={makeDefault.isPending} onClick={() => makeDefault.mutate(s.id)}>
                        Make default
                      </Button>
                    )}
                    <Button size="sm" variant="ghost" onClick={() => setEditing({ strategy: s })}>
                      Edit
                    </Button>
                    {!s.is_default && (
                      <Button size="sm" variant="ghost" onClick={() => setDeleting(s)}>
                        Delete
                      </Button>
                    )}
                  </div>
                </div>
                <ul className="flex-1 space-y-1 px-4 py-3 text-sm">
                  {entries.length === 0 && <li className="text-slate-500">Manages nothing: devices keep their own settings.</li>}
                  {entries.map(([k, v]) => (
                    <li key={k}>{describeOption(k, v)}</li>
                  ))}
                </ul>
              </Card>
            );
          })}
        </div>
      )}
      <p className="mt-4 text-sm text-slate-500">
        Assign strategies on a <Link to="/devices" className="text-teal-700 hover:underline">device</Link> or{" "}
        <Link to="/device-groups" className="text-teal-700 hover:underline">device group</Link>, or install devices with{" "}
        <code>preset-strategy-name</code>.
      </p>

      {editing && <StrategyEditor strategy={editing.strategy} onClose={() => setEditing(null)} />}
      <Modal title="Delete strategy?" open={!!deleting} onClose={() => setDeleting(null)}>
        <p className="mb-4 text-sm">
          Delete <b>{deleting?.name}</b>? Devices and groups using it fall back to their group&apos;s or the default strategy, and settings only
          it managed go back to the device defaults.
        </p>
        <ErrorText error={remove.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={() => setDeleting(null)}>Cancel</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => deleting && remove.mutate(deleting.id)}>
            Delete
          </Button>
        </div>
      </Modal>
    </>
  );
}

function OptionControl({ def, value, onChange }: { def: OptionDef; value: string | undefined; onChange: (v: string | undefined) => void }) {
  const managed = value !== undefined;
  let control: React.ReactNode = null;
  if (managed) {
    if (def.type === "bool") {
      control = (
        <Select value={value} onChange={(e) => onChange(e.target.value)} aria-label={def.label}>
          <option value="Y">On</option>
          <option value="N">Off</option>
        </Select>
      );
    } else if (def.type === "select") {
      control = (
        <Select value={value} onChange={(e) => onChange(e.target.value)} aria-label={def.label}>
          {def.choices!.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </Select>
      );
    } else {
      control = (
        <Input
          type={def.type === "number" ? "number" : "text"}
          min={def.type === "number" ? 0 : undefined}
          value={value}
          placeholder={def.placeholder}
          onChange={(e) => onChange(e.target.value)}
          aria-label={def.label}
          className="sm:w-56"
        />
      );
    }
  }
  const defaultValue = def.type === "bool" ? (def.key.startsWith("allow-") || def.key === "direct-server" ? "Y" : "N") : def.choices?.[0]?.value ?? "";
  return (
    <div className="flex flex-col gap-2 py-2 sm:flex-row sm:items-center sm:justify-between">
      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" className="mt-0.5" checked={managed} onChange={(e) => onChange(e.target.checked ? defaultValue : undefined)} />
        <span>
          {def.label}
          {def.help && <span className="block text-xs text-slate-500">{def.help}</span>}
        </span>
      </label>
      {control}
    </div>
  );
}

function StrategyEditor({ strategy, onClose }: { strategy: Strategy | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState(strategy?.name ?? "");
  const [opts, setOpts] = useState<Record<string, string>>(strategy?.options ?? {});
  const [custom, setCustom] = useState({ key: "", value: "" });
  const save = useMutation({
    mutationFn: () => {
      const body = { name, options: opts };
      return strategy ? api(`/strategies/${strategy.id}`, { method: "PATCH", body }) : api("/strategies", { method: "POST", body });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["strategies"] });
      qc.invalidateQueries({ queryKey: ["device"] });
      onClose();
    },
  });
  const set = (k: string, v: string | undefined) => {
    const next = { ...opts };
    if (v === undefined) delete next[k];
    else next[k] = v;
    setOpts(next);
  };
  const others = Object.entries(opts).filter(([k]) => !knownKeys.has(k));
  const customError = blockedKeys.has(custom.key.trim()) ? "The RustDesk client refuses this option for security reasons." : "";

  return (
    <Modal title={strategy ? `Edit ${strategy.name}` : "New strategy"} open onClose={onClose} wide>
      <form
        className="max-h-[70vh] space-y-4 overflow-y-auto pr-1"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Input aria-label="Name" placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} required maxLength={100} autoFocus={!strategy} />
        <p className="text-xs text-slate-500">Tick a setting to manage it. Unticked settings stay as each device has them.</p>
        {optionSections.map((section) => (
          <fieldset key={section.title}>
            <legend className="mb-1 text-sm font-semibold">{section.title}</legend>
            <div className="divide-y divide-slate-100 dark:divide-slate-800">
              {section.options.map((def) => (
                <OptionControl key={def.key} def={def} value={opts[def.key]} onChange={(v) => set(def.key, v)} />
              ))}
            </div>
          </fieldset>
        ))}
        <fieldset>
          <legend className="mb-1 text-sm font-semibold">Other options</legend>
          <p className="mb-2 text-xs text-slate-500">Any other RustDesk option, by its exact name.</p>
          {others.map(([k, v]) => (
            <div key={k} className="mb-2 flex gap-2">
              <Input value={k} disabled className="font-mono" />
              <Input value={v} onChange={(e) => set(k, e.target.value)} className="font-mono" />
              <Button type="button" variant="ghost" aria-label={`Remove ${k}`} onClick={() => set(k, undefined)}>
                <Trash2 size={16} />
              </Button>
            </div>
          ))}
          <div className="flex gap-2">
            <Input placeholder="option-name" value={custom.key} onChange={(e) => setCustom({ ...custom, key: e.target.value })} className="font-mono" />
            <Input placeholder="value" value={custom.value} onChange={(e) => setCustom({ ...custom, value: e.target.value })} className="font-mono" />
            <Button
              type="button"
              disabled={!custom.key.trim() || !!customError}
              onClick={() => {
                set(custom.key.trim(), custom.value);
                setCustom({ key: "", value: "" });
              }}
            >
              Add
            </Button>
          </div>
          {customError && <p className="mt-1 text-xs text-red-600">{customError}</p>}
        </fieldset>
        <ErrorText error={save.error} />
        <div className="sticky bottom-0 flex justify-end gap-2 bg-white pt-2 dark:bg-slate-900">
          <Button type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            Save
          </Button>
        </div>
      </form>
    </Modal>
  );
}
