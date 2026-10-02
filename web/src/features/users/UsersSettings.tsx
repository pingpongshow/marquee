import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  KeyRound,
  Pencil,
  Plus,
  Shield,
  Trash2,
  UserPlus,
  UserRound,
  Users,
} from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { liveGroupsQuery, liveStatusQuery } from "../livetv/api";
import { requestsStatusQuery } from "../requests/api";
import {
  librariesQuery,
  meQuery,
  useCreateUser,
  useDeleteUser,
  usersQuery,
  useUpdateUser,
} from "@/api/queries";
import type { User, UserRestrictions } from "@/api/types";
import {
  Alert,
  Badge,
  Button,
  Card,
  Dialog,
  Field,
  Input,
  Select,
  Spinner,
  Toggle,
} from "@/components/ui";
import { SaveBar } from "../settings/SaveBar";
import { useSectionDraft } from "../settings/useSectionDraft";
import { Avatar, AvatarPicker } from "./Avatar";
import { ratingOptions, remoteQualityOptions } from "./constants";
import { InviteDialog, InviteList } from "./Invites";

function describe(r: UserRestrictions, libraryNames: Map<number, string>) {
  const parts: string[] = [];
  if (r.libraryIds)
    parts.push(
      r.libraryIds.length
        ? r.libraryIds.map((id) => libraryNames.get(id) ?? "?").join(", ")
        : "No libraries",
    );
  if (r.maxContentRating) parts.push(`Up to ${r.maxContentRating}`);
  if (r.allowRemote === false) parts.push("Home network only");
  if (r.remoteQualityKbps)
    parts.push(`Remote ≤ ${(r.remoteQualityKbps / 1000).toFixed(0)} Mbps`);
  if (r.liveTv === false) parts.push("No Live TV");
  else if (r.liveTvGroups)
    parts.push(`Live TV: ${r.liveTvGroups.join(", ") || "none"}`);
  return parts.join(" · ");
}

/** May this person request titles (Seerr), and as which Seerr user. */
function RequestPermission({
  value,
  onChange,
}: {
  value: UserRestrictions;
  onChange: (r: UserRestrictions) => void;
}) {
  const status = useQuery(requestsStatusQuery);
  const seerrUsers = useQuery({
    queryKey: ["requests", "seerr-users"],
    queryFn: () => unwrap(api.GET("/requests/seerr/users")),
    enabled: !!status.data?.enabled && !!value.canRequest,
  });
  if (!status.data?.enabled) return null;
  return (
    <div className="space-y-3">
      <Toggle
        label="Can request movies and shows"
        help="Through Seerr. Every request still needs an admin's approval."
        checked={!!value.canRequest}
        onChange={(v) => onChange({ ...value, canRequest: v })}
      />
      {value.canRequest && (
        <Field
          label="Request as Seerr user"
          help="Requests appear in Seerr under this user."
        >
          {(id) => (
            <Select
              id={id}
              value={value.seerrUserId ?? ""}
              onChange={(e) =>
                onChange({
                  ...value,
                  seerrUserId: e.target.value ? Number(e.target.value) : null,
                })
              }
            >
              <option value="">Seerr's admin (the API key)</option>
              {seerrUsers.data?.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.displayName}
                </option>
              ))}
            </Select>
          )}
        </Field>
      )}
    </div>
  );
}

/** May this person watch Live TV, and which channel groups (LIVE-4). */
function LiveTvPermission({
  value,
  onChange,
}: {
  value: UserRestrictions;
  onChange: (r: UserRestrictions) => void;
}) {
  const status = useQuery(liveStatusQuery);
  const groups = useQuery({
    ...liveGroupsQuery,
    enabled: !!status.data?.enabled,
  });
  if (!status.data?.enabled) return null;
  const allowed = value.liveTv !== false;
  const chosen = value.liveTvGroups ?? null;
  const toggle = (g: string, on: boolean) => {
    const all = groups.data?.map((x) => x.name) ?? [];
    const next = new Set(chosen ?? all);
    if (on) next.add(g);
    else next.delete(g);
    onChange({
      ...value,
      liveTvGroups: next.size === all.length ? null : [...next],
    });
  };
  return (
    <div className="space-y-3">
      <Toggle
        label="Can watch Live TV"
        checked={allowed}
        onChange={(v) => onChange({ ...value, liveTv: v })}
      />
      {allowed && !!groups.data?.length && (
        <Field
          label="Channel groups"
          help="Only channels in the ticked groups appear for this person."
        >
          {() => (
            <div className="flex flex-wrap gap-x-5 gap-y-2">
              {groups.data.map((g) => (
                <label key={g.name} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={chosen === null || chosen.includes(g.name)}
                    onChange={(e) => toggle(g.name, e.target.checked)}
                  />
                  {g.name}
                </label>
              ))}
            </div>
          )}
        </Field>
      )}
    </div>
  );
}

/** May this person schedule Live TV recordings (LIVE-5). */
function RecordPermission({
  value,
  onChange,
}: {
  value: UserRestrictions;
  onChange: (r: UserRestrictions) => void;
}) {
  const status = useQuery(liveStatusQuery);
  if (!status.data?.enabled || !status.data.dvrAvailable) return null;
  return (
    <Toggle
      label="Can record Live TV"
      help="Schedule recordings and series, and cancel upcoming ones."
      checked={!!value.canRecord}
      onChange={(v) => onChange({ ...value, canRecord: v })}
    />
  );
}

/** Restrictions editor shared by the add and edit dialogs. */
function RestrictionsEditor({
  value,
  onChange,
}: {
  value: UserRestrictions;
  onChange: (r: UserRestrictions) => void;
}) {
  const libraries = useQuery(librariesQuery);
  const all = value.libraryIds == null;
  const selected = new Set(value.libraryIds ?? []);
  return (
    <div className="space-y-5">
      <div>
        <Toggle
          label="Access to all libraries"
          help="Includes libraries added later."
          checked={all}
          onChange={(v) =>
            onChange({
              ...value,
              libraryIds: v
                ? undefined
                : (libraries.data?.map((l) => l.id) ?? []),
            })
          }
        />
        {!all && (
          <div className="mt-3 grid gap-2 sm:grid-cols-2">
            {libraries.data?.map((l) => (
              <label
                key={l.id}
                className="flex items-center gap-2 rounded-md bg-surface-2 px-3 py-2 text-sm"
              >
                <input
                  type="checkbox"
                  className="size-4 accent-[var(--color-accent)]"
                  checked={selected.has(l.id)}
                  onChange={(e) => {
                    const next = new Set(selected);
                    if (e.target.checked) next.add(l.id);
                    else next.delete(l.id);
                    onChange({ ...value, libraryIds: [...next] });
                  }}
                />
                {l.name}
              </label>
            ))}
          </div>
        )}
      </div>
      <Field
        label="Maximum content rating"
        help="Movies and shows rated higher, and unrated ones, are hidden. Music isn't affected."
      >
        {(id) => (
          <Select
            id={id}
            value={value.maxContentRating ?? ""}
            onChange={(e) =>
              onChange({
                ...value,
                maxContentRating: (e.target.value ||
                  undefined) as UserRestrictions["maxContentRating"],
              })
            }
          >
            {ratingOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <RequestPermission value={value} onChange={onChange} />
      <LiveTvPermission value={value} onChange={onChange} />
      <RecordPermission value={value} onChange={onChange} />
      <Toggle
        label="Allow streaming away from home"
        help="Over Tailscale. Turn off to limit this person to the home network."
        checked={value.allowRemote !== false}
        onChange={(v) => onChange({ ...value, allowRemote: v })}
      />
      <Field label="Remote quality limit">
        {(id) => (
          <Select
            id={id}
            value={value.remoteQualityKbps ?? 0}
            onChange={(e) =>
              onChange({ ...value, remoteQualityKbps: Number(e.target.value) })
            }
          >
            {remoteQualityOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </Select>
        )}
      </Field>
    </div>
  );
}

function AddUserDialog({ onClose }: { onClose: () => void }) {
  const create = useCreateUser();
  const [managed, setManaged] = useState(false);
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [pin, setPin] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);
  const [restrictions, setRestrictions] = useState<UserRestrictions>({});
  // Only administrators need a password; anyone else may have a password, a PIN, both or neither.
  const valid =
    username.trim() &&
    (!password || password.length >= 8) &&
    (!isAdmin || managed || password.length >= 8) &&
    (!pin || /^\d{4}$/.test(pin));

  return (
    <Dialog
      open
      wide
      onClose={onClose}
      title="Add user"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={!valid}
            loading={create.isPending}
            onClick={() =>
              create.mutate(
                {
                  username: username.trim(),
                  displayName: displayName.trim() || undefined,
                  password: managed ? undefined : password,
                  pin: pin || undefined,
                  isManaged: managed,
                  isAdmin: !managed && isAdmin,
                  restrictions,
                },
                { onSuccess: onClose },
              )
            }
          >
            Add user
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        {create.isError && <Alert tone="error">{create.error.message}</Alert>}
        <div className="grid gap-2 sm:grid-cols-2">
          {[
            {
              m: false,
              icon: UserRound,
              title: "Person",
              help: "Signs in by choosing their profile, with an optional PIN or password.",
            },
            {
              m: true,
              icon: Users,
              title: "Managed profile",
              help: "For children or shared TVs. Opened by switching profile, optionally with a PIN.",
            },
          ].map((o) => (
            <button
              key={o.title}
              type="button"
              aria-pressed={managed === o.m}
              onClick={() => setManaged(o.m)}
              className={
                managed === o.m
                  ? "flex gap-3 rounded-lg border border-accent bg-accent/10 p-3 text-left"
                  : "flex gap-3 rounded-lg border border-border p-3 text-left hover:bg-surface-2"
              }
            >
              <o.icon
                className="mt-0.5 size-5 shrink-0 text-accent"
                aria-hidden
              />
              <div>
                <div className="text-sm font-medium">{o.title}</div>
                <div className="text-xs text-muted">{o.help}</div>
              </div>
            </button>
          ))}
        </div>
        <div className="grid gap-5 sm:grid-cols-2">
          <Field label="Username">
            {(id) => (
              <Input
                id={id}
                autoComplete="off"
                maxLength={64}
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            )}
          </Field>
          <Field label="Display name" help="Optional.">
            {(id) => (
              <Input
                id={id}
                maxLength={64}
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
              />
            )}
          </Field>
          {!managed && (
            <Field
              label="Password"
              help={
                isAdmin
                  ? "Required for administrators. At least 8 characters."
                  : "Optional. Needed to sign in from outside the home network if PIN sign-in is home-only."
              }
            >
              {(id) => (
                <Input
                  id={id}
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              )}
            </Field>
          )}
          <Field
            label="PIN"
            help="Optional, 4 digits. Asked when choosing this profile."
          >
            {(id) => (
              <Input
                id={id}
                inputMode="numeric"
                maxLength={4}
                value={pin}
                onChange={(e) => setPin(e.target.value.replace(/\D/g, ""))}
              />
            )}
          </Field>
        </div>
        {!managed && (
          <Toggle
            label="Administrator"
            help="Can change settings, libraries and users."
            checked={isAdmin}
            onChange={setIsAdmin}
          />
        )}
        {!(isAdmin && !managed) && (
          <RestrictionsEditor value={restrictions} onChange={setRestrictions} />
        )}
      </div>
    </Dialog>
  );
}

function EditUserDialog({
  user,
  isSelf,
  onClose,
}: {
  user: User;
  isSelf: boolean;
  onClose: () => void;
}) {
  const update = useUpdateUser();
  const live = useQuery(usersQuery).data?.find((u) => u.id === user.id) ?? user; // picture changes save immediately
  const [displayName, setDisplayName] = useState(user.displayName);
  const [password, setPassword] = useState("");
  const [pin, setPin] = useState<string | null>(null);
  const [isAdmin, setIsAdmin] = useState(user.isAdmin);
  const [restrictions, setRestrictions] = useState<UserRestrictions>(
    user.restrictions,
  );
  const needsPassword = isAdmin && !user.isAdmin && !user.hasPassword;
  const valid =
    displayName.trim() &&
    (!password || password.length >= 8) &&
    (!needsPassword || password.length >= 8) &&
    (pin === null || pin === "" || /^\d{4}$/.test(pin));

  return (
    <Dialog
      open
      wide
      onClose={onClose}
      title={`Edit ${user.displayName}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={!valid}
            loading={update.isPending}
            onClick={() =>
              update.mutate(
                {
                  id: user.id,
                  body: {
                    displayName: displayName.trim(),
                    password: password || undefined,
                    pin: pin ?? undefined,
                    isAdmin: user.isManaged ? undefined : isAdmin,
                    restrictions,
                  },
                },
                { onSuccess: onClose },
              )
            }
          >
            Save changes
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        {update.isError && <Alert tone="error">{update.error.message}</Alert>}
        <AvatarPicker user={live} />
        <div className="grid gap-5 sm:grid-cols-2">
          <Field label="Display name">
            {(id) => (
              <Input
                id={id}
                maxLength={64}
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
              />
            )}
          </Field>
          {!user.isManaged && (
            <Field
              label={user.hasPassword ? "Reset password" : "Set a password"}
              help={
                needsPassword
                  ? "Administrators need a password."
                  : user.hasPassword
                    ? "Leave empty to keep the current one."
                    : "Optional."
              }
            >
              {(id) => (
                <Input
                  id={id}
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              )}
            </Field>
          )}
          <Field
            label="PIN"
            help={
              user.hasPin
                ? "Set. Enter a new PIN, or clear it below."
                : "Not set."
            }
          >
            {(id) => (
              <div className="flex gap-2">
                <Input
                  id={id}
                  inputMode="numeric"
                  maxLength={4}
                  placeholder={user.hasPin ? "••••" : ""}
                  value={pin ?? ""}
                  onChange={(e) => setPin(e.target.value.replace(/\D/g, ""))}
                />
                {user.hasPin && (
                  <Button variant="ghost" onClick={() => setPin("")}>
                    Remove
                  </Button>
                )}
              </div>
            )}
          </Field>
        </div>
        {live.hasTwoFactor && !isSelf && <TwoFactorOff user={live} />}
        {!user.isManaged && (
          <Toggle
            label="Administrator"
            help={
              isSelf
                ? "You can't remove your own admin access if you're the only administrator."
                : undefined
            }
            checked={isAdmin}
            onChange={setIsAdmin}
          />
        )}
        {!isAdmin && (
          <RestrictionsEditor value={restrictions} onChange={setRestrictions} />
        )}
      </div>
    </Dialog>
  );
}

/** Turns off another user's two-factor sign-in, e.g. when they've lost their phone (USER-9). */
function TwoFactorOff({ user }: { user: User }) {
  const qc = useQueryClient();
  const off = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/auth/totp/disable", { body: { userId: user.id } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersQuery.queryKey }),
  });
  return (
    <div className="flex items-center justify-between gap-4 rounded-lg border border-border p-3">
      <div className="text-sm">
        <div className="font-medium">Two-factor sign-in is on</div>
        <div className="text-muted">
          Turn it off if {user.displayName} can't get to their authenticator app
          or recovery codes.
        </div>
        {off.isError && <Alert tone="error">{off.error.message}</Alert>}
      </div>
      <Button
        variant="ghost"
        loading={off.isPending}
        onClick={() => off.mutate()}
      >
        Turn off
      </Button>
    </div>
  );
}

function SignInOptions() {
  const s = useSectionDraft("security");
  if (!s.draft) return null;
  return (
    <Card
      title="Sign-in"
      description="How people sign in on this server's apps and web page."
    >
      <Field
        label="PIN sign-in"
        help="Shows a “Who's watching?” profile picker where people enter their 4-digit PIN. Over Tailscale, a password is safer than a 4-digit PIN."
      >
        {(id) => (
          <Select
            id={id}
            value={s.draft!.pinSignIn ?? "local"}
            onChange={(e) =>
              s.update({
                pinSignIn: e.target.value as "off" | "local" | "everywhere",
              })
            }
          >
            <option value="local">
              On the home network only (recommended)
            </option>
            <option value="everywhere">Everywhere, including remote</option>
            <option value="off">Off: always use a password</option>
          </Select>
        )}
      </Field>
      <p className="text-xs text-faint">
        Five wrong PINs lock that profile for 15 minutes. People without a PIN
        use their password; managed profiles without a PIN open with one tap.
      </p>
      <SaveBar
        dirty={s.dirty}
        saving={s.saving}
        error={s.error}
        savedAt={s.savedAt}
        onSave={s.save}
        onReset={s.reset}
      />
    </Card>
  );
}

export function UsersSettings() {
  const users = useQuery(usersQuery);
  const me = useQuery(meQuery);
  const libraries = useQuery(librariesQuery);
  const del = useDeleteUser();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<User | null>(null);
  const [deleting, setDeleting] = useState<User | null>(null);
  const libraryNames = new Map(
    (libraries.data ?? []).map((l) => [l.id, l.name]),
  );
  const [inviting, setInviting] = useState(false);
  // Friends joined through an invite (USER-13); everyone else is the household.
  const household = users.data?.filter((u) => !u.restrictions.friend) ?? [];
  const friends = users.data?.filter((u) => u.restrictions.friend) ?? [];
  const row = (u: User) => (
    <li key={u.id} className="flex items-center gap-4 py-3">
      <Avatar
        name={u.displayName}
        url={u.avatarUrl}
        className="size-10 text-sm"
      />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{u.displayName}</span>
          <span className="text-xs text-faint">@{u.username}</span>
          {u.isAdmin && (
            <Badge tone="accent">
              <Shield className="mr-0.5 inline size-3" aria-hidden />{" "}
              Admin
            </Badge>
          )}
          {u.isManaged && <Badge>Managed</Badge>}
          {u.hasPin && (
            <Badge>
              <KeyRound className="mr-0.5 inline size-3" aria-hidden />{" "}
              PIN
            </Badge>
          )}
          {u.id === me.data?.id && <Badge>You</Badge>}
        </div>
        <div className="truncate text-xs text-muted">
          {u.isAdmin
            ? "Full access"
            : describe(u.restrictions, libraryNames) || "All libraries"}
          {u.lastSeenAt &&
            ` · Last active ${new Date(u.lastSeenAt).toLocaleDateString()}`}
        </div>
      </div>
      <Button
        size="sm"
        variant="ghost"
        aria-label={`Edit ${u.displayName}`}
        onClick={() => setEditing(u)}
      >
        <Pencil className="size-4" />
      </Button>
      {u.id !== me.data?.id && (
        <Button
          size="sm"
          variant="ghost"
          aria-label={`Delete ${u.displayName}`}
          onClick={() => setDeleting(u)}
        >
          <Trash2 className="size-4" />
        </Button>
      )}
    </li>
  );

  return (
    <div className="space-y-6">
      <SignInOptions />
      <Card
        title="Users"
        description="Everyone in your household. Managed profiles are for children and shared TVs."
        actions={
          <Button size="sm" variant="primary" onClick={() => setAdding(true)}>
            <Plus className="size-4" /> Add user
          </Button>
        }
      >
        {users.isPending && <Spinner />}
        <ul className="-my-2 divide-y divide-border">{household.map(row)}</ul>
      </Card>
      <Card
        title="Friends"
        description="People outside your household you've shared this server with. They sign in with their own password and can't switch profiles."
        actions={
          <Button size="sm" variant="primary" onClick={() => setInviting(true)}>
            <UserPlus className="size-4" /> Invite a friend
          </Button>
        }
      >
        {friends.length > 0 ? (
          <ul className="-my-2 divide-y divide-border" aria-label="Friends">
            {friends.map(row)}
          </ul>
        ) : (
          <p className="text-sm text-muted">No friends have joined yet.</p>
        )}
        <InviteList />
      </Card>
      {inviting && (
        <InviteDialog
          onClose={() => setInviting(false)}
          restrictionsEditor={(value, onChange) => (
            <RestrictionsEditor value={value} onChange={onChange} />
          )}
        />
      )}
      {adding && <AddUserDialog onClose={() => setAdding(false)} />}
      {editing && (
        <EditUserDialog
          user={editing}
          isSelf={editing.id === me.data?.id}
          onClose={() => setEditing(null)}
        />
      )}
      <Dialog
        open={!!deleting}
        onClose={() => setDeleting(null)}
        title="Delete user?"
        footer={
          <>
            <Button variant="ghost" onClick={() => setDeleting(null)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              loading={del.isPending}
              onClick={() =>
                deleting &&
                del.mutate(deleting.id, { onSuccess: () => setDeleting(null) })
              }
            >
              Delete user
            </Button>
          </>
        }
      >
        <p className="text-sm text-muted">
          <strong className="text-text">{deleting?.displayName}</strong> will be
          signed out everywhere, and their watch history and playlists will be
          removed.
        </p>
        {del.isError && (
          <div className="mt-3">
            <Alert tone="error">{del.error.message}</Alert>
          </div>
        )}
      </Dialog>
    </div>
  );
}
