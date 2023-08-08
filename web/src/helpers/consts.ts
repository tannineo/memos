// UNKNOWN_ID is the symbol for unknown id
export const UNKNOWN_ID = -1;
export const UNKNOWN_USERNAME = "";

// default animation duration
export const ANIMATION_DURATION = 200;

// millisecond in a day
export const DAILY_TIMESTAMP = 3600 * 24 * 1000;

export const VISIBILITY_SELECTOR_ITEMS = [
  { text: "PRIVATE", value: "PRIVATE" },
  { text: "PROTECTED", value: "PROTECTED" },
  { text: "PUBLIC", value: "PUBLIC" },
] as const;

// space width for tab action in editor
export const TAB_SPACE_WIDTH = 2;

// default fetch memo amount
export const DEFAULT_MEMO_LIMIT = 20;

export const SYSTEM_SETTINGS = {
  DISABLE_PASSWORD_LOGIN: "disable-password-login",
  CUSTOMIZED_PROFILE: "customized-profile",
  LOCAL_STORAGE_PATH: "local-storage-path",
  STORAGE_SERVICE_ID: "storage-service-id",
  ALLOW_SIGNUP: "allow-signup",
  TELEGRAM_BOT_TOKEN: "telegram-bot-token",
  ADDITIONAL_STYLE: "additional-style",
  ADDITIONAL_SCRIPT: "additional-script",
  DISABLE_PUBLIC_MEMOS: "disable-public-memos",
  MEMO_DISPLAY_WITH_UPDATED_TS: "memo-display-with-updated-ts",
  MAX_UPLOAD_SIZE_MIB: "max-upload-size-mib",
  AUTO_BACKUP_INTERVAL: "auto-backup-interval",
};

export interface SystemSetting {
  name: typeof SYSTEM_SETTINGS[keyof typeof SYSTEM_SETTINGS];
  value: string;
}
