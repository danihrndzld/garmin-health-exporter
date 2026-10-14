//! MyFitnessPal sign in. The user signs in inside an app window; we copy the session
//! cookies to mfp_session.json (mode 0600) for the Go core. The password never
//! reaches Rust or the UI, and the login window has no IPC permissions.
use serde::Serialize;
use std::{
    fs,
    io::Write,
    os::unix::fs::OpenOptionsExt,
    path::PathBuf,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
    time::{SystemTime, UNIX_EPOCH},
};
use tauri::{
    webview::{Cookie, PageLoadEvent},
    AppHandle, Emitter, Manager, WebviewUrl, WebviewWindowBuilder,
};

const LOGIN_URL: &str = "https://www.myfitnesspal.com/account/login";
const SESSION_COOKIE: &str = "__Secure-next-auth.session-token";
// A full Safari string; the Go core sends the same one, because Cloudflare ties
// cf_clearance to the User-Agent.
const USER_AGENT: &str = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Safari/605.1.15";

#[derive(Serialize, Clone)]
struct SavedCookie {
    name: String,
    value: String,
    domain: String,
    path: String,
    expires: i64,
}

#[derive(Serialize, Clone)]
pub struct MfpStatus {
    connected: bool,
    saved_at: Option<String>,
    session_expires: Option<i64>,
}

fn session_path(app: &AppHandle) -> Result<PathBuf, String> {
    let dir = app.path().app_data_dir().map_err(|e| e.to_string())?;
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    Ok(dir.join("mfp_session.json"))
}

fn is_mfp_domain(d: &str) -> bool {
    let d = d.trim_start_matches('.');
    d == "myfitnesspal.com" || d.ends_with(".myfitnesspal.com")
}

/// Keep cookies for myfitnesspal.com and its subdomains. Filtering by suffix matters:
/// cookies_for_url drops cookies set on ".myfitnesspal.com" (cf_clearance).
fn mfp_cookies(all: Vec<Cookie<'static>>) -> Vec<SavedCookie> {
    all.into_iter()
        .filter(|c| is_mfp_domain(c.domain().unwrap_or("")))
        .map(|c| SavedCookie {
            name: c.name().to_string(),
            value: c.value().to_string(),
            domain: c.domain().unwrap_or("").to_string(),
            path: c.path().unwrap_or("/").to_string(),
            expires: c.expires_datetime().map(|t| t.unix_timestamp()).unwrap_or(0),
        })
        .collect()
}

fn write_private(path: &PathBuf, data: &[u8]) -> std::io::Result<()> {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or(0);
    let tmp = path.with_file_name(format!(".mfp_session.{}.{}.tmp", std::process::id(), nanos));
    let res = (|| {
        let mut f = fs::OpenOptions::new().write(true).create_new(true).mode(0o600).open(&tmp)?;
        f.write_all(data)?;
        f.sync_all()?;
        fs::rename(&tmp, path)
    })();
    if res.is_err() {
        let _ = fs::remove_file(&tmp);
    }
    res
}

/// Remove the MFP cookies from the main webview's store (the login window shares it).
fn clear_mfp_cookies(app: &AppHandle) {
    if let Some(main) = app.get_webview_window("main") {
        if let Ok(all) = main.cookies() {
            for c in all {
                if is_mfp_domain(c.domain().unwrap_or("")) {
                    let _ = main.delete_cookie(c);
                }
            }
        }
    }
}

fn status_from_file(path: &PathBuf) -> MfpStatus {
    let v: Option<serde_json::Value> = fs::read(path).ok().and_then(|b| serde_json::from_slice(&b).ok());
    match v {
        Some(v) => MfpStatus {
            connected: true,
            saved_at: v["saved_at"].as_str().map(String::from),
            session_expires: v["session_expires"].as_i64(),
        },
        None => MfpStatus { connected: false, saved_at: None, session_expires: None },
    }
}

#[tauri::command]
pub fn mfp_status(app: AppHandle) -> Result<MfpStatus, String> {
    Ok(status_from_file(&session_path(&app)?))
}

#[tauri::command]
pub async fn mfp_connect(app: AppHandle) -> Result<(), String> {
    if let Some(w) = app.get_webview_window("mfp-login") {
        let _ = w.set_focus();
        return Ok(());
    }
    let path = session_path(&app)?;
    // Start fresh so a stale token is never re-saved.
    let _ = fs::remove_file(&path);
    clear_mfp_cookies(&app);
    let saved = Arc::new(AtomicBool::new(false));
    WebviewWindowBuilder::new(&app, "mfp-login", WebviewUrl::External(LOGIN_URL.parse().unwrap()))
        .title("Connect MyFitnessPal")
        .inner_size(480.0, 720.0)
        .user_agent(USER_AGENT)
        .on_page_load(move |win, payload| {
            let url = payload.url();
            if payload.event() != PageLoadEvent::Finished
                || url.scheme() != "https"
                || !is_mfp_domain(url.host_str().unwrap_or(""))
                || url.path().starts_with("/account/login")
            {
                return;
            }
            let (win, path, saved) = (win.clone(), path.clone(), saved.clone());
            // cookies() pumps the main run loop; never call it from the page load callback itself.
            tauri::async_runtime::spawn(async move {
                let Ok(all) = win.cookies() else { return };
                let cookies = mfp_cookies(all);
                let Some(token) = cookies.iter().find(|c| c.name == SESSION_COOKIE) else { return };
                if saved.swap(true, Ordering::SeqCst) {
                    return;
                }
                let body = serde_json::json!({
                    "cookies": cookies,
                    "user_agent": USER_AGENT,
                    "saved_at": time::OffsetDateTime::now_utc().format(&time::format_description::well_known::Rfc3339).unwrap_or_default(),
                    "session_expires": token.expires,
                });
                if write_private(&path, body.to_string().as_bytes()).is_ok() {
                    let app = win.app_handle().clone();
                    let _ = app.emit("mfp-connected", status_from_file(&path));
                    let _ = win.close();
                } else {
                    saved.store(false, Ordering::SeqCst);
                }
            });
        })
        .build()
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
pub async fn mfp_disconnect(app: AppHandle) -> Result<(), String> {
    let path = session_path(&app)?;
    let _ = fs::remove_file(&path);
    clear_mfp_cookies(&app);
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_only_myfitnesspal_cookies_including_dot_domains() {
        let make = |name: &'static str, domain: &'static str| Cookie::build((name, "v")).domain(domain).path("/").build().into_owned();
        let kept = mfp_cookies(vec![
            make(SESSION_COOKIE, "www.myfitnesspal.com"),
            make("cf_clearance", ".myfitnesspal.com"),
            make("other", "evilmyfitnesspal.com"),
            make("ga", "google.com"),
            make("root", "myfitnesspal.com"),
            make("evil2", "myfitnesspal.com.evil.com"),
            Cookie::build(("nodomain", "v")).build().into_owned(),
        ]);
        let names: Vec<_> = kept.iter().map(|c| c.name.as_str()).collect();
        assert_eq!(names, vec![SESSION_COOKIE, "cf_clearance", "root"]);
    }

    #[test]
    fn is_mfp_domain_cases() {
        for ok in ["myfitnesspal.com", ".myfitnesspal.com", "www.myfitnesspal.com"] {
            assert!(is_mfp_domain(ok), "{ok}");
        }
        for bad in ["", "myfitnesspal.com.evil.com", "evilmyfitnesspal.com", "google.com"] {
            assert!(!is_mfp_domain(bad), "{bad}");
        }
    }
}
