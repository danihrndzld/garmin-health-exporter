const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('garmin', {
  chooseDir:        ()     => ipcRenderer.invoke('choose-dir'),
  openFolder:       (p)    => ipcRenderer.invoke('open-folder', p),
  downloadHealth:   (opts) => ipcRenderer.invoke('download-health', opts),
  onLog:            (cb)   => ipcRenderer.on('log', (_, d) => cb(d)),
  onProgress:       (cb)   => ipcRenderer.on('progress', (_, d) => cb(d)),
  removeAllListeners: ()   => { ipcRenderer.removeAllListeners('log'); ipcRenderer.removeAllListeners('progress'); },
  defaultOutputDir: () => ipcRenderer.invoke('default-output-dir'),
});
