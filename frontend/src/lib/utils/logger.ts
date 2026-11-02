type LogLevel = 'debug' | 'info' | 'warn' | 'error';

const LOG_LEVELS: Record<LogLevel, number> = {
	debug: 0,
	info: 1,
	warn: 2,
	error: 3
};

/**
 * Get enabled log level from environment or localStorage.
 * Defaults to 'warn' in production, 'debug' in development.
 */
function getEnabledLogLevel(): LogLevel {
	if (typeof window === 'undefined') {
		return 'warn';
	}

	const stored = localStorage.getItem('retroxide.log-level');
	if (stored && stored in LOG_LEVELS) {
		return stored as LogLevel;
	}

	return import.meta.env.DEV ? 'debug' : 'warn';
}

/**
 * Simple logger utility for debugging frontend issues.
 * Control verbosity via localStorage: localStorage.setItem('retroxide.log-level', 'debug')
 */
export const logger = {
	debug(msg: string, data?: unknown) {
		this._log('debug', msg, data);
	},

	info(msg: string, data?: unknown) {
		this._log('info', msg, data);
	},

	warn(msg: string, data?: unknown) {
		this._log('warn', msg, data);
	},

	error(msg: string, data?: unknown) {
		this._log('error', msg, data);
	},

	_log(level: LogLevel, msg: string, data?: unknown) {
		const enabledLevel = getEnabledLogLevel();
		if (LOG_LEVELS[level] < LOG_LEVELS[enabledLevel]) {
			return;
		}

		const timestamp = new Date().toISOString().split('T')[1];
		const prefix = `[${timestamp}] [${level.toUpperCase()}]`;

		if (data !== undefined) {
			console[level === 'debug' || level === 'info' ? 'log' : level](prefix, msg, data);
		} else {
			console[level === 'debug' || level === 'info' ? 'log' : level](prefix, msg);
		}
	}
};
