import { getStaticMessages } from "@/i18n/staticMessages";

export const AUTH_PASSWORD_MIN_LENGTH = 8;
export const AUTH_PASSWORD_MAX_LENGTH = 512;

export const validateAuthPassword = (value: string): string | null => {
  const messages = getStaticMessages();
  if (!value) {
    return null;
  }
  if (value.length < AUTH_PASSWORD_MIN_LENGTH) {
    return messages.settingsAuth.passwordMinLength(AUTH_PASSWORD_MIN_LENGTH);
  }
  if (value.length > AUTH_PASSWORD_MAX_LENGTH) {
    return messages.settingsAuth.passwordMaxLength(AUTH_PASSWORD_MAX_LENGTH);
  }
  return null;
};
