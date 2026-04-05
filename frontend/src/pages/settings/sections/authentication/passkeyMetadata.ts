import type { PasskeyCredential } from "./types";
import {
  formatDateForLocale,
  formatRelativeTimeFromNow,
  getCurrentLocale,
} from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

function getMessages() {
  return getStaticMessages();
}

function formatCreatedDate(dateString: string) {
  const locale = getCurrentLocale();
  const messages = getMessages();
  const timestamp = new Date(dateString);
  if (Number.isNaN(timestamp.getTime())) {
    return messages.settingsAuthentication.unknownDate;
  }

  return formatDateForLocale(locale, dateString, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

function formatRelativeLastUsed(dateString: string | null) {
  const locale = getCurrentLocale();
  const messages = getMessages();
  if (!dateString) {
    return messages.settingsAuthentication.notUsedYet;
  }

  const timestamp = new Date(dateString);
  if (Number.isNaN(timestamp.getTime())) {
    return messages.settingsAuthentication.unknownLastUse;
  }

  return formatRelativeTimeFromNow(dateString, locale);
}

export function getPasskeyStateBadge(passkey: PasskeyCredential) {
  const messages = getMessages();
  if (passkey.backup_state) {
    return {
      label: messages.settingsAuthentication.synced,
      className:
        "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
    };
  }

  if (passkey.backup_eligible === true) {
    return {
      label: messages.settingsAuthentication.backupReady,
      className: "border-sky-500/20 bg-sky-500/10 text-sky-700 dark:text-sky-300",
    };
  }

  if (passkey.backup_eligible === false) {
    return {
      label: messages.settingsAuthentication.deviceBound,
      className: "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300",
    };
  }

  return null;
}

export function buildPasskeyMetadata(passkey: PasskeyCredential) {
  const messages = getMessages();
  const parts = [messages.settingsAuthentication.created(formatCreatedDate(passkey.created_at))];

  if (passkey.last_used_at) {
    parts.push(messages.settingsAuthentication.lastUsed(formatRelativeLastUsed(passkey.last_used_at)));
  } else {
    parts.push(messages.settingsAuthentication.notUsedYet);
  }

  if (passkey.backup_state) {
    parts.push(messages.settingsAuthentication.syncedToAccount);
  } else if (passkey.backup_eligible === true) {
    parts.push(messages.settingsAuthentication.backupCapable);
  } else if (passkey.backup_eligible === false) {
    parts.push(messages.settingsAuthentication.deviceBound);
  }

  return parts.join(" · ");
}
