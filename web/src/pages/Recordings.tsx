import { useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { api, qs, type Paged, type Recording, type ServerSettings } from "../lib/api";
import { bytes, dateTime } from "../lib/format";
import { Badge, Button, Card, ErrorText, Loading, Modal, PageHeader, Pagination, Table, Td } from "../components/ui";

const PAGE_SIZE = 50;

export function Recordings() {
  const qc = useQueryClient();
  const [page, setPage] = useState(1);
  const [playing, setPlaying] = useState<Recording | null>(null);
  const settings = useQuery({ queryKey: ["settings"], queryFn: () => api<ServerSettings>("/settings") });
  const list = useQuery({
    queryKey: ["recordings", page],
    queryFn: () => api<Paged<Recording>>(`/recordings${qs({ current: page, pageSize: PAGE_SIZE })}`),
    placeholderData: keepPreviousData,
  });
  const remove = useMutation({
    mutationFn: (id: number) => api(`/recordings/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["recordings"] }),
  });

  return (
    <>
      <PageHeader title="Recordings">Session recordings uploaded by devices.</PageHeader>
      {settings.data && !settings.data.accept_recordings && (
        <p className="mb-4 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
          Uploads are switched off in <Link to="/settings" className="underline">Settings</Link>. The standard RustDesk app never uploads recordings (it
          saves them on the device); only custom-built clients do.
        </p>
      )}
      <Card>
        {list.isPending ? (
          <Loading />
        ) : list.error ? (
          <div className="p-4">
            <ErrorText error={list.error} />
          </div>
        ) : (
          <>
            <Table head={["Device", "Direction", "Size", "Recorded", ""]} empty={list.data.data.length === 0}>
              {list.data.data.map((r) => (
                <tr key={r.id}>
                  <Td>
                    <div className="font-medium">{r.device_name || r.device_id}</div>
                    <div className="max-w-xs truncate font-mono text-xs text-slate-500" title={r.file}>
                      {r.file}
                    </div>
                  </Td>
                  <Td>{r.incoming ? "Someone connected to it" : "It connected to another device"}</Td>
                  <Td className="whitespace-nowrap">
                    {bytes(r.size)} {!r.completed && <Badge tone="amber">Incomplete</Badge>}
                  </Td>
                  <Td className="whitespace-nowrap">{dateTime(r.created_at)}</Td>
                  <Td className="text-right whitespace-nowrap">
                    {r.file.endsWith(".webm") && (
                      <Button size="sm" variant="ghost" onClick={() => setPlaying(r)}>
                        Play
                      </Button>
                    )}
                    <a href={`/admin/api/recordings/${r.id}/file?download=1`} className="px-2.5 py-1 text-xs font-medium text-slate-600 hover:underline dark:text-slate-300">
                      Download
                    </a>
                    <Button size="sm" variant="ghost" disabled={remove.isPending} onClick={() => remove.mutate(r.id)}>
                      Delete
                    </Button>
                  </Td>
                </tr>
              ))}
            </Table>
            <Pagination page={page} pageSize={PAGE_SIZE} total={list.data.total} onPage={setPage} />
          </>
        )}
      </Card>
      <Modal title={playing ? playing.device_name || playing.device_id : ""} open={!!playing} onClose={() => setPlaying(null)} wide>
        {playing && <video src={`/admin/api/recordings/${playing.id}/file`} controls autoPlay className="w-full rounded bg-black" />}
      </Modal>
    </>
  );
}
