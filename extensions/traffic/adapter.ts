// Copied only into the isolated build. Official source files remain untouched.
import type { Socket } from '../socket';
export function initTrafficExtension(socket: Socket): void {
  const page = document.getElementById('page-dashboard');
  const grid = document.getElementById('dash-grid-root');
  if (!page || !grid || document.getElementById('traffic-extension-frame')) return;
  const frame = document.createElement('iframe');
  frame.id = 'traffic-extension-frame';
  frame.title = 'OpenClash 实时流量';
  frame.loading = 'lazy';
  frame.style.cssText = 'display:block;border:0;width:calc(100% - 40px);margin:16px 20px 0;height:345px;border-radius:12px;';
  const resize = () => { frame.style.height = page.clientWidth < 700 ? '450px' : '345px'; };
  new ResizeObserver(resize).observe(page);
  const setRouter = (id: unknown) => {
    if (typeof id !== 'string' || !id) return;
    frame.src = '/extensions/traffic/?embed=1&router=' + encodeURIComponent(id);
  };
  frame.src = '/extensions/traffic/?embed=1';
  page.insertBefore(frame, grid);
  socket.on('router:active', d => setRouter(d?.activeId));
}
