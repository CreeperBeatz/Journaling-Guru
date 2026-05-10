// Shared weekday labels keyed by ISO/Postgres dow (0=Sunday..6=Saturday).
// Same ordering as users.reflection_weekday so the index *is* the value.
export const WEEKDAY_LABELS = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
] as const;

export type Weekday = (typeof WEEKDAY_LABELS)[number];
