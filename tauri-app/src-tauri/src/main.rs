// All Garmin work happens in the `garmin-core` sidecar (the Node core compiled with bun);
// the webview drives it through tauri-plugin-shell. No custom Rust commands needed.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_clipboard_manager::init())
        .run(tauri::generate_context!())
        .expect("error while running garmin-export");
}
