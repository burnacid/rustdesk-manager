import { useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { api, qs, type AdminUser, type Device, type DeviceGroup, type Paged, type Strategy } from "../lib/api";
import { osShort, timeAgo } from "../lib/format";
import { Badge, Button, Card, ErrorText, Input, Loading, OnlineBadge, PageHeader, Pagination, Select, Table, Td } from "../components/ui";

const PAGE_SIZE = 50;

export function Devices() {
  const [params, setParams] = useSearchParams();
  const [search, setSearch] = useState(params.get("q") ?? "");
  const status = params.get("status") ?? "";
  const group = params.get("group") ?? "";
  const page = Number(params.get("page") ?? 1);

  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params);
    if (v) p.set(k, v);
    else p.delete(k);
    if (k !== "page") p.delete("page");
    setParams(p, { replace: true });
  };

  const groups = useQuery({ queryKey: ["device-groups"], queryFn: () => api<DeviceGroup[]>("/device-groups") });
  const list = useQuery({
    queryKey: ["devices", params.toString()],
    queryFn: () =>
      api<Paged<Device>>(`/devices${qs({ q: params.get("q") ?? "", status, group, current: page, pageSize: PAGE_SIZE })}`),
    placeholderData: keepPreviousData,
    refetchInterval: 15000,
  });
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const pageIDs = list.data?.data.map((d) => d.id) ?? [];
  const allChecked = pageIDs.length > 0 && pageIDs.every((id) => selected.has(id));
  const toggle = (id: number) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const toggleAll = () =>
    setSelected((prev) => {
      const next = new Set(prev);
      for (const id of pageIDs) {
        if (allChecked) next.delete(id);
        else next.add(id);
      }
      return next;
    });

  return (
    <>
      <PageHeader title="Devices">Devices appear here after they report to this server.</PageHeader>
      <Card>
        <div className="flex flex-wrap gap-2 border-b border-slate-200 p-3 dark:border-slate-800">
          <form
            className="min-w-48 flex-1"
            onSubmit={(e) => {
              e.preventDefault();
              set("q", search.trim());
            }}
          >
            <Input placeholder="Search ID, hostname, user, note…" value={search} onChange={(e) => setSearch(e.target.value)} />
          </form>
          <Select value={status} onChange={(e) => set("status", e.target.value)} aria-label="Status">
            <option value="">All statuses</option>
            <option value="online">Online</option>
            <option value="offline">Offline</option>
          </Select>
          <Select value={group} onChange={(e) => set("group", e.target.value)} aria-label="Group">
            <option value="">All groups</option>
            <option value="none">No group</option>
            {groups.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </Select>
        </div>
        {selected.size > 0 && <BulkBar ids={[...selected]} groups={groups.data ?? []} onDone={() => setSelected(new Set())} />}
        {list.isPending ? (
          <Loading />
        ) : list.error ? (
          <div className="p-4">
            <ErrorText error={list.error} />
          </div>
        ) : (
          <>
            <Table
              head={[
                <input key="all" type="checkbox" className="size-4 align-middle" checked={allChecked} onChange={toggleAll} aria-label="Select all on this page" />,
                "Device",
                "Status",
                "OS",
                "Owner",
                "Group",
                "Version",
                "Last seen",
              ]}
              empty={list.data.data.length === 0}
            >
              {list.data.data.map((d) => (
                <tr key={d.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/50">
                  <Td className="w-8">
                    <input
                      type="checkbox"
                      className="size-4 align-middle"
                      checked={selected.has(d.id)}
                      onChange={() => toggle(d.id)}
                      aria-label={`Select ${d.hostname || d.rustdesk_id}`}
                    />
                  </Td>
                  <Td>
                    <Link to={`/devices/${d.id}`} className="font-medium text-teal-700 hover:underline dark:text-teal-400">
                      {d.hostname || d.rustdesk_id}
                    </Link>
                    <div className="text-xs text-slate-500">{d.rustdesk_id}</div>
                  </Td>
                  <Td>
                    <div className="flex flex-wrap gap-1">
                      <OnlineBadge online={d.online} />
                      {d.active_conns.length > 0 && (
                        <Badge tone="accent">
                          {d.active_conns.length} session{d.active_conns.length > 1 && "s"}
                        </Badge>
                      )}
                    </div>
                  </Td>
                  <Td>{osShort(d.os)}</Td>
                  <Td>
                    {d.user?.name ?? "—"}
                    {d.username && <div className="text-xs text-slate-500">signed in: {d.username}</div>}
                  </Td>
                  <Td>{d.group?.name ?? "—"}</Td>
                  <Td>{d.version || "—"}</Td>
                  <Td className="whitespace-nowrap">{d.online ? "now" : timeAgo(d.last_seen_at)}</Td>
                </tr>
              ))}
            </Table>
            <Pagination page={page} pageSize={PAGE_SIZE} total={list.data.total} onPage={(p) => set("page", String(p))} />
          </>
        )}
      </Card>
    </>
  );
}

type BulkAction = "set_group" | "set_user" | "set_strategy" | "disconnect" | "delete";

function BulkBar({ ids, groups, onDone }: { ids: number[]; groups: DeviceGroup[]; onDone: () => void }) {
  const qc = useQueryClient();
  const [action, setAction] = useState<BulkAction>("set_group");
  const [value, setValue] = useState("0");
  const [result, setResult] = useState<string | null>(null);
  const users = useQuery({
    queryKey: ["users", "all"],
    queryFn: () => api<Paged<AdminUser>>("/users?pageSize=500"),
    enabled: action === "set_user",
  });
  const strategies = useQuery({ queryKey: ["strategies"], queryFn: () => api<Strategy[]>("/strategies"), enabled: action === "set_strategy" });
  const options: { id: number; name: string }[] =
    action === "set_group" ? groups : action === "set_user" ? (users.data?.data ?? []) : action === "set_strategy" ? (strategies.data ?? []) : [];
  const needsValue = action.startsWith("set_");

  const run = useMutation({
    mutationFn: () => api<{ affected: number }>("/devices/bulk", { method: "POST", body: { ids, action, value: Number(value) } }),
    onSuccess: (res) => {
      setResult(
        action === "disconnect"
          ? `Disconnect sent to ${res.affected} device(s) with open sessions.`
          : action === "delete"
            ? `Deleted ${res.affected} device(s).`
            : `Updated ${res.affected} device(s).`,
      );
      qc.invalidateQueries({ queryKey: ["devices"] });
      if (action === "delete") onDone();
    },
  });

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-slate-200 bg-teal-50 p-3 text-sm dark:border-slate-800 dark:bg-teal-950/40">
      <span className="font-medium">{ids.length} selected</span>
      <Select
        value={action}
        onChange={(e) => {
          setAction(e.target.value as BulkAction);
          setValue("0");
          setResult(null);
        }}
        aria-label="Action"
      >
        <option value="set_group">Set group</option>
        <option value="set_user">Set owner</option>
        <option value="set_strategy">Set strategy</option>
        <option value="disconnect">Disconnect sessions</option>
        <option value="delete">Delete</option>
      </Select>
      {needsValue && (
        <Select value={value} onChange={(e) => setValue(e.target.value)} aria-label="Value">
          <option value="0">{action === "set_strategy" ? "Inherit from group" : "None"}</option>
          {options.map((o) => (
            <option key={o.id} value={o.id}>
              {o.name}
            </option>
          ))}
        </Select>
      )}
      <Button
        variant={action === "delete" ? "danger" : "primary"}
        size="sm"
        disabled={run.isPending}
        onClick={() => {
          if (action === "delete" && !confirm(`Delete ${ids.length} device(s)? Their audit history is kept.`)) return;
          run.mutate();
        }}
      >
        Apply
      </Button>
      <Button size="sm" variant="ghost" onClick={onDone}>
        Clear selection
      </Button>
      {result && <span className="text-green-700 dark:text-green-400">{result}</span>}
      <ErrorText error={run.error} />
    </div>
  );
}
