'use strict';

const fs = require('fs');
const path = require('path');

const SCHEMA_VERSION = '1';

/**
 * SQLite cache for Garmin health data using sql.js (WASM).
 * Two tables: daily_cache (keyed by date+endpoint) and activity_cache (keyed by activity_id+data_type).
 */
class GarminCache {
  constructor(db, dbPath) {
    this._db = db;
    this._dbPath = dbPath;
  }

  // ---- Daily cache ----

  getCachedDaily(date, endpointName) {
    const stmt = this._db.prepare(
      'SELECT json_blob FROM daily_cache WHERE date = ? AND endpoint = ?'
    );
    stmt.bind([date, endpointName]);
    let result = null;
    if (stmt.step()) {
      result = JSON.parse(stmt.getAsObject().json_blob);
    }
    stmt.free();
    return result;
  }

  setCachedDaily(date, endpointName, jsonData) {
    this._db.run(
      'INSERT OR REPLACE INTO daily_cache (date, endpoint, json_blob, fetched_at) VALUES (?, ?, ?, ?)',
      [date, endpointName, JSON.stringify(jsonData), new Date().toISOString()]
    );
  }

  // ---- Activity cache ----

  getCachedActivity(activityId, dataType) {
    const stmt = this._db.prepare(
      'SELECT json_blob FROM activity_cache WHERE activity_id = ? AND data_type = ?'
    );
    stmt.bind([String(activityId), dataType]);
    let result = null;
    if (stmt.step()) {
      result = JSON.parse(stmt.getAsObject().json_blob);
    }
    stmt.free();
    return result;
  }

  setCachedActivity(activityId, dataType, jsonData) {
    this._db.run(
      'INSERT OR REPLACE INTO activity_cache (activity_id, data_type, json_blob, fetched_at) VALUES (?, ?, ?, ?)',
      [String(activityId), dataType, JSON.stringify(jsonData), new Date().toISOString()]
    );
  }

  // ---- Utilities ----

  /**
   * Returns true if `date` (YYYY-MM-DD) is within `refreshDays` days of today.
   */
  isWithinRefreshWindow(date, refreshDays) {
    const target = new Date(date + 'T00:00:00');
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const diffMs = today.getTime() - target.getTime();
    const diffDays = diffMs / (1000 * 60 * 60 * 24);
    return diffDays < refreshDays;
  }

  clearAll() {
    this._db.run('DELETE FROM daily_cache');
    this._db.run('DELETE FROM activity_cache');
  }

  save() {
    if (!this._dbPath) return;
    const data = this._db.export();
    const dir = path.dirname(this._dbPath);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    fs.writeFileSync(this._dbPath, Buffer.from(data));
  }

  close() {
    try {
      this.save();
    } finally {
      if (this._db) {
        this._db.close();
        this._db = null;
      }
    }
  }

  getStats() {
    const daily = this._db.exec('SELECT COUNT(*) as cnt FROM daily_cache');
    const activity = this._db.exec('SELECT COUNT(*) as cnt FROM activity_cache');
    return {
      dailyCount: daily.length ? daily[0].values[0][0] : 0,
      activityCount: activity.length ? activity[0].values[0][0] : 0,
    };
  }
}

/**
 * Create tables and meta row for a fresh database.
 */
function createSchema(db) {
  db.run(`CREATE TABLE IF NOT EXISTS meta (
    key TEXT PRIMARY KEY,
    value TEXT
  )`);
  db.run(`CREATE TABLE IF NOT EXISTS daily_cache (
    date TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    json_blob TEXT,
    fetched_at TEXT,
    PRIMARY KEY (date, endpoint)
  )`);
  db.run(`CREATE TABLE IF NOT EXISTS activity_cache (
    activity_id TEXT NOT NULL,
    data_type TEXT NOT NULL,
    json_blob TEXT,
    fetched_at TEXT,
    PRIMARY KEY (activity_id, data_type)
  )`);
  db.run("INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_version', ?)", [SCHEMA_VERSION]);
}

/**
 * Read the stored schema version. Returns null if meta table missing.
 */
function readSchemaVersion(db) {
  try {
    const rows = db.exec("SELECT value FROM meta WHERE key = 'schema_version'");
    if (rows.length && rows[0].values.length) {
      return rows[0].values[0][0];
    }
  } catch (_e) {
    // meta table doesn't exist
  }
  return null;
}

/**
 * Initialise (or open) a cache database at `dbPath`.
 * Returns a GarminCache instance.
 *
 * @param {string} dbPath  Absolute path to the .db file
 * @returns {Promise<GarminCache>}
 */
async function initCache(dbPath) {
  const initSqlJs = require('sql.js');

  const wasmBinary = fs.readFileSync(
    path.join(path.dirname(require.resolve('sql.js')), 'sql-wasm.wasm')
  );

  const SQL = await initSqlJs({ wasmBinary });

  let db;
  let needsSchema = false;

  if (dbPath && fs.existsSync(dbPath)) {
    try {
      const fileBuffer = fs.readFileSync(dbPath);
      db = new SQL.Database(fileBuffer);
      const version = readSchemaVersion(db);
      if (version !== SCHEMA_VERSION) {
        console.warn(
          `[GarminCache] Schema version mismatch (got ${version}, expected ${SCHEMA_VERSION}). Recreating database.`
        );
        db.close();
        db = new SQL.Database();
        needsSchema = true;
      }
    } catch (err) {
      console.warn('[GarminCache] Corrupt database, recreating:', err.message);
      db = new SQL.Database();
      needsSchema = true;
    }
  } else {
    db = new SQL.Database();
    needsSchema = true;
  }

  if (needsSchema) {
    createSchema(db);
  }

  return new GarminCache(db, dbPath);
}

module.exports = { initCache, GarminCache };
