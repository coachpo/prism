export const PRICING_MINUTES_PER_DAY = 1_440;

export function pricingMinuteToTime(value: number): string {
  if (!Number.isInteger(value) || value < 0) return "";
  const minute = value % PRICING_MINUTES_PER_DAY;
  return `${String(Math.floor(minute / 60)).padStart(2, "0")}:${String(minute % 60).padStart(2, "0")}`;
}

export function pricingTimeToMinute(value: string): number {
  const match = /^(\d{2}):(\d{2})$/.exec(value);
  if (!match) return -1;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (hour > 23 || minute > 59) return -1;
  return hour * 60 + minute;
}

export function pricingWindowEndMinute(time: string, nextDay: boolean): number {
  const minute = pricingTimeToMinute(time);
  if (minute < 0) return -1;
  return minute + (nextDay ? PRICING_MINUTES_PER_DAY : 0);
}

export function togglePricingWeekday(mask: number, bit: number, enabled: boolean): number {
  const flag = 1 << bit;
  return enabled ? mask | flag : mask & ~flag;
}
