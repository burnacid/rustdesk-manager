import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { Plus } from "lucide-react";
import { api, type DeviceGroup, type Strategy } from "../lib/api";
import { Button, Card, ErrorText, Field, Input, Loading, Modal, PageHeader, Select, Table, Td } from "../components/ui";

type Editing = { group: DeviceGroup | null } | null; // group null = new

export function DeviceGroups() {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<Editing>(null);
  const [deleting, setDeleting] = useState<DeviceGroup | null>(null);
  const list = useQuery({ queryKey: ["device-groups"], queryFn: () => api<DeviceGroup[]>("/device-groups") });
  const remove = useMutation({
    mutationFn: (id: number) => api(`/device-groups/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      setDeleting(null);
      qc.invalidateQueries({ queryKey: ["device-groups"] });
      qc.invalidateQueries({ queryKey: ["devices"] });
    },
  });

  return (
    <>
      <PageHeader
        title="Device groups"
        actions={
          <Button variant="primary" onClick={() => setEditing({ group: null })}>
            <Plus size={16} /> New group
          </Button>
        }
      >
        Group devices to filter them and, later, to share them with users and apply strategies. Install with{" "}
        <code className="rounded bg-slate-100 px-1 dark:bg-slate-800">preset-device-group-name</code> to place new devices automatically.
      </PageHeader>
      <Card>
        {list.isPending ? (
          <Loading />
        ) : list.error ? (
          <div className="p-4">
            <ErrorText error={list.error} />
          </div>
        ) : (
          <Table head={["Name", "Devices", "Strategy", "Note", ""]} empty={list.data.length === 0}>
            {list.data.map((g) => (
              <tr key={g.id}>
                <Td className="font-medium">{g.name}</Td>
                <Td>
                  <Link to={`/devices?group=${g.id}`} className="text-teal-700 hover:underline dark:text-teal-400">
                    {g.device_count}
                  </Link>
                </Td>
                <Td>{g.strategy?.name ?? <span className="text-slate-400">Default</span>}</Td>
                <Td>{g.note || "—"}</Td>
                <Td className="text-right whitespace-nowrap">
                  <Button size="sm" variant="ghost" onClick={() => setEditing({ group: g })}>
                    Edit
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setDeleting(g)}>
                    Delete
                  </Button>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      {editing && <GroupForm group={editing.group} onClose={() => setEditing(null)} />}
      <Modal title="Delete group?" open={!!deleting} onClose={() => setDeleting(null)}>
        <p className="mb-4 text-sm">
          Delete <b>{deleting?.name}</b>? Its {deleting?.device_count} device(s) stay registered but no longer have a group.
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

function GroupForm({ group, onClose }: { group: DeviceGroup | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState(group?.name ?? "");
  const [note, setNote] = useState(group?.note ?? "");
  const [strategyID, setStrategyID] = useState(String(group?.strategy?.id ?? 0));
  const strategies = useQuery({ queryKey: ["strategies"], queryFn: () => api<Strategy[]>("/strategies") });
  const save = useMutation({
    mutationFn: () => {
      const body = { name, note, strategy_id: Number(strategyID) };
      return group ? api(`/device-groups/${group.id}`, { method: "PATCH", body }) : api("/device-groups", { method: "POST", body });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["device-groups"] });
      qc.invalidateQueries({ queryKey: ["devices"] });
      qc.invalidateQueries({ queryKey: ["strategies"] });
      onClose();
    },
  });
  return (
    <Modal title={group ? "Edit group" : "New group"} open onClose={onClose}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="Name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} required maxLength={100} />
        </Field>
        <Field label="Strategy for devices in this group">
          <Select value={strategyID} onChange={(e) => setStrategyID(e.target.value)} className="w-full">
            <option value="0">Default strategy</option>
            {strategies.data
              ?.filter((s) => !s.is_default)
              .map((s) => (
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
