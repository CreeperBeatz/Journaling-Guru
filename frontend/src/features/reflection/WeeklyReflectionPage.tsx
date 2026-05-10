import { WeeklyReflection } from "./WeeklyReflection";

// /weekly — standalone host for WeeklyReflection. The wizard owns its
// own header/states, so this page is a thin wrapper.
export function WeeklyReflectionPage() {
  return <WeeklyReflection />;
}
