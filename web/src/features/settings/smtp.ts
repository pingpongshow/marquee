export type SmtpSecurity = "none" | "starttls" | "tls";

const usualPorts: Record<SmtpSecurity, number> = { none: 25, starttls: 587, tls: 465 };

/**
 * The port to show after choosing a security setting: the usual one for it, unless the
 * current port is an unusual one someone chose on purpose.
 */
export function portForSecurity(security: SmtpSecurity, current: number | undefined): number {
  const usual = current === undefined || current === 0 || Object.values(usualPorts).includes(current);
  return usual ? usualPorts[security] : current;
}
