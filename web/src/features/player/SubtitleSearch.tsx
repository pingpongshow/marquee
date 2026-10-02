import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BadgeCheck, Download, Ear } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { Alert, Button, Dialog, Select, Spinner } from "@/components/ui";

const languages: [string, string][] = [
  ["en", "English"], ["es", "Spanish"], ["fr", "French"], ["de", "German"], ["it", "Italian"], ["pt-BR", "Portuguese (Brazil)"],
  ["pt-PT", "Portuguese"], ["nl", "Dutch"], ["sv", "Swedish"], ["no", "Norwegian"], ["da", "Danish"], ["fi", "Finnish"],
  ["pl", "Polish"], ["ru", "Russian"], ["uk", "Ukrainian"], ["el", "Greek"], ["tr", "Turkish"], ["ar", "Arabic"], ["he", "Hebrew"],
  ["hi", "Hindi"], ["ja", "Japanese"], ["ko", "Korean"], ["zh-CN", "Chinese (simplified)"], ["zh-TW", "Chinese (traditional)"],
];

// User preferences store three-letter codes; OpenSubtitles wants two.
const fromPref: Record<string, string> = { eng: "en", spa: "es", fre: "fr", fra: "fr", ger: "de", deu: "de", ita: "it", por: "pt-PT", dut: "nl", nld: "nl", swe: "sv", jpn: "ja", kor: "ko", chi: "zh-CN", zho: "zh-CN", rus: "ru" };

/** Searches OpenSubtitles for the playing item and adds the chosen file (PLAY-7). */
export function SubtitleSearchDialog({ itemId, preferred, onClose, onAdded }: { itemId: number; preferred?: string; onClose: () => void; onAdded: (streamId: number) => void }) {
  const qc = useQueryClient();
  const [lang, setLang] = useState(fromPref[preferred ?? ""] ?? "en");
  const results = useQuery({
    queryKey: ["subtitles", itemId, lang],
    queryFn: () => unwrap(api.GET("/items/{itemId}/subtitles/search", { params: { path: { itemId }, query: { languages: lang } } })),
    retry: false,
  });
  const download = useMutation({
    mutationFn: (r: NonNullable<typeof results.data>[number]) =>
      unwrap(api.POST("/items/{itemId}/subtitles", { params: { path: { itemId } }, body: { fileId: r.fileId, language: r.language, release: r.release, hearingImpaired: r.hearingImpaired } })),
    onSuccess: async (res) => {
      await qc.invalidateQueries({ queryKey: ["items", itemId] });
      onAdded(res.streamId);
    },
  });
  return (
    <Dialog open onClose={onClose} title="Find subtitles">
      <div className="space-y-4">
        <Select aria-label="Language" value={lang} onChange={(e) => setLang(e.target.value)}>
          {languages.map(([code, name]) => (
            <option key={code} value={code}>
              {name}
            </option>
          ))}
        </Select>
        {results.isPending && <Spinner />}
        {results.isError && <Alert tone="error">{results.error.message}</Alert>}
        {download.isError && <Alert tone="error">{download.error.message}</Alert>}
        {results.data?.length === 0 && <p className="text-sm text-muted">No subtitles found in this language.</p>}
        <ul className="max-h-96 space-y-1 overflow-y-auto">
          {results.data?.map((r) => (
            <li key={r.fileId}>
              <button
                onClick={() => download.mutate(r)}
                disabled={download.isPending}
                className="flex w-full items-center gap-3 rounded-md p-2 text-left hover:bg-surface-2 disabled:opacity-50"
              >
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium" title={r.release}>
                    {r.release || r.fileName}
                  </span>
                  <span className="flex flex-wrap items-center gap-x-3 text-xs text-muted">
                    <span>{r.downloads.toLocaleString()} downloads</span>
                    {r.hashMatch && (
                      <span className="flex items-center gap-1 text-success">
                        <BadgeCheck className="size-3.5" /> Made for this file
                      </span>
                    )}
                    {r.hearingImpaired && (
                      <span className="flex items-center gap-1">
                        <Ear className="size-3.5" /> SDH
                      </span>
                    )}
                    {r.aiTranslated && <span className="text-accent">Machine translated</span>}
                    {r.foreignPartsOnly && <span>Foreign parts only</span>}
                  </span>
                </span>
                {download.isPending && download.variables?.fileId === r.fileId ? <Spinner /> : <Download className="size-4 text-muted" aria-hidden />}
              </button>
            </li>
          ))}
        </ul>
        <div className="flex justify-end">
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
