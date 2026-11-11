import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { api, qs, type Device, type DeviceGroup, type Paged } from "../lib/api";
import { osShort, timeAgo } from "../lib/format";
import { Badge, Card, ErrorText, Input, Loading, OnlineBadge, PageHeader, Pagination, Select, Table, Td } from "../components/ui";

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
        {list.isPending ? (
          <Loading />
        ) : list.error ? (
          <div className="p-4">
            <ErrorText error={list.error} />
          </div>
        ) : (
          <>
            <Table head={["Device", "Status", "OS", "Owner", "Group", "Version", "Last seen"]} empty={list.data.data.length === 0}>
              {list.data.data.map((d) => (
                <tr key={d.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/50">
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
