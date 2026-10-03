import { useQuery } from "@tanstack/react-query";
import { clsx } from "clsx";
import { useState } from "react";
import { meQuery, useUpdateMe } from "@/api/queries";
import type { SubtitleStyle } from "@/api/types";
import { Alert, Button, Card, Field, Select } from "@/components/ui";
import { defaultSubtitleStyle, resolveSubtitleStyle, subtitleTextStyle, type ResolvedSubtitleStyle } from "@/lib/subtitleStyle";

const sizes: [ResolvedSubtitleStyle["size"], string][] = [
  ["small", "Small"],
  ["medium", "Medium"],
  ["large", "Large"],
  ["huge", "Huge"],
];
const colours: [string, string][] = [
  ["#FFFFFF", "White"],
  ["#FFFF00", "Yellow"],
  ["#00FFFF", "Cyan"],
  ["#00FF00", "Green"],
];

/**
 * Subtitle appearance (PLAY-20): size, colour, background and position, with a live preview.
 * Saved in the person's preferences, so it follows them to every app.
 */
export function SubtitleAppearanceCard() {
  const me = useQuery(meQuery);
  const update = useUpdateMe();
  const [draft, setDraft] = useState<ResolvedSubtitleStyle | null>(null);
  const [saved, setSaved] = useState(false);
  if (!me.data) return null;
  const current = resolveSubtitleStyle(me.data.preferences?.subtitleStyle);
  const s = draft ?? current;
  const set = (patch: Partial<SubtitleStyle>) => {
    setSaved(false);
    setDraft({ ...s, ...patch } as ResolvedSubtitleStyle);
  };
  const custom = !colours.some(([c]) => c === s.color);
  const text = subtitleTextStyle(s);
  return (
    <Card title="Subtitle appearance" description="How text subtitles look on every app. Styled (ASS) subtitles keep their own look.">
      <div
        className="relative flex h-44 w-full items-end justify-center overflow-hidden rounded-lg bg-gradient-to-br from-sky-900 via-slate-700 to-amber-800 text-[22px]"
        data-testid="subtitle-preview"
        aria-label="Subtitle preview"
      >
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_40%,rgba(255,255,255,0.25),transparent_45%)]" aria-hidden />
        <span
          className={clsx("absolute px-1.5 py-0.5 text-center leading-snug", s.position === "raised" ? "bottom-[32%]" : "bottom-[7%]")}
          style={{ color: text.color, fontSize: text.fontSize, background: text.background, textShadow: text.textShadow }}
        >
          This is how your subtitles will look.
        </span>
      </div>
      <div className="grid gap-5 sm:grid-cols-2">
        <Field label="Size">
          {(id) => (
            <div id={id} className="flex rounded-md bg-surface-2 p-0.5 text-sm" role="group" aria-label="Subtitle size">
              {sizes.map(([v, l]) => (
                <button
                  key={v}
                  type="button"
                  aria-pressed={s.size === v}
                  onClick={() => set({ size: v })}
                  className={clsx("flex-1 rounded px-2 py-1.5", s.size === v ? "bg-surface-3 text-text" : "text-muted hover:text-text")}
                >
                  {l}
                </button>
              ))}
            </div>
          )}
        </Field>
        <Field label="Colour">
          {(id) => (
            <div id={id} className="flex items-center gap-2" role="group" aria-label="Subtitle colour">
              {colours.map(([c, l]) => (
                <button
                  key={c}
                  type="button"
                  title={l}
                  aria-label={l}
                  aria-pressed={s.color === c}
                  onClick={() => set({ color: c })}
                  className={clsx("size-8 rounded-full border-2", s.color === c ? "border-accent" : "border-border")}
                  style={{ background: c }}
                />
              ))}
              <label className={clsx("flex h-8 items-center gap-1.5 rounded-full border-2 px-2 text-xs", custom ? "border-accent" : "border-border")}>
                <input type="color" aria-label="Custom colour" value={s.color.toLowerCase()} onChange={(e) => set({ color: e.target.value.toUpperCase() })} className="size-5 cursor-pointer border-0 bg-transparent p-0" />
                Custom
              </label>
            </div>
          )}
        </Field>
        <Field label="Background">
          {(id) => (
            <Select id={id} value={s.background} onChange={(e) => set({ background: e.target.value as ResolvedSubtitleStyle["background"] })}>
              <option value="none">None (drop shadow)</option>
              <option value="outline">Outline</option>
              <option value="translucent">Translucent box</option>
              <option value="opaque">Solid box</option>
            </Select>
          )}
        </Field>
        <Field label="Position">
          {(id) => (
            <Select id={id} value={s.position} onChange={(e) => set({ position: e.target.value as ResolvedSubtitleStyle["position"] })}>
              <option value="bottom">Bottom</option>
              <option value="raised">Raised (above the controls)</option>
            </Select>
          )}
        </Field>
      </div>
      {update.isError && <Alert tone="error">{update.error.message}</Alert>}
      <div className="flex items-center justify-end gap-3">
        {saved && !draft && (
          <span className="text-sm text-success" role="status">
            Subtitle appearance saved.
          </span>
        )}
        <Button variant="ghost" disabled={JSON.stringify(s) === JSON.stringify(defaultSubtitleStyle)} onClick={() => set(defaultSubtitleStyle)}>
          Reset to default
        </Button>
        <Button
          variant="primary"
          disabled={!draft || JSON.stringify(draft) === JSON.stringify(current)}
          loading={update.isPending}
          onClick={() =>
            draft &&
            update.mutate(
              // PATCH /me replaces preferences, so send the rest of them unchanged.
              { preferences: { ...me.data!.preferences, subtitleStyle: draft } },
              {
                onSuccess: () => {
                  setDraft(null);
                  setSaved(true);
                },
              },
            )
          }
        >
          Save appearance
        </Button>
      </div>
    </Card>
  );
}
