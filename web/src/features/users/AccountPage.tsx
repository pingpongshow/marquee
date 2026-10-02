import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { ListenBrainzCard } from "./ListenBrainz";
import { TwoFactorCard } from "./TwoFactor";
import { meQuery, useUpdateMe } from "@/api/queries";
import type { UserPreferences } from "@/api/types";
import {
  Alert,
  Button,
  Card,
  Field,
  Input,
  Select,
  Spinner,
} from "@/components/ui";
import { StatsView } from "../settings/StatsView";
import { AvatarPicker } from "./Avatar";
import { LinkDeviceCard } from "./LinkDevice";
import {
  languageOptions,
  localQualityOptions,
  remoteQualityOptions,
} from "./constants";

/** Per-user account and playback preferences (any user). */
export function AccountPage() {
  const me = useQuery(meQuery);
  const update = useUpdateMe();
  const [name, setName] = useState<string | null>(null);
  const [cur, setCur] = useState("");
  const [next, setNext] = useState("");
  const [pin, setPin] = useState("");
  const [prefs, setPrefs] = useState<UserPreferences | null>(null);
  const [saved, setSaved] = useState<string | null>(null);
  if (!me.data) return <Spinner />;
  const u = me.data;
  const p = prefs ?? u.preferences;
  const done = (msg: string) => () => {
    setSaved(msg);
    setCur("");
    setNext("");
    setPin("");
  };

  return (
    <div className="mx-auto max-w-2xl space-y-6 p-6 lg:p-8">
      <h1 className="text-2xl font-bold">Account</h1>
      {saved && <Alert tone="success">{saved}</Alert>}
      {update.isError && <Alert tone="error">{update.error.message}</Alert>}

      <Card title="Profile">
        <AvatarPicker user={u} />
        <Field label="Display name">
          {(id) => (
            <Input
              id={id}
              maxLength={64}
              value={name ?? u.displayName}
              onChange={(e) => setName(e.target.value)}
            />
          )}
        </Field>
        <div className="flex justify-end">
          <Button
            variant="primary"
            disabled={!name || name === u.displayName}
            loading={update.isPending}
            onClick={() =>
              update.mutate(
                { displayName: name ?? undefined },
                { onSuccess: done("Profile saved.") },
              )
            }
          >
            Save
          </Button>
        </div>
      </Card>

      <LinkDeviceCard />

      {!u.isManaged && (
        <Card
          title="Password"
          description={
            u.hasPassword
              ? undefined
              : "You don't have a password. Add one to sign in from outside the home network or on devices without PIN sign-in."
          }
        >
          <div className="grid gap-5 sm:grid-cols-2">
            {u.hasPassword && (
              <Field label="Current password">
                {(id) => (
                  <Input
                    id={id}
                    type="password"
                    autoComplete="current-password"
                    value={cur}
                    onChange={(e) => setCur(e.target.value)}
                  />
                )}
              </Field>
            )}
            <Field label="New password" help="At least 8 characters.">
              {(id) => (
                <Input
                  id={id}
                  type="password"
                  autoComplete="new-password"
                  value={next}
                  onChange={(e) => setNext(e.target.value)}
                />
              )}
            </Field>
          </div>
          <div className="flex justify-end">
            <Button
              variant="primary"
              disabled={(u.hasPassword && !cur) || next.length < 8}
              loading={update.isPending}
              onClick={() =>
                update.mutate(
                  { currentPassword: cur || undefined, newPassword: next },
                  { onSuccess: done("Password saved.") },
                )
              }
            >
              {u.hasPassword ? "Change password" : "Set password"}
            </Button>
          </div>
        </Card>
      )}

      {u.hasPassword && <TwoFactorCard />}
      <ListenBrainzCard />

      {!u.isManaged && (
        <Card
          title="Profile PIN"
          description="Asked when someone switches to your profile on a shared device."
        >
          <Field label={u.hasPin ? "New PIN" : "PIN"} help="4 digits.">
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
          <div className="flex justify-end gap-2">
            {u.hasPin && (
              <Button
                variant="ghost"
                onClick={() =>
                  update.mutate(
                    { pin: "" },
                    { onSuccess: done("PIN removed.") },
                  )
                }
              >
                Remove PIN
              </Button>
            )}
            <Button
              variant="primary"
              disabled={pin.length !== 4}
              loading={update.isPending}
              onClick={() =>
                update.mutate({ pin }, { onSuccess: done("PIN saved.") })
              }
            >
              Save PIN
            </Button>
          </div>
        </Card>
      )}

      <Card
        title="Playback"
        description="Defaults for every app you sign in to."
      >
        <div className="grid gap-5 sm:grid-cols-2">
          <Field label="Preferred audio language">
            {(id) => (
              <Select
                id={id}
                value={p.audioLanguage ?? ""}
                onChange={(e) =>
                  setPrefs({ ...p, audioLanguage: e.target.value || undefined })
                }
              >
                {languageOptions.map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Preferred subtitle language">
            {(id) => (
              <Select
                id={id}
                value={p.subtitleLanguage ?? ""}
                onChange={(e) =>
                  setPrefs({
                    ...p,
                    subtitleLanguage: e.target.value || undefined,
                  })
                }
              >
                {languageOptions.map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="Subtitles">
            {(id) => (
              <Select
                id={id}
                value={p.subtitleMode ?? "foreign"}
                onChange={(e) =>
                  setPrefs({
                    ...p,
                    subtitleMode: e.target
                      .value as UserPreferences["subtitleMode"],
                  })
                }
              >
                <option value="off">Off</option>
                <option value="forced">Forced only</option>
                <option value="foreign">
                  When audio is in another language
                </option>
                <option value="always">Always</option>
              </Select>
            )}
          </Field>
          <div />
          <Field label="Quality at home">
            {(id) => (
              <Select
                id={id}
                value={p.localQualityKbps ?? 0}
                onChange={(e) =>
                  setPrefs({ ...p, localQualityKbps: Number(e.target.value) })
                }
              >
                {localQualityOptions.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field
            label="Quality away from home"
            help="Automatic adapts to your connection."
          >
            {(id) => (
              <Select
                id={id}
                value={p.remoteQualityKbps ?? 0}
                onChange={(e) =>
                  setPrefs({ ...p, remoteQualityKbps: Number(e.target.value) })
                }
              >
                {remoteQualityOptions.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.value === 0 ? "Automatic" : o.label}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        </div>
        <div className="flex justify-end">
          <Button
            variant="primary"
            disabled={!prefs}
            loading={update.isPending}
            onClick={() =>
              prefs &&
              update.mutate(
                { preferences: prefs },
                {
                  onSuccess: () => (
                    setPrefs(null),
                    setSaved("Playback preferences saved.")
                  ),
                },
              )
            }
          >
            Save preferences
          </Button>
        </div>
      </Card>
      <Card
        title="Your stats"
        description="What you've watched and listened to."
      >
        <StatsView admin={false} />
      </Card>
    </div>
  );
}
