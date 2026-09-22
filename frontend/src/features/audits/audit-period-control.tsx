import { format } from "date-fns";
import { enUS, zhCN } from "date-fns/locale";
import { CalendarRange } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { DateTimePicker } from "@/shared/components/date-time-picker";
import { PeriodSelector } from "@/shared/components/period-selector";
import { toDateTimeLocal } from "@/shared/lib/format";
import type { PeriodDays } from "@/shared/lib/period";

import { inclusiveRangeFromLocal, isValidCustomRange, type AuditCustomRange } from "./audit-range";

export function AuditPeriodControl({
  periodDays,
  customRange,
  onPeriodDaysChange,
  onCustomRangeChange,
}: {
  periodDays: PeriodDays;
  customRange: AuditCustomRange | null;
  onPeriodDaysChange: (value: PeriodDays) => void;
  onCustomRangeChange: (value: AuditCustomRange | null) => void;
}) {
  const { t, i18n } = useTranslation();
  const isChinese = i18n.language.toLowerCase().startsWith("zh");
  const [open, setOpen] = useState(false);
  const [draftStart, setDraftStart] = useState("");
  const [draftEnd, setDraftEnd] = useState("");
  const [error, setError] = useState("");

  function openEditor(nextOpen: boolean): void {
    if (nextOpen) {
      const start = customRange?.start ?? new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString();
      const end = customRange?.end ?? new Date().toISOString();
      setDraftStart(toDateTimeLocal(start));
      setDraftEnd(toDateTimeLocal(end));
      setError("");
    }
    setOpen(nextOpen);
  }

  function apply(): void {
    const range = inclusiveRangeFromLocal(draftStart, draftEnd);
    if (!range || !isValidCustomRange(range.start, range.end)) {
      setError(t("audits.customRangeInvalid"));
      return;
    }
    onCustomRangeChange(range);
    setOpen(false);
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <PeriodSelector
        value={customRange ? null : periodDays}
        onChange={(value) => {
          onCustomRangeChange(null);
          onPeriodDaysChange(value);
        }}
        ariaLabel={t("audits.usageSummary")}
      />
      <Popover open={open} onOpenChange={openEditor}>
        <PopoverTrigger asChild>
          <Button
            type="button"
            variant={customRange ? "default" : "secondary"}
            size="sm"
            className="h-8 max-w-[18rem] gap-1.5 px-2.5"
            aria-label={t("audits.customRange")}
          >
            <CalendarRange className="size-3.5" />
            <span className="truncate font-normal">{customRange ? formatRangeLabel(customRange, isChinese) : t("audits.customRange")}</span>
          </Button>
        </PopoverTrigger>
        <PopoverContent align="end" className="w-80 space-y-3 p-3">
          <div className="space-y-1.5">
            <p className="text-xs text-muted-foreground">{t("audits.customRangeStart")}</p>
            <DateTimePicker value={draftStart} onChange={setDraftStart} placeholder={t("audits.customRangeStart")} timeLabel={t("audits.customRangeTime")} />
          </div>
          <div className="space-y-1.5">
            <p className="text-xs text-muted-foreground">{t("audits.customRangeEnd")}</p>
            <DateTimePicker value={draftEnd} onChange={setDraftEnd} placeholder={t("audits.customRangeEnd")} timeLabel={t("audits.customRangeTime")} />
          </div>
          {error ? <p className="text-xs text-destructive">{error}</p> : null}
          <div className="flex justify-end gap-2">
            {customRange ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  onCustomRangeChange(null);
                  setOpen(false);
                }}
              >
                {t("audits.customRangeClear")}
              </Button>
            ) : null}
            <Button type="button" size="sm" onClick={apply}>{t("audits.customRangeApply")}</Button>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}

function formatRangeLabel(range: AuditCustomRange, isChinese: boolean): string {
  const start = new Date(range.start);
  const end = new Date(range.end);
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime())) {
    return "";
  }
  const pattern = isChinese ? "M月d日 HH:mm" : "MMM d HH:mm";
  const locale = isChinese ? zhCN : enUS;
  return `${format(start, pattern, { locale })} – ${format(end, pattern, { locale })}`;
}
