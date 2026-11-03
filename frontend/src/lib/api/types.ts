export type HealthStatus = 'unknown' | 'ok' | 'unreachable';

export type ApiError = {
  code: string;
  message: string;
  request_id?: string;
};

export type RomSummary = {
  rom_id: string;
  title: string;
  /** Platform key; the value to filter listings by. */
  platform: string;
  /** Platform display name. */
  platform_name: string;
  sha256: string;
  cover_art_url?: string | null;
};

export type RomListResponse = {
  item: RomSummary[];
  next_cursor: string | null;
};

/** Signs in with an email or a username. */
export type LoginRequest = {
  login: string;
  password: string;
};

export type SessionResponse = {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
};

export type UploadState = 'processing' | 'completed' | 'failed';

export type UploadAcceptedResponse = {
  upload_id: string;
  status: UploadState;
};

export type UploadStatusResponse = {
  upload_id: string;
  status: UploadState;
  rom_id: string | null;
  error: string | null;
};

export type RomHash = {
  sha256: string;
  sha1: string;
  md5: string;
  crc32?: string;
};

export type RomDetail = {
  rom_id: string;
  title: string;
  platform: string;
  platform_name: string;
  region: string | null;
  version: string | null;
  publisher?: string | null;
  release_year?: string | null;
  genre?: string | null;
  description?: string | null;
  cover_art_url?: string | null;
  title_screen_url?: string | null;
  screenshot_url?: string | null;
  hash: RomHash;
};

/**
 * Replaces a ROM's metadata. A null region or version clears it; publisher, release year, genre,
 * and description are kept when omitted and cleared when empty.
 */
export type UpdateRomRequest = {
  title: string;
  platform: string;
  region: string | null;
  version: string | null;
  publisher?: string;
  release_year?: string;
  genre?: string;
  description?: string;
};

/** A short-lived URL the browser can open to stream a ROM download without a bearer token. */
export type DownloadLink = {
  url: string;
  filename: string;
  expiresIn: number;
};

export type DatabaseDriver = 'sqlite' | 'mysql';

export type MySQLConnection = {
  host: string;
  port: number;
  user: string;
  password: string;
  database: string;
};

export type SetupLockedField = 'db_driver' | 'sqlite_path' | 'mysql' | 'storage_root';

export type SetupStatus = {
  required: boolean;
  defaults?: {
    db_driver: DatabaseDriver;
    sqlite_path: string;
    mysql: MySQLConnection;
    storage_root: string;
  };
  locked?: SetupLockedField[];
};

export type SetupRequest = {
  db_driver: DatabaseDriver;
  sqlite_path: string;
  mysql: MySQLConnection;
  storage_root: string;
  admin_email: string;
  admin_password: string;
};

/** A platform present in the library. */
export type Platform = {
  /** The value to filter ROM listings by. */
  key: string;
  name: string;
  rom_count: number;
};

export type UserRole = 'admin' | 'user';

export type Profile = {
  user_id: string;
  email: string;
  /** Optional sign-in name; null when the user signs in by email only. */
  username: string | null;
  role: UserRole | string;
  /** The chosen interface language; null follows the browser. */
  locale: string | null;
  /** The chosen interface font; null uses the application default. */
  font: UIFont | null;
};

/**
 * Changes the email, password, and/or language. Email and password changes need the current
 * password; an empty locale goes back to following the browser.
 */
export type UpdateProfileRequest = {
  email?: string;
  /** An empty string removes the username. */
  username?: string;
  current_password?: string;
  new_password?: string;
  locale?: string;
  /** An interface font, or an empty string for the application default. */
  font?: UIFont | '';
};

export type UpdateProfileResponse = {
  profile: Profile;
  /** Present after a password change, which ends every other session. */
  session: SessionResponse | null;
};

export type SettingSource = 'default' | 'config_file' | 'environment';

/** The interface font: the pixel font, Ubuntu, or the operating system's font. */
export type UIFont = 'pixel' | 'ubuntu' | 'system';

export type Appearance = {
  font: UIFont;
};

export type SettingOption = {
  value: string;
  label: string;
};

export type AppSetting = {
  key: string;
  label: string;
  description: string;
  group: string;
  value: string;
  /** A saved value that applies after the next restart. */
  pending_value: string | null;
  source: SettingSource;
  editable: boolean;
  /** Live settings apply as soon as they are saved. */
  live: boolean;
  /** Allowed values, for settings with a fixed set of choices. */
  options?: SettingOption[];
};

export type SettingsResponse = {
  item: AppSetting[];
  can_edit: boolean;
  restart_required: boolean;
};

/** An account as administrators see it, with what it holds. */
export type ManagedUser = {
  id: string;
  email: string;
  username: string | null;
  role: UserRole;
  rom_count: number;
  storage_bytes: number;
  upload_count: number;
  active_sessions: number;
  platforms: Platform[];
  /** The signed-in administrator, who cannot delete or demote themselves. */
  is_self: boolean;
};

export type CreateUserRequest = {
  email: string;
  username?: string;
  password: string;
  role: UserRole;
};

/** Changes only the fields present; a password resets it and signs the user out everywhere. */
export type UpdateUserRequest = {
  email?: string;
  username?: string;
  role?: UserRole;
  password?: string;
};

export type DeleteUserResponse = {
  transferred_roms: number;
  transferred_libraries: number;
  transferred_uploads: number;
};
