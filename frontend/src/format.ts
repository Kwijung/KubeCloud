import type { Language } from "./i18n";

const GiB = 1024 ** 3;

// formatBytes shows sizes in GiB (or MiB below 1 GiB), dropping a trailing ".0".
export function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined || bytes === null || Number.isNaN(bytes)) {
    return "";
  }
  if (bytes < GiB) {
    return `${Math.round(bytes / 1024 ** 2)} MiB`;
  }
  const value = bytes / GiB;
  return `${Number.isInteger(value) ? value : value.toFixed(1).replace(/\.0$/, "")} GiB`;
}

export function formatDateTime(iso: string, language: Language): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toLocaleString(language === "ko" ? "ko-KR" : "en-US", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

export function formatRelative(iso: string, language: Language, now = Date.now()): string {
  const date = new Date(iso).getTime();
  if (Number.isNaN(date)) {
    return "";
  }
  const seconds = Math.round((date - now) / 1000);
  const rtf = new Intl.RelativeTimeFormat(language, { numeric: "auto" });
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ["day", 86400],
    ["hour", 3600],
    ["minute", 60],
  ];
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return rtf.format(Math.round(seconds / size), unit);
    }
  }
  return rtf.format(0, "minute");
}

// fill replaces {name} placeholders in a message.
export function fill(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (match, key: string) => (key in values ? String(values[key]) : match));
}

// GiB input helpers for the flavor form.
export function gibToBytes(value: string): number {
  return Math.round(Number(value) * GiB);
}

export function bytesToGib(bytes: number): string {
  const value = bytes / GiB;
  return Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/0+$/, "").replace(/\.$/, "");
}
