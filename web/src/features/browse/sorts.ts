import type { operations } from "@/api/schema.gen";

export type LibrarySort = NonNullable<NonNullable<operations["listLibraryItems"]["parameters"]["query"]>["sort"]>;

/** Library sorts, also usable by smart collections (META-7). */
export const sortOptions: { value: LibrarySort; label: string; video?: boolean }[] = [
  { value: "title", label: "Title" },
  { value: "-added", label: "Date added" },
  { value: "-released", label: "Release date" },
  { value: "-year", label: "Year (newest)" },
  { value: "year", label: "Year (oldest)" },
  { value: "-rating", label: "Rating" },
  { value: "-viewed", label: "Last watched" },
  { value: "-duration", label: "Duration", video: true },
  { value: "random", label: "Random" },
];
