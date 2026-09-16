export function configuredRightsEmail(): string | null {
  const value = process.env.RIGHTS_CONTACT_EMAIL?.trim();
  return value && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value) && !value.toLowerCase().endsWith(".invalid") ? value : null;
}
