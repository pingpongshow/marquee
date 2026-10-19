import { useState } from "react";
import type { UserRestrictions } from "@/api/types";
import { Field, Input, Toggle } from "@/components/ui";
import { formatAllowance, parseAllowance } from "./timeLimits";

/** An allowance typed as minutes or h:mm; empty means none. */
function AllowanceInput({
  label,
  help,
  value,
  emptyLabel,
  onChange,
}: {
  label: string;
  help: string;
  value: number | null | undefined;
  emptyLabel: string;
  onChange: (minutes: number | null) => void;
}) {
  // What's typed is kept while it's being typed; a valid entry applies at once.
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? (value ? formatAllowance(value) : "");
  const parsed = parseAllowance(shown);
  return (
    <Field
      label={label}
      help={help}
      error={parsed === undefined ? "Enter minutes (90) or hours and minutes (1:30), up to 24 hours." : undefined}
    >
      {(id) => (
        <Input
          id={id}
          className="w-40"
          inputMode="numeric"
          placeholder={emptyLabel}
          value={shown}
          onChange={(e) => {
            setText(e.target.value);
            const p = parseAllowance(e.target.value);
            if (p !== undefined) onChange(p);
          }}
          onBlur={() => parseAllowance(shown) !== undefined && setText(null)}
        />
      )}
    </Field>
  );
}

/** Time limits for a kid's (or any non-admin's) profile: a daily allowance of video, a
 * different one at weekends, and viewing hours. All off by default. */
export function TimeLimitsEditor({
  value,
  onChange,
}: {
  value: UserRestrictions;
  onChange: (r: UserRestrictions) => void;
}) {
  const weekendOn = value.weekendLimitMinutes != null;
  const hoursOn = !!(value.allowedFrom || value.allowedUntil);
  return (
    <fieldset className="space-y-4 rounded-lg border border-border p-4">
      <legend className="px-1 text-sm font-semibold text-text">Time limits</legend>
      <p className="-mt-1 text-sm text-muted">
        Off unless set. Counts movies, shows, home videos, on demand and Live TV, on the
        server's clock; music doesn't count. When time's up, video stops with a message.
      </p>
      <AllowanceInput
        label="Daily limit"
        help="Minutes of video a day, e.g. 90 or 1:30. Empty = no limit."
        emptyLabel="No limit"
        value={value.dailyLimitMinutes}
        onChange={(m) => onChange({ ...value, dailyLimitMinutes: m })}
      />
      <Toggle
        label="Different limit at weekends"
        help="Saturdays and Sundays."
        checked={weekendOn}
        onChange={(on) => {
          const next = { ...value };
          if (on) next.weekendLimitMinutes = value.dailyLimitMinutes || 120;
          else delete next.weekendLimitMinutes;
          onChange(next);
        }}
      />
      {weekendOn && (
        <AllowanceInput
          label="Weekend limit"
          help="Empty = no limit at weekends."
          emptyLabel="No limit"
          value={value.weekendLimitMinutes}
          onChange={(m) => onChange({ ...value, weekendLimitMinutes: m ?? 0 })}
        />
      )}
      <Toggle
        label="Viewing hours"
        help="Video only plays between these times. They may cross midnight."
        checked={hoursOn}
        onChange={(on) => {
          const next = { ...value };
          if (on) {
            next.allowedFrom = "07:00";
            next.allowedUntil = "20:00";
          } else {
            delete next.allowedFrom;
            delete next.allowedUntil;
          }
          onChange(next);
        }}
      />
      {hoursOn && (
        <div className="flex flex-wrap items-end gap-3">
          <Field label="From">
            {(id) => (
              <Input
                id={id}
                type="time"
                className="w-36"
                value={value.allowedFrom ?? ""}
                onChange={(e) => onChange({ ...value, allowedFrom: e.target.value || undefined })}
              />
            )}
          </Field>
          <Field label="Until">
            {(id) => (
              <Input
                id={id}
                type="time"
                className="w-36"
                value={value.allowedUntil ?? ""}
                onChange={(e) => onChange({ ...value, allowedUntil: e.target.value || undefined })}
              />
            )}
          </Field>
        </div>
      )}
    </fieldset>
  );
}
