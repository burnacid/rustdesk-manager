import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router";
import { ArrowLeft, FolderUp, MonitorUp } from "lucide-react";
import { rustdeskLink } from "../lib/rustdesk";
import { api, type AdminUser, type Device, type DeviceGroup, type EffectiveStrategy, type Paged, type Session, type Strategy } from "../lib/api";
import { dateTime, timeAgo } from "../lib/format";
import { Badge, Button, Card, ErrorText, Field, Input, LinkButton, Loading, Modal, OnlineBadge, PageHeader, Select } from "../components/ui";
import { SessionsTable } from "../components/SessionsTable";

type Detail = {
  device: Device;
  sysinfo: Record<string, unknown> | null;
  uuid_bound: boolean;
  strategy: EffectiveStrategy;
  active_sessions: Session[];
  recent_sessions: Session[];
};

export function DeviceDetail() {
  const { id } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [editing, setEditing] = useState(false);
  const [confirm, setConfirm] = useState<"delete" | "reset" | null>(null);

  const q = useQuery({ queryKey: ["device", id], queryFn: () => api<Detail>(`/devices/${id}`), refetchInterval: 5000 });
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["device", id] });
    qc.invalidateQueries({ queryKey: ["devices"] });
  };
  const disconnect = useMutation({
    mutationFn: (connIDs: number[]) => api(`/devices/${id}/disconnect`, { method: "POST", body: { conn_ids: connIDs } }),
    onSuccess: () => setTimeout(refresh, 4000), // applied on the next heartbeat (about 3 s)
  });
  const remove = useMutation({
    mutationFn: () => api(`/devices/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["devices"] });
      navigate("/devices");
    },
  });
  const resetUUID = useMutation({
    mutationFn: () => api(`/devices/${id}/reset-uuid`, { method: "POST" }),
    onSuccess: () => {
      setConfirm(null);
      refresh();
    },
  });

  if (q.isPending) return <Loading />;
  if (q.error) return <ErrorText error={q.error} />;
  const { device: d, sysinfo, active_sessions, recent_sessions, strategy: eff } = q.data;
  const strategySource =
    eff.source === "device" ? "set on this device" : eff.source === "group" ? `from group ${eff.group?.name}` : "default";
  const strategyStatus =
    eff.status === "applied" ? (
      <Badge tone="green">Applied</Badge>
    ) : eff.status === "pending" ? (
      <Badge tone="amber">Waiting for device</Badge>
    ) : null;

  const info: [string, ReactNode][] = [
    ["RustDesk ID", <span className="font-mono">{d.rustdesk_id}</span>],
    ["Name", d.custom_hostname ? `${d.custom_hostname} (reported: ${d.reported_hostname || "—"})` : d.hostname || "—"],
    ["Logged-in user", d.username || "—"],
    ["Operating system", d.os || "—"],
    ["CPU", d.cpu || "—"],
    ["Memory", d.memory || "—"],
    ["RustDesk version", d.version || "—"],
    ["Owner", d.user?.name ?? "—"],
    ["Group", d.group?.name ?? "—"],
    [
      "Strategy",
      eff.strategy ? (
        <span className="flex flex-wrap items-center gap-1">
          <Link to="/strategies" className="hover:underline">
            {eff.strategy.name}
          </Link>
          <span className="text-slate-500">({strategySource})</span>
          {strategyStatus}
        </span>
      ) : (
        "None"
      ),
    ],
    ["Last IP", d.last_ip || "—"],
    ["Last seen", d.online ? "now" : `${timeAgo(d.last_seen_at)} (${dateTime(d.last_seen_at)})`],
    ["Registered", dateTime(d.created_at)],
    ["Note", d.note || "—"],
  ];

  return (
    <>
      <Link to="/devices" className="mb-3 inline-flex items-center gap-1 text-sm text-slate-500 hover:text-slate-900 dark:hover:text-white">
        <ArrowLeft size={14} /> Devices
      </Link>
      <PageHeader
        title={d.hostname || d.rustdesk_id}
        actions={
          <>
            <LinkButton variant="primary" href={rustdeskLink("connect", d.rustdesk_id)} title="Opens the RustDesk app on this computer">
              <MonitorUp size={16} /> Connect
            </LinkButton>
            <LinkButton href={rustdeskLink("file-transfer", d.rustdesk_id)} title="Opens the RustDesk app on this computer">
              <FolderUp size={16} /> File transfer
            </LinkButton>
            <Button onClick={() => setEditing(true)}>Edit</Button>
            <Button onClick={() => setConfirm("reset")}>Reset machine binding</Button>
            <Button variant="danger" onClick={() => setConfirm("delete")}>
              Delete
            </Button>
          </>
        }
      >
        <OnlineBadge online={d.online} />
      </PageHeader>

      <div className="grid gap-6 xl:grid-cols-3">
        <Card title="Details">
          <dl className="divide-y divide-slate-100 text-sm dark:divide-slate-800">
            {info.map(([k, v]) => (
              <div key={k} className="grid grid-cols-5 gap-2 px-4 py-2">
                <dt className="col-span-2 text-slate-500 dark:text-slate-400">{k}</dt>
                <dd className="col-span-3 break-words">{v}</dd>
              </div>
            ))}
          </dl>
          {sysinfo && (
            <details className="border-t border-slate-200 px-4 py-3 text-sm dark:border-slate-800">
              <summary className="cursor-pointer text-slate-500">Raw system info</summary>
              <pre className="mt-2 overflow-x-auto rounded bg-slate-100 p-2 text-xs dark:bg-slate-950">{JSON.stringify(sysinfo, null, 2)}</pre>
            </details>
          )}
        </Card>

        <div className="space-y-6 xl:col-span-2">
          <Card
            title={`Active sessions (${d.active_conns.length})`}
            actions={
              d.active_conns.length > 0 && (
                <Button size="sm" variant="danger" disabled={disconnect.isPending} onClick={() => disconnect.mutate([])}>
                  Disconnect all
                </Button>
              )
            }
          >
            {disconnect.isSuccess && <p className="px-4 pt-3 text-sm text-slate-500">Disconnect sent. It takes effect within a few seconds.</p>}
            <ErrorText error={disconnect.error} />
            {d.active_conns.length === 0 ? (
              <p className="px-4 py-6 text-sm text-slate-500">No one is connected to this device.</p>
            ) : (
              <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                {d.active_conns.map((c) => {
                  const s = active_sessions.find((x) => x.conn_id === c);
                  return (
                    <li key={c} className="flex items-center justify-between gap-3 px-4 py-3 text-sm">
                      <div>
                        <div className="font-medium">{s ? s.peer_name || s.peer_id || "Not logged in yet" : `Connection ${c}`}</div>
                        <div className="text-xs text-slate-500">{s ? `${s.peer_id} · ${s.ip} · since ${dateTime(s.started_at)}` : "No audit record"}</div>
                      </div>
                      <Button size="sm" disabled={disconnect.isPending} onClick={() => disconnect.mutate([c])}>
                        Disconnect
                      </Button>
                    </li>
                  );
                })}
              </ul>
            )}
          </Card>
          <Card
            title="Recent sessions"
            actions={
              <Link to={`/audit?device=${encodeURIComponent(d.rustdesk_id)}`} className="text-sm text-teal-700 hover:underline dark:text-teal-400">
                Full history
              </Link>
            }
          >
            <SessionsTable rows={recent_sessions} showDevice={false} />
          </Card>
        </div>
      </div>

      {editing && <EditDevice device={d} onClose={() => setEditing(false)} onSaved={refresh} />}

      <Modal title="Delete device?" open={confirm === "delete"} onClose={() => setConfirm(null)}>
        <p className="mb-4 text-sm">
          This removes <b>{d.hostname || d.rustdesk_id}</b> from the list. Its audit history is kept. If RustDesk is still running on it, it
          registers again on its next heartbeat.
        </p>
        <ErrorText error={remove.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={() => setConfirm(null)}>Cancel</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
            Delete
          </Button>
        </div>
      </Modal>
      <Modal title="Reset machine binding?" open={confirm === "reset"} onClose={() => setConfirm(null)}>
        <p className="mb-4 text-sm">
          Each RustDesk ID is tied to the first machine that reported it, so another machine can&apos;t take it over. Reset this after
          replacing or reinstalling the computer. The next machine that reports ID <b>{d.rustdesk_id}</b> then takes it over.
        </p>
        <ErrorText error={resetUUID.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={() => setConfirm(null)}>Cancel</Button>
          <Button variant="primary" disabled={resetUUID.isPending} onClick={() => resetUUID.mutate()}>
            Reset
          </Button>
        </div>
      </Modal>
    </>
  );
}

function EditDevice({ device, onClose, onSaved }: { device: Device; onClose: () => void; onSaved: () => void }) {
  const [note, setNote] = useState(device.note);
  const [customName, setCustomName] = useState(device.custom_hostname);
  const [groupID, setGroupID] = useState(String(device.group?.id ?? 0));
  const [userID, setUserID] = useState(String(device.user?.id ?? 0));
  const [strategyID, setStrategyID] = useState(String(device.strategy?.id ?? 0));
  const strategies = useQuery({ queryKey: ["strategies"], queryFn: () => api<Strategy[]>("/strategies") });
  const groups = useQuery({ queryKey: ["device-groups"], queryFn: () => api<DeviceGroup[]>("/device-groups") });
  const users = useQuery({ queryKey: ["users", "all"], queryFn: () => api<Paged<AdminUser>>("/users?pageSize=500") });
  const save = useMutation({
    mutationFn: () =>
      api(`/devices/${device.id}`, { method: "PATCH", body: { note, custom_hostname: customName, group_id: Number(groupID), user_id: Number(userID), strategy_id: Number(strategyID) } }),
    onSuccess: () => {
      onSaved();
      onClose();
    },
  });
  return (
    <Modal title="Edit device" open onClose={onClose}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="Display name (empty = use the reported hostname)">
          <Input value={customName} onChange={(e) => setCustomName(e.target.value)} placeholder={device.reported_hostname} />
        </Field>
        <Field label="Owner">
          <Select value={userID} onChange={(e) => setUserID(e.target.value)} className="w-full">
            <option value="0">No owner</option>
            {users.data?.data.map((u) => (
              <option key={u.id} value={u.id}>
                {u.display_name ? `${u.display_name} (${u.name})` : u.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Group">
          <Select value={groupID} onChange={(e) => setGroupID(e.target.value)} className="w-full">
            <option value="0">No group</option>
            {groups.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Strategy">
          <Select value={strategyID} onChange={(e) => setStrategyID(e.target.value)} className="w-full">
            <option value="0">Inherit (group or default)</option>
            {strategies.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Note">
          <Input value={note} onChange={(e) => setNote(e.target.value)} />
        </Field>
        <ErrorText error={save.error} />
        <div className="flex justify-end gap-2">
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
