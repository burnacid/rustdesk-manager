import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, type Alarm, type Session } from "../lib/api";
import { alarmTypes, label } from "../lib/labels";
import { dateTime } from "../lib/format";
import { Card, ErrorText, Loading, PageHeader, Stat, Table, Td } from "../components/ui";
import { SessionsTable } from "../components/SessionsTable";

type DashboardData = {
  devices_total: number;
  devices_online: number;
  active_sessions: number;
  sessions_24h: number;
  alarms_24h: number;
  recent_sessions: Session[];
  recent_alarms: Alarm[];
};

export function Dashboard() {
  const q = useQuery({ queryKey: ["dashboard"], queryFn: () => api<DashboardData>("/dashboard"), refetchInterval: 15000 });
  if (q.isPending) return <Loading />;
  if (q.error) return <ErrorText error={q.error} />;
  const d = q.data;
  return (
    <>
      <PageHeader title="Dashboard" />
      <div className="mb-6 grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat label="Devices online" value={`${d.devices_online} / ${d.devices_total}`} />
        <Stat label="Active sessions" value={d.active_sessions} />
        <Stat label="Sessions (24 h)" value={d.sessions_24h} />
        <Stat label="Alarms (24 h)" value={d.alarms_24h} tone={d.alarms_24h > 0 ? "red" : undefined} />
      </div>
      <div className="grid gap-6 xl:grid-cols-2">
        <Card title="Recent sessions" actions={<Link to="/audit" className="text-sm text-teal-700 hover:underline dark:text-teal-400">All sessions</Link>}>
          <SessionsTable rows={d.recent_sessions} />
        </Card>
        <Card title="Recent alarms" actions={<Link to="/audit?tab=alarms" className="text-sm text-teal-700 hover:underline dark:text-teal-400">All alarms</Link>}>
          <Table head={["Device", "Alarm", "Time"]} empty={d.recent_alarms.length === 0}>
            {d.recent_alarms.map((a) => (
              <tr key={a.id}>
                <Td>{a.device_name || a.device_id}</Td>
                <Td>{label(alarmTypes, a.type)}</Td>
                <Td className="whitespace-nowrap">{dateTime(a.created_at)}</Td>
              </tr>
            ))}
          </Table>
        </Card>
      </div>
    </>
  );
}
