export type HealthStatus = 'unknown' | 'ok' | 'unreachable';

export type ApiError = {
  code: string;
  message: string;
  request_id?: string;
};

export type RomSummary = {
  rom_id: string;
  title: string;
  platform: string;
  sha256: string;
  cover_art_url?: string | null;
};

export type RomListResponse = {
  item: RomSummary[];
  next_cursor: string | null;
};

export type LoginRequest = {
  email: string;
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
  region: string | null;
  version: string | null;
  cover_art_url?: string | null;
  hash: RomHash;
};

export type UpdateRomRequest = {
  title: string;
  platform: string;
  region: string | null;
  version: string | null;
};

export type DownloadedRom = {
  blob: Blob;
  filename: string;
  contentType: string;
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
