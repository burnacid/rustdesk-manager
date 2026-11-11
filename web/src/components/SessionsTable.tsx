import { Link } from "react-router";
import type { Session } from "../lib/api";
import { connTypes, label, primaryAuths, twoFactors } from "../lib/labels";
import { dateTime, duration } from "../lib/format";
import { Badge, Table, Td } from "./ui";

export function SessionsTable({ rows, showDevice = true }: { rows: Session[]; showDevice?: boolean }) {
  const head = [...(showDevice ? ["Device"] : []), "From", "Type", "Login", "Started", "Duration"];
  return (
    <Table head={head} empty={rows.length === 0}>
      {rows.map((s) => (
        <tr key={s.id}>
          {showDevice && (
            <Td>
              <Link to={`/audit?device=${encodeURIComponent(s.device_id)}`} className="font-medium hover:underline">
                {s.device_name || s.device_id}
              </Link>
              {s.device_name && <div className="text-xs text-slate-500">{s.device_id}</div>}
            </Td>
          )}
          <Td>
            <div>{s.peer_name || s.peer_id || <span className="text-slate-400">not logged in</span>}</div>
            <div className="text-xs text-slate-500">
              {s.peer_name && s.peer_id} {s.ip && `· ${s.ip}`}
            </div>
          </Td>
          <Td>{s.authed_at ? label(connTypes, s.type) : "—"}</Td>
          <Td>
            {s.authed_at ? (
              <>
                {label(primaryAuths, s.primary_auth) || "—"}
                {s.two_factor > 0 && <div className="text-xs text-slate-500">+ {label(twoFactors, s.two_factor)}</div>}
              </>
            ) : (
              <Badge tone="amber">No login</Badge>
            )}
          </Td>
          <Td className="whitespace-nowrap">{dateTime(s.started_at)}</Td>
          <Td className="whitespace-nowrap">{s.closed_at ? duration(s.started_at, s.closed_at) : <Badge tone="green">Active</Badge>}</Td>
        </tr>
      ))}
    </Table>
  );
}
