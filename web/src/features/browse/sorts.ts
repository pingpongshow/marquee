import type { operations } from "@/api/schema.gen";

export type LibrarySort = NonNullable<NonNullable<operations["listLibraryItems"]["parameters"]["query"]>["sort"]>;

/** Library sorts, also usable by smart collections (META-7). */
/** `personal` sorts (the person's own rating) are for library grids only, not smart collections. */
export const sortOptions: { value: LibrarySort; label: string; video?: boolean; personal?: boolean }[] = [
  { value: "title", label: "Title" },
  { value: "-added", label: "Date added" },
  { value: "-released", label: "Release date" },
  { value: "-year", label: "Year (newest)" },
  { value: "year", label: "Year (oldest)" },
  { value: "-rating", label: "Rating" },
  { value: "-myRating", label: "My rating", personal: true },
  { value: "-viewed", label: "Last watched" },
  { value: "-duration", label: "Duration", video: true },
  { value: "random", label: "Random" },
];
