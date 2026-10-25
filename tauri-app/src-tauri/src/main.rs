//! Garmin Export's Tauri shell. Garmin work happens in the Go core; Rust only handles
//! the MyFitnessPal sign in window.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod mfp;

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_clipboard_manager::init())
        .invoke_handler(tauri::generate_handler![
            mfp::mfp_connect,
            mfp::mfp_disconnect,
            mfp::mfp_status
        ])
        .run(tauri::generate_context!())
        .expect("Tauri could not start the app");
}
