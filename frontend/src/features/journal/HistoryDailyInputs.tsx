import { DailyInputs } from "@/features/daily/DailyInputs";
import { useDailyInput, useUpdateDailyInput } from "@/features/daily/hooks";

interface Props {
  localDate: string;
}

// HistoryDailyInputs is the past-day flavor of DailyInputs — same UI,
// but writes via PATCH /api/daily/inputs/by-date/:date so the user can
// amend yesterday's mood after the day-start cutoff has rolled over.
export function HistoryDailyInputs({ localDate }: Props) {
  const { data } = useDailyInput(localDate);
  const update = useUpdateDailyInput(localDate);

  return (
    <DailyInputs
      title="Check-in"
      input={data?.input ?? null}
      onSave={(body) => update.mutate(body)}
      isSaving={update.isPending}
    />
  );
}
