import { useParams } from "@tanstack/react-router";
import { Construction } from "lucide-react";
import { findSection } from "./sections";

export function SettingsSectionPage() {
  const { section } = useParams({ from: "/settings/$section" });
  const s = findSection(section);
  if (!s) return <p className="text-muted">Unknown settings section.</p>;
  const Body = s.component;
  return (
    <div className="mx-auto max-w-3xl pb-8">
      <h1 className="text-2xl font-bold">{s.label}</h1>
      <p className="mt-1 mb-6 text-muted">{s.description}</p>
      {Body ? (
        <Body />
      ) : (
        <div className="flex items-center gap-3 rounded-lg border border-dashed border-border p-6 text-muted">
          <Construction className="size-5" aria-hidden />
          This section arrives in milestone {s.milestone}. See docs/05-roadmap.md.
        </div>
      )}
    </div>
  );
}
