import { exportToSvg } from "@excalidraw/excalidraw";
window.renderSvg = async (scene) => {
  const svg = await exportToSvg({
    elements: scene.elements,
    appState: { ...(scene.appState || {}), exportBackground: true, viewBackgroundColor: "#ffffff", exportWithDarkMode: false },
    files: scene.files || {},
    exportPadding: 48,
  });
  return svg.outerHTML;
};
