/** In Town's wording and dates (MUSIC-27). A show's date and time are the venue's, whichever zone
 *  the person is in, so they're read as they are rather than converted. */

/** A venue-local date, as "Fri 20 Nov", with the year when it isn't this one. */
export function formatShowDate(localDate: string, today = new Date()): string {
  const [y, m, d] = localDate.split("-").map(Number);
  if (!y || !m || !d) return localDate;
  const at = new Date(y, m - 1, d);
  return at.toLocaleDateString(undefined, {
    weekday: "short",
    day: "numeric",
    month: "short",
    ...(y !== today.getFullYear() && { year: "numeric" }),
  });
}

/** A venue-local "HH:mm" in the person's own time format. */
export function formatShowTime(localTime: string | undefined): string {
  if (!localTime) return "";
  const [h, m] = localTime.split(":").map(Number);
  if (h === undefined || m === undefined || Number.isNaN(h) || Number.isNaN(m)) return localTime;
  return new Date(2000, 0, 1, h, m).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

/** "<Artist> has 3 upcoming shows". */
export function showsHeadline(artist: string, count: number): string {
  return `${artist} has ${count} upcoming ${count === 1 ? "show" : "shows"}`;
}

/** How many of someone's shows are near them, for the artist banner. */
export function nearLine(nearCount: number): string {
  if (nearCount === 0) return "";
  return nearCount === 1 ? "One near you" : `${nearCount} near you`;
}

/** The distance a show is away, in the person's unit. */
export function formatDistance(distance: number | undefined, unit: string | undefined): string {
  if (distance === undefined || !unit) return "";
  const rounded = distance < 10 ? distance.toFixed(1) : String(Math.round(distance));
  return unit === "km" ? `${rounded} km away` : `${rounded} ${rounded === "1.0" ? "mile" : "miles"} away`;
}

/** The countries offered for a location, those Ticketmaster covers most. */
export const inTownCountries = [
  "US", "GB", "CA", "IE", "AU", "NZ", "AT", "BE", "CZ", "DK", "FI", "FR", "DE", "IT", "MX", "NL", "NO", "PL", "PT", "ES", "SE", "CH",
];

/** Their browser's country when it's one of them, else the US. */
export function guessCountry(language = typeof navigator === "undefined" ? "" : navigator.language): string {
  const region = language.split("-")[1]?.toUpperCase();
  return region && inTownCountries.includes(region) ? region : "US";
}

/** Miles where road signs use them. */
export const unitForCountry = (country: string): "miles" | "km" => (country === "GB" || country === "US" ? "miles" : "km");
