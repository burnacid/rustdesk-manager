import { useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router";
import { ArrowLeft, Check, Plus, X } from "lucide-react";
import { api, qs, type AbPeer, type AbTag, type AddressBook } from "../lib/api";
import { cssToTagColor, tagColorCss } from "../lib/labels";
import { Button, Card, ErrorText, Field, Input, Loading, Modal, PageHeader, Pagination, Table, Td } from "../components/ui";

const PAGE_SIZE = 100;

type PeersResp = { total: number; data: AbPeer[]; tags: AbTag[] };

function TagChip({ tag, colors }: { tag: string; colors: AbTag[] }) {
  const c = colors.find((t) => t.name === tag)?.color;
  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-slate-200 px-2 py-0.5 text-xs dark:border-slate-700">
      <span className="h-2 w-2 rounded-full" style={{ background: c ? tagColorCss(c) : "#94a3b8" }} />
      {tag}
    </span>
  );
}

export function AddressBookDetail() {
  const { id } = useParams();
  const qc = useQueryClient();
  const [page, setPage] = useState(1);
  const [editing, setEditing] = useState<{ peer: AbPeer | null } | null>(null);
  const [newTag, setNewTag] = useState({ name: "", color: "#0f766e" });

  const book = useQuery({ queryKey: ["address-book", id], queryFn: () => api<AddressBook>(`/address-books/${id}`) });
  const peers = useQuery({
    queryKey: ["address-book-peers", id, page],
    queryFn: () => api<PeersResp>(`/address-books/${id}/peers${qs({ current: page, pageSize: PAGE_SIZE })}`),
    placeholderData: keepPreviousData,
  });
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["address-book-peers", id] });
    qc.invalidateQueries({ queryKey: ["address-books"] });
  };
  const removePeer = useMutation({
    mutationFn: (peer: string) => api(`/address-books/${id}/peers/${encodeURIComponent(peer)}`, { method: "DELETE" }),
    onSuccess: refresh,
  });
  const addTag = useMutation({
    mutationFn: () => api(`/address-books/${id}/tags`, { method: "POST", body: { name: newTag.name.trim(), color: cssToTagColor(newTag.color) } }),
    onSuccess: () => {
      setNewTag({ ...newTag, name: "" });
      refresh();
    },
  });
  const removeTag = useMutation({
    mutationFn: (name: string) => api(`/address-books/${id}/tags/${encodeURIComponent(name)}`, { method: "DELETE" }),
    onSuccess: refresh,
  });

  if (book.isPending) return <Loading />;
  if (book.error) return <ErrorText error={book.error} />;
  const b = book.data;
  const tags = peers.data?.tags ?? [];

  return (
    <>
      <Link to={b.personal ? "/address-books?kind=personal" : "/address-books"} className="mb-3 inline-flex items-center gap-1 text-sm text-slate-500 hover:text-slate-900 dark:hover:text-white">
        <ArrowLeft size={14} /> Address books
      </Link>
      <PageHeader
        title={b.personal ? `${b.owner.name}'s address book` : b.name}
        actions={
          <Button variant="primary" onClick={() => setEditing({ peer: null })}>
            <Plus size={16} /> Add device
          </Button>
        }
      >
        {b.personal ? "Personal book: only its owner sees it in RustDesk." : `Owner: ${b.owner.name}${b.note ? ` · ${b.note}` : ""}`}
      </PageHeader>

      <div className="grid gap-6 xl:grid-cols-4">
        <Card className="xl:col-span-3">
          {peers.isPending ? (
            <Loading />
          ) : peers.error ? (
            <div className="p-4">
              <ErrorText error={peers.error} />
            </div>
          ) : (
            <>
              <Table head={["Device", "Tags", "Password", "Note", ""]} empty={peers.data.data.length === 0}>
                {peers.data.data.map((p) => (
                  <tr key={p.id}>
                    <Td>
                      <div className="font-medium">{p.alias || p.hostname || p.id}</div>
                      <div className="text-xs text-slate-500">
                        <span className="font-mono">{p.id}</span>
                        {p.hostname && p.alias && ` · ${p.hostname}`}
                        {p.platform && ` · ${p.platform}`}
                        {!p.same_server && " · not on this server"}
                      </div>
                    </Td>
                    <Td>
                      <div className="flex flex-wrap gap-1">
                        {p.tags.map((t) => (
                          <TagChip key={t} tag={t} colors={tags} />
                        ))}
                      </div>
                    </Td>
                    <Td>{p.has_password ? <Check size={16} className="text-green-600" aria-label="Saved" /> : <span className="text-slate-400">—</span>}</Td>
                    <Td className="max-w-xs break-words">{p.note || "—"}</Td>
                    <Td className="text-right whitespace-nowrap">
                      <Button size="sm" variant="ghost" onClick={() => setEditing({ peer: p })}>
                        Edit
                      </Button>
                      <Button size="sm" variant="ghost" disabled={removePeer.isPending} onClick={() => removePeer.mutate(p.id)}>
                        Remove
                      </Button>
                    </Td>
                  </tr>
                ))}
              </Table>
              <Pagination page={page} pageSize={PAGE_SIZE} total={peers.data.total} onPage={setPage} />
            </>
          )}
          <ErrorText error={removePeer.error} />
        </Card>

        <Card title="Tags">
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {tags.length === 0 && <li className="px-4 py-3 text-sm text-slate-500">No tags yet.</li>}
            {tags.map((t) => (
              <li key={t.name} className="flex items-center justify-between px-4 py-2 text-sm">
                <TagChip tag={t.name} colors={tags} />
                <button className="text-slate-400 hover:text-red-600" aria-label={`Delete tag ${t.name}`} onClick={() => removeTag.mutate(t.name)}>
                  <X size={14} />
                </button>
              </li>
            ))}
          </ul>
          <form
            className="flex gap-2 border-t border-slate-200 p-3 dark:border-slate-800"
            onSubmit={(e) => {
              e.preventDefault();
              if (newTag.name.trim()) addTag.mutate();
            }}
          >
            <input
              type="color"
              value={newTag.color}
              onChange={(e) => setNewTag({ ...newTag, color: e.target.value })}
              className="h-9 w-9 shrink-0 cursor-pointer rounded border border-slate-300 dark:border-slate-700"
              aria-label="Tag color"
            />
            <Input placeholder="New tag" value={newTag.name} onChange={(e) => setNewTag({ ...newTag, name: e.target.value })} />
            <Button type="submit" disabled={addTag.isPending}>
              Add
            </Button>
          </form>
          <div className="px-3 pb-3">
            <ErrorText error={addTag.error ?? removeTag.error} />
          </div>
        </Card>
      </div>

      {editing && <PeerForm bookID={id!} personal={b.personal} peer={editing.peer} tags={tags} onClose={() => setEditing(null)} onSaved={refresh} />}
    </>
  );
}

function PeerForm({
  bookID,
  personal,
  peer,
  tags,
  onClose,
  onSaved,
}: {
  bookID: string;
  personal: boolean;
  peer: AbPeer | null;
  tags: AbTag[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [f, setF] = useState({ id: peer?.id ?? "", alias: peer?.alias ?? "", note: peer?.note ?? "", password: "", tags: peer?.tags ?? [] });
  const [clearPassword, setClearPassword] = useState(false);
  const save = useMutation({
    mutationFn: () => {
      const body: Record<string, unknown> = { alias: f.alias, note: f.note, tags: f.tags };
      if (!personal && (f.password || clearPassword)) body.password = clearPassword ? "" : f.password;
      if (peer) return api(`/address-books/${bookID}/peers/${encodeURIComponent(peer.id)}`, { method: "PATCH", body });
      return api(`/address-books/${bookID}/peers`, { method: "POST", body: { ...body, id: f.id.trim() } });
    },
    onSuccess: () => {
      onSaved();
      onClose();
    },
  });
  const toggleTag = (t: string) => setF({ ...f, tags: f.tags.includes(t) ? f.tags.filter((x) => x !== t) : [...f.tags, t] });

  return (
    <Modal title={peer ? `Edit ${peer.alias || peer.id}` : "Add device"} open onClose={onClose}>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="RustDesk ID">
          <Input value={f.id} onChange={(e) => setF({ ...f, id: e.target.value })} required disabled={!!peer} autoFocus={!peer} />
        </Field>
        <Field label="Alias (name shown in RustDesk)">
          <Input value={f.alias} onChange={(e) => setF({ ...f, alias: e.target.value })} />
        </Field>
        {!personal && (
          <Field label={peer?.has_password ? "Password (saved; type to replace)" : "Password (optional; members connect without typing it)"}>
            <Input type="password" value={f.password} disabled={clearPassword} onChange={(e) => setF({ ...f, password: e.target.value })} autoComplete="new-password" />
            {peer?.has_password && (
              <label className="mt-1 flex items-center gap-2 text-xs text-slate-500">
                <input type="checkbox" checked={clearPassword} onChange={(e) => setClearPassword(e.target.checked)} /> Remove saved password
              </label>
            )}
          </Field>
        )}
        {tags.length > 0 && (
          <fieldset>
            <legend className="mb-1 text-sm font-medium">Tags</legend>
            <div className="flex flex-wrap gap-2">
              {tags.map((t) => (
                <label key={t.name} className="flex items-center gap-1.5 text-sm">
                  <input type="checkbox" checked={f.tags.includes(t.name)} onChange={() => toggleTag(t.name)} />
                  <span className="h-2 w-2 rounded-full" style={{ background: tagColorCss(t.color) }} />
                  {t.name}
                </label>
              ))}
            </div>
          </fieldset>
        )}
        <Field label="Note">
          <Input value={f.note} onChange={(e) => setF({ ...f, note: e.target.value })} />
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
