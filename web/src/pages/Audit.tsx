import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router";
import { api, qs, type Alarm, type FileAudit, type Paged, type Session } from "../lib/api";
import { alarmTypes, fileDirections, label } from "../lib/labels";
import { bytes, dateTime } from "../lib/format";
import { Card, ErrorText, Input, Loading, PageHeader, Pagination, Table, Td } from "../components/ui";
import { SessionsTable } from "../components/SessionsTable";

const PAGE_SIZE = 50;
const tabs = [
  { key: "sessions", label: "Sessions" },
  { key: "files", label: "File transfers" },
  { key: "alarms", label: "Alarms" },
] as const;
type Tab = (typeof tabs)[number]["key"];

export function Audit() {
  const [params, setParams] = useSearchParams();
  const tab = (params.get("tab") as Tab) || "sessions";
  const page = Number(params.get("page") ?? 1);
  const filters = { device: params.get("device") ?? "", from: params.get("from") ?? "", to: params.get("to") ?? "" };

  const set = (k: string, v: string) => {
    const p = new URLSearchParams(params);
    if (v) p.set(k, v);
    else p.delete(k);
    if (k !== "page") p.delete("page");
    setParams(p, { replace: true });
  };

  return (
    <>
      <PageHeader title="Audit log">Sessions, file transfers and security alarms reported by your devices.</PageHeader>
      <div className="mb-4 flex gap-1 border-b border-slate-200 dark:border-slate-800">
        {tabs.map((t) => (
          <button
            key={t.key}
            onClick={() => set("tab", t.key === "sessions" ? "" : t.key)}
            className={`-mb-px border-b-2 px-3 py-2 text-sm ${
              tab === t.key ? "border-teal-700 font-medium text-teal-700 dark:text-teal-400" : "border-transparent text-slate-500 hover:text-slate-900 dark:hover:text-white"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>
      <Card>
        <div className="grid gap-2 border-b border-slate-200 p-3 sm:grid-cols-3 dark:border-slate-800">
          <Input placeholder="Device ID" value={filters.device} onChange={(e) => set("device", e.target.value.trim())} aria-label="Device ID" />
          <Input type="date" value={filters.from} onChange={(e) => set("from", e.target.value)} aria-label="From date" />
          <Input type="date" value={filters.to} onChange={(e) => set("to", e.target.value)} aria-label="To date" />
        </div>
        {tab === "sessions" && <Sessions filters={filters} page={page} onPage={(p) => set("page", String(p))} />}
        {tab === "files" && <Files filters={filters} page={page} onPage={(p) => set("page", String(p))} />}
        {tab === "alarms" && <Alarms filters={filters} page={page} onPage={(p) => set("page", String(p))} />}
      </Card>
    </>
  );
}

type ListProps = { filters: Record<string, string>; page: number; onPage: (p: number) => void };

function useAuditList<T>(kind: string, { filters, page }: ListProps) {
  return useQuery({
    queryKey: ["audit", kind, filters, page],
    queryFn: () => api<Paged<T>>(`/audit/${kind}${qs({ ...filters, current: page, pageSize: PAGE_SIZE })}`),
    placeholderData: keepPreviousData,
  });
}

function ListState({ q, children }: { q: { isPending: boolean; error: unknown }; children: React.ReactNode }) {
  if (q.isPending) return <Loading />;
  if (q.error)
    return (
      <div className="p-4">
        <ErrorText error={q.error} />
      </div>
    );
  return <>{children}</>;
}

function Sessions(props: ListProps) {
  const q = useAuditList<Session>("conns", props);
  return (
    <ListState q={q}>
      {q.data && (
        <>
          <SessionsTable rows={q.data.data} />
          <Pagination page={props.page} pageSize={PAGE_SIZE} total={q.data.total} onPage={props.onPage} />
        </>
      )}
    </ListState>
  );
}

function Files(props: ListProps) {
  const q = useAuditList<FileAudit>("files", props);
  return (
    <ListState q={q}>
      {q.data && (
        <>
          <Table head={["Device", "Peer", "Direction", "Path", "Files", "Time"]} empty={q.data.data.length === 0}>
            {q.data.data.map((f) => {
              const info = typeof f.info === "object" ? f.info : {};
              return (
                <tr key={f.id}>
                  <Td>
                    <div className="font-medium">{f.device_name || f.device_id}</div>
                    {f.device_name && <div className="text-xs text-slate-500">{f.device_id}</div>}
                  </Td>
                  <Td>
                    <div>{info.name || f.peer_id}</div>
                    <div className="text-xs text-slate-500">
                      {info.name && f.peer_id} {info.ip && `· ${info.ip}`}
                    </div>
                  </Td>
                  <Td className="whitespace-nowrap">{label(fileDirections, f.type)}</Td>
                  <Td className="max-w-xs font-mono text-xs break-all">{f.path}</Td>
                  <Td>
                    {info.num ?? 0} item(s)
                    {info.files && info.files.length > 0 && (
                      <details className="text-xs text-slate-500">
                        <summary className="cursor-pointer">Largest</summary>
                        <ul>
                          {info.files.map(([name, size], i) => (
                            <li key={i}>
                              {name || "(single file)"} · {bytes(size)}
                            </li>
                          ))}
                        </ul>
                      </details>
                    )}
                  </Td>
                  <Td className="whitespace-nowrap">{dateTime(f.created_at)}</Td>
                </tr>
              );
            })}
          </Table>
          <Pagination page={props.page} pageSize={PAGE_SIZE} total={q.data.total} onPage={props.onPage} />
        </>
      )}
    </ListState>
  );
}

function Alarms(props: ListProps) {
  const q = useAuditList<Alarm>("alarms", props);
  return (
    <ListState q={q}>
      {q.data && (
        <>
          <Table head={["Device", "Alarm", "Details", "Time"]} empty={q.data.data.length === 0}>
            {q.data.data.map((a) => (
              <tr key={a.id}>
                <Td>
                  <div className="font-medium">{a.device_name || a.device_id}</div>
                  {a.device_name && <div className="text-xs text-slate-500">{a.device_id}</div>}
                </Td>
                <Td>{label(alarmTypes, a.type)}</Td>
                <Td className="max-w-md font-mono text-xs break-all">{typeof a.info === "string" ? a.info : JSON.stringify(a.info)}</Td>
                <Td className="whitespace-nowrap">{dateTime(a.created_at)}</Td>
              </tr>
            ))}
          </Table>
          <Pagination page={props.page} pageSize={PAGE_SIZE} total={q.data.total} onPage={props.onPage} />
        </>
      )}
    </ListState>
  );
}
