import { useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router";
import { Plus } from "lucide-react";
import { api, qs, type AdminUser, type DeviceGroup, type LoginSession, type Paged, type UserGroup } from "../lib/api";
import { dateTime, timeAgo } from "../lib/format";
import { useUser } from "../auth";
import { Badge, Button, Card, ErrorText, Field, Input, Loading, Modal, PageHeader, Pagination, Select, Table, Td } from "../components/ui";

const PAGE_SIZE = 50;

export function Users() {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") === "groups" ? "groups" : "users";
  return (
    <>
      <PageHeader title="Users">
        People who log in to the RustDesk app with an account from this server. Admins can also use this web console.
      </PageHeader>
      <div className="mb-4 flex gap-1 border-b border-slate-200 dark:border-slate-800">
        {(["users", "groups"] as const).map((t) => (
          <button
            key={t}
            onClick={() => setParams(t === "users" ? {} : { tab: t }, { replace: true })}
            className={`-mb-px border-b-2 px-3 py-2 text-sm ${
              tab === t ? "border-teal-700 font-medium text-teal-700 dark:text-teal-400" : "border-transparent text-slate-500 hover:text-slate-900 dark:hover:text-white"
            }`}
          >
            {t === "users" ? "Users" : "User groups"}
          </button>
        ))}
      </div>
      {tab === "users" ? <UserList /> : <UserGroupList />}
    </>
  );
}

function UserList() {
  const [search, setSearch] = useState("");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [editing, setEditing] = useState<{ user: AdminUser | null } | null>(null);
  const [sessionsOf, setSessionsOf] = useState<AdminUser | null>(null);
  const list = useQuery({
    queryKey: ["users", q, page],
    queryFn: () => api<Paged<AdminUser>>(`/users${qs({ q, current: page, pageSize: PAGE_SIZE })}`),
    placeholderData: keepPreviousData,
  });

  return (
    <Card>
      <div className="flex flex-wrap gap-2 border-b border-slate-200 p-3 dark:border-slate-800">
        <form
          className="min-w-48 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            setQ(search.trim());
            setPage(1);
          }}
        >
          <Input placeholder="Search name or email…" value={search} onChange={(e) => setSearch(e.target.value)} />
        </form>
        <Button variant="primary" onClick={() => setEditing({ user: null })}>
          <Plus size={16} /> New user
        </Button>
      </div>
      {list.isPending ? (
        <Loading />
      ) : list.error ? (
        <div className="p-4">
          <ErrorText error={list.error} />
        </div>
      ) : (
        <>
          <Table head={["User", "Role", "Group", "Devices", "Last login", ""]} empty={list.data.data.length === 0}>
            {list.data.data.map((u) => (
              <tr key={u.id}>
                <Td>
                  <div className="font-medium">{u.display_name || u.name}</div>
                  <div className="text-xs text-slate-500">
                    {u.name}
                    {u.email && ` · ${u.email}`}
                  </div>
                </Td>
                <Td>
                  <div className="flex flex-wrap gap-1">
                    {u.is_admin ? <Badge tone="accent">Admin</Badge> : <Badge>User</Badge>}
                    {u.status === 0 && <Badge tone="red">Disabled</Badge>}
                    {u.totp_enabled && <Badge tone="green">2FA</Badge>}
                  </div>
                </Td>
                <Td>{u.group?.name ?? "—"}</Td>
                <Td>{u.device_count}</Td>
                <Td className="whitespace-nowrap">{timeAgo(u.last_login_at)}</Td>
                <Td className="text-right whitespace-nowrap">
                  <Button size="sm" variant="ghost" onClick={() => setSessionsOf(u)}>
                    Logins
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setEditing({ user: u })}>
                    Edit
                  </Button>
                </Td>
              </tr>
            ))}
          </Table>
          <Pagination page={page} pageSize={PAGE_SIZE} total={list.data.total} onPage={setPage} />
        </>
      )}
      {editing && <UserForm user={editing.user} onClose={() => setEditing(null)} />}
      {sessionsOf && <UserSessions user={sessionsOf} onClose={() => setSessionsOf(null)} />}
    </Card>
  );
}

function UserForm({ user, onClose }: { user: AdminUser | null; onClose: () => void }) {
  const qc = useQueryClient();
  const me = useUser();
  const [f, setF] = useState({
    name: user?.name ?? "",
    display_name: user?.display_name ?? "",
    email: user?.email ?? "",
    note: user?.note ?? "",
    password: "",
    is_admin: user?.is_admin ?? false,
    enabled: (user?.status ?? 1) === 1,
    group_id: String(user?.group?.id ?? 0),
    reset_totp: false,
  });
  const [confirmDelete, setConfirmDelete] = useState(false);
  const groups = useQuery({ queryKey: ["user-groups"], queryFn: () => api<UserGroup[]>("/user-groups") });
  const isSelf = user?.id === me.id;
  const done = () => {
    qc.invalidateQueries({ queryKey: ["users"] });
    qc.invalidateQueries({ queryKey: ["user-groups"] });
    onClose();
  };
  const save = useMutation({
    mutationFn: () => {
      const body: Record<string, unknown> = {
        name: f.name,
        display_name: f.display_name,
        email: f.email,
        note: f.note,
        is_admin: f.is_admin,
        status: f.enabled ? 1 : 0,
        group_id: Number(f.group_id),
      };
      if (f.password) body.password = f.password;
      if (f.reset_totp) body.reset_totp = true;
      return user ? api(`/users/${user.id}`, { method: "PATCH", body }) : api("/users", { method: "POST", body });
    },
    onSuccess: done,
  });
  const remove = useMutation({ mutationFn: () => api(`/users/${user!.id}`, { method: "DELETE" }), onSuccess: done });
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setF({ ...f, [k]: e.target instanceof HTMLInputElement && e.target.type === "checkbox" ? e.target.checked : e.target.value });

  if (confirmDelete && user) {
    return (
      <Modal title="Delete user?" open onClose={() => setConfirmDelete(false)}>
        <p className="mb-4 text-sm">
          Delete <b>{user.name}</b>? Their {user.device_count} device(s) stay registered without an owner, and they are signed out of RustDesk.
        </p>
        <ErrorText error={remove.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={() => setConfirmDelete(false)}>Cancel</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
            Delete
          </Button>
        </div>
      </Modal>
    );
  }

  return (
    <Modal title={user ? `Edit ${user.name}` : "New user"} open onClose={onClose}>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Username">
            <Input value={f.name} onChange={set("name")} required autoFocus={!user} autoComplete="off" />
          </Field>
          <Field label="Display name">
            <Input value={f.display_name} onChange={set("display_name")} />
          </Field>
        </div>
        <Field label="Email">
          <Input type="email" value={f.email} onChange={set("email")} />
        </Field>
        <Field label={user ? "New password (leave empty to keep)" : "Password"}>
          <Input type="password" value={f.password} onChange={set("password")} required={!user} minLength={8} autoComplete="new-password" />
        </Field>
        <Field label="User group">
          <Select value={f.group_id} onChange={set("group_id")} className="w-full">
            <option value="0">No group</option>
            {groups.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Note">
          <Input value={f.note} onChange={set("note")} />
        </Field>
        <div className="flex flex-wrap gap-x-6 gap-y-2 text-sm">
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={f.is_admin} onChange={set("is_admin")} disabled={isSelf} /> Admin (sees all devices, can use this console)
          </label>
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={f.enabled} onChange={set("enabled")} disabled={isSelf} /> Enabled
          </label>
        </div>
        {user?.totp_enabled && (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={f.reset_totp} onChange={set("reset_totp")} /> Turn off 2FA (lost phone)
          </label>
        )}
        {user && f.password && <p className="text-xs text-slate-500">Changing the password signs this user out of RustDesk everywhere.</p>}
        <ErrorText error={save.error} />
        <div className="flex justify-between gap-2 pt-2">
          {user && !isSelf ? (
            <Button type="button" variant="ghost" className="text-red-600" onClick={() => setConfirmDelete(true)}>
              Delete user
            </Button>
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            <Button type="button" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={save.isPending}>
              Save
            </Button>
          </div>
        </div>
      </form>
    </Modal>
  );
}

function UserSessions({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const qc = useQueryClient();
  const key = ["user-sessions", user.id];
  const list = useQuery({ queryKey: key, queryFn: () => api<LoginSession[]>(`/users/${user.id}/sessions`) });
  const revoke = useMutation({
    mutationFn: (sid: number) => api(`/users/${user.id}/sessions/${sid}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });
  return (
    <Modal title={`Logins of ${user.name}`} open onClose={onClose}>
      {list.isPending ? (
        <Loading />
      ) : list.error ? (
        <ErrorText error={list.error} />
      ) : list.data.length === 0 ? (
        <p className="text-sm text-slate-500">Not logged in anywhere.</p>
      ) : (
        <ul className="divide-y divide-slate-100 text-sm dark:divide-slate-800">
          {list.data.map((s) => (
            <li key={s.id} className="flex items-center justify-between gap-3 py-2">
              <div className="min-w-0">
                <div className="font-medium">
                  {s.kind === "client" ? `RustDesk on ${s.device_info?.name || s.device_id || "unknown"}` : "Web console"}
                  {s.current && <span className="ml-2 text-xs text-slate-500">(this browser)</span>}
                </div>
                <div className="truncate text-xs text-slate-500">
                  {s.kind === "client" ? `ID ${s.device_id} · ${s.device_info?.os ?? ""}` : s.user_agent} · since {dateTime(s.created_at)} · active {timeAgo(s.last_used_at)}
                </div>
              </div>
              {!s.current && (
                <Button size="sm" disabled={revoke.isPending} onClick={() => revoke.mutate(s.id)}>
                  Sign out
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <ErrorText error={revoke.error} />
    </Modal>
  );
}

function UserGroupList() {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<{ group: UserGroup | null } | null>(null);
  const [deleting, setDeleting] = useState<UserGroup | null>(null);
  const list = useQuery({ queryKey: ["user-groups"], queryFn: () => api<UserGroup[]>("/user-groups") });
  const remove = useMutation({
    mutationFn: (id: number) => api(`/user-groups/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      setDeleting(null);
      qc.invalidateQueries({ queryKey: ["user-groups"] });
      qc.invalidateQueries({ queryKey: ["users"] });
    },
  });
  return (
    <Card>
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 p-3 dark:border-slate-800">
        <p className="text-sm text-slate-500 dark:text-slate-400">
          Members of a group see each other and each other&apos;s devices in RustDesk, plus the device groups you allow.
        </p>
        <Button variant="primary" onClick={() => setEditing({ group: null })}>
          <Plus size={16} /> New user group
        </Button>
      </div>
      {list.isPending ? (
        <Loading />
      ) : list.error ? (
        <div className="p-4">
          <ErrorText error={list.error} />
        </div>
      ) : (
        <Table head={["Name", "Members", "Can access device groups", ""]} empty={list.data.length === 0}>
          {list.data.map((g) => (
            <tr key={g.id}>
              <Td>
                <div className="font-medium">{g.name}</div>
                {g.note && <div className="text-xs text-slate-500">{g.note}</div>}
              </Td>
              <Td>{g.member_count}</Td>
              <Td>
                <div className="flex flex-wrap gap-1">
                  {g.device_groups.length === 0 ? "—" : g.device_groups.map((d) => <Badge key={d.id}>{d.name}</Badge>)}
                </div>
              </Td>
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
      {editing && <UserGroupForm group={editing.group} onClose={() => setEditing(null)} />}
      <Modal title="Delete user group?" open={!!deleting} onClose={() => setDeleting(null)}>
        <p className="mb-4 text-sm">
          Delete <b>{deleting?.name}</b>? Its {deleting?.member_count} member(s) are kept but no longer belong to a group.
        </p>
        <ErrorText error={remove.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={() => setDeleting(null)}>Cancel</Button>
          <Button variant="danger" disabled={remove.isPending} onClick={() => deleting && remove.mutate(deleting.id)}>
            Delete
          </Button>
        </div>
      </Modal>
    </Card>
  );
}

function UserGroupForm({ group, onClose }: { group: UserGroup | null; onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState(group?.name ?? "");
  const [note, setNote] = useState(group?.note ?? "");
  const [access, setAccess] = useState<number[]>(group?.device_groups.map((d) => d.id) ?? []);
  const deviceGroups = useQuery({ queryKey: ["device-groups"], queryFn: () => api<DeviceGroup[]>("/device-groups") });
  const save = useMutation({
    mutationFn: () => {
      const body = { name, note, device_group_ids: access };
      return group ? api(`/user-groups/${group.id}`, { method: "PATCH", body }) : api("/user-groups", { method: "POST", body });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["user-groups"] });
      onClose();
    },
  });
  const toggle = (id: number) => setAccess(access.includes(id) ? access.filter((x) => x !== id) : [...access, id]);
  return (
    <Modal title={group ? "Edit user group" : "New user group"} open onClose={onClose}>
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
        <fieldset>
          <legend className="mb-1 text-sm font-medium">Members can see devices in</legend>
          {deviceGroups.data?.length === 0 && (
            <p className="text-sm text-slate-500">
              No device groups yet. <Link to="/device-groups" className="text-teal-700 hover:underline">Create one</Link>.
            </p>
          )}
          <div className="max-h-48 space-y-1 overflow-y-auto">
            {deviceGroups.data?.map((d) => (
              <label key={d.id} className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={access.includes(d.id)} onChange={() => toggle(d.id)} />
                {d.name} <span className="text-slate-500">({d.device_count})</span>
              </label>
            ))}
          </div>
        </fieldset>
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
