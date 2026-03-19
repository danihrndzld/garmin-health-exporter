const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('garmin', {
  chooseDir:        ()     => ipcRenderer.invoke('choose-dir'),
  openFolder:       (p)    => ipcRenderer.invoke('open-folder', p),
  downloadHealth:   (opts) => ipcRenderer.invoke('download-health', opts),
  checkDeps:        ()     => ipcRenderer.invoke('check-deps'),
  installDeps:      ()     => ipcRenderer.invoke('install-deps'),
  onLog:            (cb)   => ipcRenderer.on('log',           (_, d) => cb(d)),
  onProgress:       (cb)   => ipcRenderer.on('progress',      (_, d) => cb(d)),
  onSetupRequired:  (cb)   => ipcRenderer.on('setup-required',(_, d) => cb(d)),
  onSetupLog:       (cb)   => ipcRenderer.on('setup-log',     (_, d) => cb(d)),
  removeAllListeners: () => {
    ipcRenderer.removeAllListeners('log');
    ipcRenderer.removeAllListeners('progress');
    ipcRenderer.removeAllListeners('setup-log');
  },
  defaultOutputDir: () => ipcRenderer.invoke('default-output-dir'),
});
