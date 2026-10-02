import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { KeyRound, Pencil, Plus, Shield, Trash2, UserRound, Users } from "lucide-react";
import { useState } from "react";
import { librariesQuery, meQuery, useCreateUser, useDeleteUser, usersQuery, useUpdateUser } from "@/api/queries";
import { Alert, Button, Card, Dialog, Field, Input, Select, Spinner, Toggle } from "@/components/ui";
import { SaveBar } from "../settings/SaveBar";
import { useSectionDraft } from "../settings/useSectionDraft";
import { Avatar, AvatarPicker } from "./Avatar";
import { ratingOptions, remoteQualityOptions } from "./constants";
function Badge({ children, tone = "muted" }) {
    return _jsx("span", { className: tone === "accent" ? "rounded bg-accent/15 px-1.5 py-0.5 text-[11px] font-medium text-accent" : "rounded bg-surface-3 px-1.5 py-0.5 text-[11px] text-muted", children: children });
}
function describe(r, libraryNames) {
    const parts = [];
    if (r.libraryIds)
        parts.push(r.libraryIds.length ? r.libraryIds.map((id) => libraryNames.get(id) ?? "?").join(", ") : "No libraries");
    if (r.maxContentRating)
        parts.push(`Up to ${r.maxContentRating}`);
    if (r.allowRemote === false)
        parts.push("Home network only");
    if (r.remoteQualityKbps)
        parts.push(`Remote ≤ ${(r.remoteQualityKbps / 1000).toFixed(0)} Mbps`);
    return parts.join(" · ");
}
/** Restrictions editor shared by the add and edit dialogs. */
function RestrictionsEditor({ value, onChange }) {
    const libraries = useQuery(librariesQuery);
    const all = value.libraryIds == null;
    const selected = new Set(value.libraryIds ?? []);
    return (_jsxs("div", { className: "space-y-5", children: [_jsxs("div", { children: [_jsx(Toggle, { label: "Access to all libraries", help: "Includes libraries added later.", checked: all, onChange: (v) => onChange({ ...value, libraryIds: v ? undefined : (libraries.data?.map((l) => l.id) ?? []) }) }), !all && (_jsx("div", { className: "mt-3 grid gap-2 sm:grid-cols-2", children: libraries.data?.map((l) => (_jsxs("label", { className: "flex items-center gap-2 rounded-md bg-surface-2 px-3 py-2 text-sm", children: [_jsx("input", { type: "checkbox", className: "size-4 accent-[var(--color-accent)]", checked: selected.has(l.id), onChange: (e) => {
                                        const next = new Set(selected);
                                        if (e.target.checked)
                                            next.add(l.id);
                                        else
                                            next.delete(l.id);
                                        onChange({ ...value, libraryIds: [...next] });
                                    } }), l.name] }, l.id))) }))] }), _jsx(Field, { label: "Maximum content rating", help: "Movies and shows rated higher, and unrated ones, are hidden. Music isn't affected.", children: (id) => (_jsx(Select, { id: id, value: value.maxContentRating ?? "", onChange: (e) => onChange({ ...value, maxContentRating: (e.target.value || undefined) }), children: ratingOptions.map((o) => (_jsx("option", { value: o.value, children: o.label }, o.value))) })) }), _jsx(Toggle, { label: "Allow streaming away from home", help: "Over Tailscale. Turn off to limit this person to the home network.", checked: value.allowRemote !== false, onChange: (v) => onChange({ ...value, allowRemote: v }) }), _jsx(Field, { label: "Remote quality limit", children: (id) => (_jsx(Select, { id: id, value: value.remoteQualityKbps ?? 0, onChange: (e) => onChange({ ...value, remoteQualityKbps: Number(e.target.value) }), children: remoteQualityOptions.map((o) => (_jsx("option", { value: o.value, children: o.label }, o.value))) })) })] }));
}
function AddUserDialog({ onClose }) {
    const create = useCreateUser();
    const [managed, setManaged] = useState(false);
    const [username, setUsername] = useState("");
    const [displayName, setDisplayName] = useState("");
    const [password, setPassword] = useState("");
    const [pin, setPin] = useState("");
    const [isAdmin, setIsAdmin] = useState(false);
    const [restrictions, setRestrictions] = useState({});
    // Only administrators need a password; anyone else may have a password, a PIN, both or neither.
    const valid = username.trim() && (!password || password.length >= 8) && (!isAdmin || managed || password.length >= 8) && (!pin || /^\d{4}$/.test(pin));
    return (_jsx(Dialog, { open: true, wide: true, onClose: onClose, title: "Add user", footer: _jsxs(_Fragment, { children: [_jsx(Button, { variant: "ghost", onClick: onClose, children: "Cancel" }), _jsx(Button, { variant: "primary", disabled: !valid, loading: create.isPending, onClick: () => create.mutate({ username: username.trim(), displayName: displayName.trim() || undefined, password: managed ? undefined : password, pin: pin || undefined, isManaged: managed, isAdmin: !managed && isAdmin, restrictions }, { onSuccess: onClose }), children: "Add user" })] }), children: _jsxs("div", { className: "space-y-5", children: [create.isError && _jsx(Alert, { tone: "error", children: create.error.message }), _jsx("div", { className: "grid gap-2 sm:grid-cols-2", children: [
                        { m: false, icon: UserRound, title: "Person", help: "Signs in by choosing their profile, with an optional PIN or password." },
                        { m: true, icon: Users, title: "Managed profile", help: "For children or shared TVs. Opened by switching profile, optionally with a PIN." },
                    ].map((o) => (_jsxs("button", { type: "button", "aria-pressed": managed === o.m, onClick: () => setManaged(o.m), className: managed === o.m ? "flex gap-3 rounded-lg border border-accent bg-accent/10 p-3 text-left" : "flex gap-3 rounded-lg border border-border p-3 text-left hover:bg-surface-2", children: [_jsx(o.icon, { className: "mt-0.5 size-5 shrink-0 text-accent", "aria-hidden": true }), _jsxs("div", { children: [_jsx("div", { className: "text-sm font-medium", children: o.title }), _jsx("div", { className: "text-xs text-muted", children: o.help })] })] }, o.title))) }), _jsxs("div", { className: "grid gap-5 sm:grid-cols-2", children: [_jsx(Field, { label: "Username", children: (id) => _jsx(Input, { id: id, autoComplete: "off", maxLength: 64, value: username, onChange: (e) => setUsername(e.target.value) }) }), _jsx(Field, { label: "Display name", help: "Optional.", children: (id) => _jsx(Input, { id: id, maxLength: 64, value: displayName, onChange: (e) => setDisplayName(e.target.value) }) }), !managed && (_jsx(Field, { label: "Password", help: isAdmin ? "Required for administrators. At least 8 characters." : "Optional. Needed to sign in from outside the home network if PIN sign-in is home-only.", children: (id) => _jsx(Input, { id: id, type: "password", autoComplete: "new-password", value: password, onChange: (e) => setPassword(e.target.value) }) })), _jsx(Field, { label: "PIN", help: "Optional, 4 digits. Asked when choosing this profile.", children: (id) => _jsx(Input, { id: id, inputMode: "numeric", maxLength: 4, value: pin, onChange: (e) => setPin(e.target.value.replace(/\D/g, "")) }) })] }), !managed && _jsx(Toggle, { label: "Administrator", help: "Can change settings, libraries and users.", checked: isAdmin, onChange: setIsAdmin }), !(isAdmin && !managed) && _jsx(RestrictionsEditor, { value: restrictions, onChange: setRestrictions })] }) }));
}
function EditUserDialog({ user, isSelf, onClose }) {
    const update = useUpdateUser();
    const live = useQuery(usersQuery).data?.find((u) => u.id === user.id) ?? user; // picture changes save immediately
    const [displayName, setDisplayName] = useState(user.displayName);
    const [password, setPassword] = useState("");
    const [pin, setPin] = useState(null);
    const [isAdmin, setIsAdmin] = useState(user.isAdmin);
    const [restrictions, setRestrictions] = useState(user.restrictions);
    const needsPassword = isAdmin && !user.isAdmin && !user.hasPassword;
    const valid = displayName.trim() && (!password || password.length >= 8) && (!needsPassword || password.length >= 8) && (pin === null || pin === "" || /^\d{4}$/.test(pin));
    return (_jsx(Dialog, { open: true, wide: true, onClose: onClose, title: `Edit ${user.displayName}`, footer: _jsxs(_Fragment, { children: [_jsx(Button, { variant: "ghost", onClick: onClose, children: "Cancel" }), _jsx(Button, { variant: "primary", disabled: !valid, loading: update.isPending, onClick: () => update.mutate({ id: user.id, body: { displayName: displayName.trim(), password: password || undefined, pin: pin ?? undefined, isAdmin: user.isManaged ? undefined : isAdmin, restrictions } }, { onSuccess: onClose }), children: "Save changes" })] }), children: _jsxs("div", { className: "space-y-5", children: [update.isError && _jsx(Alert, { tone: "error", children: update.error.message }), _jsx(AvatarPicker, { user: live }), _jsxs("div", { className: "grid gap-5 sm:grid-cols-2", children: [_jsx(Field, { label: "Display name", children: (id) => _jsx(Input, { id: id, maxLength: 64, value: displayName, onChange: (e) => setDisplayName(e.target.value) }) }), !user.isManaged && (_jsx(Field, { label: user.hasPassword ? "Reset password" : "Set a password", help: needsPassword ? "Administrators need a password." : user.hasPassword ? "Leave empty to keep the current one." : "Optional.", children: (id) => _jsx(Input, { id: id, type: "password", autoComplete: "new-password", value: password, onChange: (e) => setPassword(e.target.value) }) })), _jsx(Field, { label: "PIN", help: user.hasPin ? "Set. Enter a new PIN, or clear it below." : "Not set.", children: (id) => (_jsxs("div", { className: "flex gap-2", children: [_jsx(Input, { id: id, inputMode: "numeric", maxLength: 4, placeholder: user.hasPin ? "••••" : "", value: pin ?? "", onChange: (e) => setPin(e.target.value.replace(/\D/g, "")) }), user.hasPin && (_jsx(Button, { variant: "ghost", onClick: () => setPin(""), children: "Remove" }))] })) })] }), !user.isManaged && _jsx(Toggle, { label: "Administrator", help: isSelf ? "You can't remove your own admin access if you're the only administrator." : undefined, checked: isAdmin, onChange: setIsAdmin }), !isAdmin && _jsx(RestrictionsEditor, { value: restrictions, onChange: setRestrictions })] }) }));
}
function SignInOptions() {
    const s = useSectionDraft("security");
    if (!s.draft)
        return null;
    return (_jsxs(Card, { title: "Sign-in", description: "How people sign in on this server's apps and web page.", children: [_jsx(Field, { label: "PIN sign-in", help: "Shows a \u201CWho's watching?\u201D profile picker where people enter their 4-digit PIN. Over Tailscale, a password is safer than a 4-digit PIN.", children: (id) => (_jsxs(Select, { id: id, value: s.draft.pinSignIn ?? "local", onChange: (e) => s.update({ pinSignIn: e.target.value }), children: [_jsx("option", { value: "local", children: "On the home network only (recommended)" }), _jsx("option", { value: "everywhere", children: "Everywhere, including remote" }), _jsx("option", { value: "off", children: "Off: always use a password" })] })) }), _jsx("p", { className: "text-xs text-faint", children: "Five wrong PINs lock that profile for 15 minutes. People without a PIN use their password; managed profiles without a PIN open with one tap." }), _jsx(SaveBar, { dirty: s.dirty, saving: s.saving, error: s.error, savedAt: s.savedAt, onSave: s.save, onReset: s.reset })] }));
}
export function UsersSettings() {
    const users = useQuery(usersQuery);
    const me = useQuery(meQuery);
    const libraries = useQuery(librariesQuery);
    const del = useDeleteUser();
    const [adding, setAdding] = useState(false);
    const [editing, setEditing] = useState(null);
    const [deleting, setDeleting] = useState(null);
    const libraryNames = new Map((libraries.data ?? []).map((l) => [l.id, l.name]));
    return (_jsxs("div", { className: "space-y-6", children: [_jsx(SignInOptions, {}), _jsxs(Card, { title: "Users", description: "Everyone who can use this server. Managed profiles are for children and shared TVs.", actions: _jsxs(Button, { size: "sm", variant: "primary", onClick: () => setAdding(true), children: [_jsx(Plus, { className: "size-4" }), " Add user"] }), children: [users.isPending && _jsx(Spinner, {}), _jsx("ul", { className: "-my-2 divide-y divide-border", children: users.data?.map((u) => (_jsxs("li", { className: "flex items-center gap-4 py-3", children: [_jsx(Avatar, { name: u.displayName, url: u.avatarUrl, className: "size-10 text-sm" }), _jsxs("div", { className: "min-w-0 flex-1", children: [_jsxs("div", { className: "flex flex-wrap items-center gap-2", children: [_jsx("span", { className: "font-medium", children: u.displayName }), _jsxs("span", { className: "text-xs text-faint", children: ["@", u.username] }), u.isAdmin && (_jsxs(Badge, { tone: "accent", children: [_jsx(Shield, { className: "mr-0.5 inline size-3", "aria-hidden": true }), " Admin"] })), u.isManaged && _jsx(Badge, { children: "Managed" }), u.hasPin && (_jsxs(Badge, { children: [_jsx(KeyRound, { className: "mr-0.5 inline size-3", "aria-hidden": true }), " PIN"] })), u.id === me.data?.id && _jsx(Badge, { children: "You" })] }), _jsxs("div", { className: "truncate text-xs text-muted", children: [u.isAdmin ? "Full access" : describe(u.restrictions, libraryNames) || "All libraries", u.lastSeenAt && ` · Last active ${new Date(u.lastSeenAt).toLocaleDateString()}`] })] }), _jsx(Button, { size: "sm", variant: "ghost", "aria-label": `Edit ${u.displayName}`, onClick: () => setEditing(u), children: _jsx(Pencil, { className: "size-4" }) }), u.id !== me.data?.id && (_jsx(Button, { size: "sm", variant: "ghost", "aria-label": `Delete ${u.displayName}`, onClick: () => setDeleting(u), children: _jsx(Trash2, { className: "size-4" }) }))] }, u.id))) })] }), adding && _jsx(AddUserDialog, { onClose: () => setAdding(false) }), editing && _jsx(EditUserDialog, { user: editing, isSelf: editing.id === me.data?.id, onClose: () => setEditing(null) }), _jsxs(Dialog, { open: !!deleting, onClose: () => setDeleting(null), title: "Delete user?", footer: _jsxs(_Fragment, { children: [_jsx(Button, { variant: "ghost", onClick: () => setDeleting(null), children: "Cancel" }), _jsx(Button, { variant: "danger", loading: del.isPending, onClick: () => deleting && del.mutate(deleting.id, { onSuccess: () => setDeleting(null) }), children: "Delete user" })] }), children: [_jsxs("p", { className: "text-sm text-muted", children: [_jsx("strong", { className: "text-text", children: deleting?.displayName }), " will be signed out everywhere, and their watch history and playlists will be removed."] }), del.isError && (_jsx("div", { className: "mt-3", children: _jsx(Alert, { tone: "error", children: del.error.message }) }))] })] }));
}
