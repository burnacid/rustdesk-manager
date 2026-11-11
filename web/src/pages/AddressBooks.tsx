import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { Plus, Trash2 } from "lucide-react";
import { api, type AbShare, type AddressBook, type AdminUser, type Paged, type UserGroup } from "../lib/api";
import { shareRules } from "../lib/labels";
import { timeAgo } from "../lib/format";
import { Badge, Button, Card, ErrorText, Field, Input, Loading, Modal, PageHeader, Select, Table, Td } from "../components/ui";

export function AddressBooks() {
  const [params, setParams] = useSearchParams();
  const kind = params.get("kind") === "personal" ? "personal" : "shared";
  const [editing, setEditing] = useState<{ book: AddressBook | null } | null>(null);
  const [sharing, setSharing] = useState<AddressBook | null>(null);
  const [deleting, setDeleting] = useState<AddressBook | null>(null);
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["address-books", kind], queryFn: () => api<AddressBook[]>(`/address-books?kind=${kind}`) });
  const remove = useMutation({
    mutationFn: (id: number) => api(`/address-books/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      setDeleting(null);
      qc.invalidateQueries({ queryKey: ["address-books"] });
    },
  });

  return (
    <>
      <PageHeader
        title="Address books"
        actions={
          kind === "shared" && (
            <Button variant="primary" onClick={() => setEditing({ book: null })}>
              <Plus size={16} /> New shared book
            </Button>
          )
        }
      >
        Shared books appear in the RustDesk app of everyone they are shared with. Personal books belong to one user.
      </PageHeader>
      <div className="mb-4 flex gap-1 border-b border-slate-200 dark:border-slate-800">
        {(["shared", "personal"] as const).map((k) => (
          <button
            key={k}
            onClick={() => setParams(k === "shared" ? {} : { kind: k }, { replace: true })}
            className={`-mb-px border-b-2 px-3 py-2 text-sm ${
              kind === k ? "border-teal-700 font-medium text-teal-700 dark:text-teal-400" : "border-transparent text-slate-500 hover:text-slate-900 dark:hover:text-white"
            }`}
          >
            {k === "shared" ? "Shared" : "Personal"}
          </button>
        ))}
      </div>
      <Card>
        {list.isPending ? (
          <Loading />
        ) : list.error ? (
          <div className="p-4">
            <ErrorText error={list.error} />
          </div>
        ) : (
          <Table head={kind === "shared" ? ["Name", "Owner", "Entries", "Shared with", "Changed", ""] : ["Owner", "Entries", "Changed", ""]} empty={list.data.length === 0}>
            {list.data.map((b) => (
              <tr key={b.id}>
                {kind === "shared" && (
                  <Td>
                    <Link to={`/address-books/${b.id}`} className="font-medium text-teal-700 hover:underline dark:text-teal-400">
                      {b.name}
                    </Link>
                    {b.note && <div className="text-xs text-slate-500">{b.note}</div>}
                  </Td>
                )}
                <Td>
                  {kind === "personal" ? (
                    <Link to={`/address-books/${b.id}`} className="font-medium text-teal-700 hover:underline dark:text-teal-400">
                      {b.owner.name}
                    </Link>
                  ) : (
                    b.owner.name
                  )}
                </Td>
                <Td>{b.peer_count}</Td>
                {kind === "shared" && (
                  <Td>
                    <div className="flex flex-wrap gap-1">
                      {b.shares.length === 0 && <span className="text-slate-400">Only the owner</span>}
                      {b.shares.map((s) => (
                        <Badge key={s.id} tone={s.rule >= 2 ? "accent" : "slate"}>
                          {s.user ? s.user.name : `${s.user_group?.name} (group)`} · {shareRules[s.rule]}
                        </Badge>
                      ))}
                    </div>
                  </Td>
                )}
                <Td className="whitespace-nowrap">{timeAgo(b.updated_at)}</Td>
                <Td className="text-right whitespace-nowrap">
                  {kind === "shared" && (
                    <>
                      <Button size="sm" variant="ghost" onClick={() => setSharing(b)}>
                        Sharing
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setEditing({ book: b })}>
                        Edit
                      </Button>
                    </>
                  )}
                  <Button size="sm" variant="ghost" onClick={() => setDeleting(b)}>
                    Delete
                  </Button>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      {editing && <BookForm book={editing.book} onClose={() => setEditing(null)} />}
      {sharing && <ShareForm book={sharing} onClose={() => setSharing(null)} />}
      <Modal title="Delete address book?" open={!!deleting} onClose={() => setDeleting(null)}>
        <p className="mb-4 text-sm">
          Delete {deleting?.personal ? `the personal address book of ${deleting?.owner.name}` : <b>{deleting?.name}</b>} with its{" "}
          {deleting?.peer_count} entries? This can&apos;t be undone.
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

function useAllUsers() {
  return useQuery({ queryKey: ["users", "all"], queryFn: () => api<Paged<AdminUser>>("/users?pageSize=500") });
}

function BookForm({ book, onClose }: { book: AddressBook | null; onClose: () => void }) {
  const qc = useQueryClient();
  const users = useAllUsers();
  const [name, setName] = useState(book?.name ?? "");
  const [note, setNote] = useState(book?.note ?? "");
  const [owner, setOwner] = useState(String(book?.owner.id ?? ""));
  const save = useMutation({
    mutationFn: () => {
      const body: Record<string, unknown> = { name, note };
      if (owner) body.owner_id = Number(owner);
      return book ? api(`/address-books/${book.id}`, { method: "PATCH", body }) : api("/address-books", { method: "POST", body });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["address-books"] });
      onClose();
    },
  });
  return (
    <Modal title={book ? "Edit address book" : "New shared address book"} open onClose={onClose}>
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
        <Field label="Note">
          <Input value={note} onChange={(e) => setNote(e.target.value)} />
        </Field>
        <Field label="Owner (always has full control)">
          <Select value={owner} onChange={(e) => setOwner(e.target.value)} className="w-full">
            {!book && <option value="">Me</option>}
            {users.data?.data.map((u) => (
              <option key={u.id} value={u.id}>
                {u.name}
              </option>
            ))}
          </Select>
        </Field>
        <p className="text-xs text-slate-500">
          Devices installed with <code>preset-address-book-name</code> set to this name join the book automatically.
        </p>
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

type ShareRow = { key: string; rule: number }; // key "u:<id>" or "g:<id>"

function ShareForm({ book, onClose }: { book: AddressBook; onClose: () => void }) {
  const qc = useQueryClient();
  const users = useAllUsers();
  const groups = useQuery({ queryKey: ["user-groups"], queryFn: () => api<UserGroup[]>("/user-groups") });
  const toRow = (s: AbShare): ShareRow => ({ key: s.user ? `u:${s.user.id}` : `g:${s.user_group!.id}`, rule: s.rule });
  const [rows, setRows] = useState<ShareRow[]>(book.shares.map(toRow));
  const save = useMutation({
    mutationFn: () =>
      api(`/address-books/${book.id}/shares`, {
        method: "PUT",
        body: rows
          .filter((r) => r.key)
          .map((r) => {
            const id = Number(r.key.slice(2));
            return r.key.startsWith("u:") ? { user_id: id, rule: r.rule } : { user_group_id: id, rule: r.rule };
          }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["address-books"] });
      onClose();
    },
  });
  const update = (i: number, patch: Partial<ShareRow>) => setRows(rows.map((r, j) => (i === j ? { ...r, ...patch } : r)));
  const used = new Set(rows.map((r) => r.key));

  return (
    <Modal title={`Share "${book.name}"`} open onClose={onClose}>
      <div className="space-y-3">
        <p className="text-sm text-slate-500">
          The owner, <b>{book.owner.name}</b>, always has full control.
        </p>
        {rows.map((r, i) => (
          <div key={i} className="flex flex-wrap gap-2 sm:flex-nowrap">
            <Select value={r.key} onChange={(e) => update(i, { key: e.target.value })} className="min-w-0 flex-1 basis-full sm:basis-auto">
              <option value="">Choose…</option>
              <optgroup label="User groups">
                {groups.data?.map((g) => (
                  <option key={g.id} value={`g:${g.id}`} disabled={used.has(`g:${g.id}`) && r.key !== `g:${g.id}`}>
                    {g.name}
                  </option>
                ))}
              </optgroup>
              <optgroup label="Users">
                {users.data?.data
                  .filter((u) => u.id !== book.owner.id)
                  .map((u) => (
                    <option key={u.id} value={`u:${u.id}`} disabled={used.has(`u:${u.id}`) && r.key !== `u:${u.id}`}>
                      {u.name}
                    </option>
                  ))}
              </optgroup>
            </Select>
            <Select value={r.rule} onChange={(e) => update(i, { rule: Number(e.target.value) })}>
              {[1, 2, 3].map((v) => (
                <option key={v} value={v}>
                  {shareRules[v]}
                </option>
              ))}
            </Select>
            <Button variant="ghost" aria-label="Remove" onClick={() => setRows(rows.filter((_, j) => j !== i))}>
              <Trash2 size={16} />
            </Button>
          </div>
        ))}
        <Button size="sm" onClick={() => setRows([...rows, { key: "", rule: 1 }])}>
          <Plus size={14} /> Add user or group
        </Button>
        <p className="text-xs text-slate-500">Read only: use the entries. Read/write and full control: also add, edit and remove entries and tags.</p>
        <ErrorText error={save.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={save.isPending} onClick={() => save.mutate()}>
            Save
          </Button>
        </div>
      </div>
    </Modal>
  );
}
