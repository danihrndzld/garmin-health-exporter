const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('garmin', {
  chooseDir:        ()     => ipcRenderer.invoke('choose-dir'),
  openFolder:       (p)    => ipcRenderer.invoke('open-folder', p),
  downloadHealth:   (opts) => ipcRenderer.invoke('download-health', opts),
  clearCache:       ()     => ipcRenderer.invoke('clear-cache'),
  onLog:            (cb)   => ipcRenderer.on('log',           (_, d) => cb(d)),
  onProgress:       (cb)   => ipcRenderer.on('progress',      (_, d) => cb(d)),
  onDownloadComplete: (cb) => ipcRenderer.on('download-complete', (_, d) => cb(d)),
  removeAllListeners: () => {
    ipcRenderer.removeAllListeners('log');
    ipcRenderer.removeAllListeners('progress');
    ipcRenderer.removeAllListeners('download-complete');
  },
  defaultOutputDir:  () => ipcRenderer.invoke('default-output-dir'),
  getVersion:        () => ipcRenderer.invoke('get-version'),
  checkForUpdates:   () => ipcRenderer.invoke('check-for-updates'),
  openUrl:           (u) => ipcRenderer.invoke('open-url', u),
});
