/**
 * Money on the wire is a decimal string of micro-dollars ("1.500000"). It is parsed and formatted
 * with BigInt only: no float ever touches an amount (ruling S14). `microToNumber` exists for chart
 * coordinates alone; tooltip and label text always come from the string.
 */
const MICRO_PER_USD = 1_000_000n;
const CENT = 10_000n;
const DECIMAL = /^(-?)(\d+)(?:\.(\d{1,6}))?$/;

/** "1.5" or "-0.000250" to micro-dollars; null for anything that is not a plain decimal with at most 6 places. */
export function tryParseMicro(s: string): bigint | null {
  const m = DECIMAL.exec(s);
  if (!m) return null;
  const micro = BigInt(m[2] ?? "0") * MICRO_PER_USD + BigInt((m[3] ?? "").padEnd(6, "0"));
  return m[1] === "-" ? -micro : micro;
}

/** Like tryParseMicro, but a malformed amount is a programming error worth failing loudly on. */
export function parseMicro(s: string): bigint {
  const v = tryParseMicro(s);
  if (v === null) throw new Error(`not a money amount: ${JSON.stringify(s)}`);
  return v;
}

const group = (digits: string) => digits.replace(/\B(?=(\d{3})+(?!\d))/g, ",");

/**
 * "$1.00": whole cents, half away from zero. An amount under one cent keeps up to six places
 * ("$0.0015") so a real saving never reads as "$0.00".
 */
export function formatUsd(s: string): string {
  const micro = parseMicro(s);
  const neg = micro < 0n;
  const abs = neg ? -micro : micro;
  const sign = neg ? "-" : "";
  if (abs !== 0n && abs < CENT) {
    const frac = (abs % MICRO_PER_USD).toString().padStart(6, "0").replace(/0+$/, "");
    return `${sign}$0.${frac.padEnd(4, "0")}`;
  }
  const cents = (abs + CENT / 2n) / CENT;
  return `${sign}$${group((cents / 100n).toString())}.${(cents % 100n).toString().padStart(2, "0")}`;
}

/** Whether the amount is exactly zero. */
export function isZeroMicro(s: string): boolean {
  return parseMicro(s) === 0n;
}

/** Whether the amount is above zero. */
export function isPositiveMicro(s: string): boolean {
  return parseMicro(s) > 0n;
}

/** `part` as a whole-number share of `whole` (0-100), integer math; 0 when `whole` is not positive. */
export function sharePct(part: string, whole: string): number {
  const w = parseMicro(whole);
  if (w <= 0n) return 0;
  const p = parseMicro(part);
  return p <= 0n ? 0 : Number((p * 100n) / w);
}

/** The larger of the amounts, as a decimal string ("0.000000" for an empty list). */
export function maxMicro(values: readonly string[]): string {
  let m = 0n;
  for (const v of values) {
    const x = parseMicro(v);
    if (x > m) m = x;
  }
  return `${m / MICRO_PER_USD}.${(m % MICRO_PER_USD).toString().padStart(6, "0")}`;
}

/** Basis points (0-10000) as "10%" or "12.34%"; "n/a" for null. Integer math, no float. */
export function formatBp(bp: number | null): string {
  if (bp === null) return "n/a";
  const whole = Math.trunc(bp / 100);
  const frac = String(bp % 100)
    .padStart(2, "0")
    .replace(/0+$/, "");
  return `${whole}${frac ? `.${frac}` : ""}%`;
}

/** 120000 as "120,000". */
export function formatTokens(n: number): string {
  return group(String(Math.trunc(n)));
}

/** For chart coordinates only. */
export function microToNumber(s: string): number {
  return Number(parseMicro(s)) / Number(MICRO_PER_USD);
}
